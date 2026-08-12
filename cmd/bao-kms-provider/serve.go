package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/user"
	"strconv"
	"sync"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/auth"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/cli"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/config"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/kmsv2"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/logging"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/metrics"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/openbao"
	appruntime "github.com/dc-tec/openbao-kubernetes-kms/internal/runtime"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/socket"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/status"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/version"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
)

const (
	diagnosticCiphertext   = "openbao-kms-provider-diagnostic-ciphertext"
	messageContextRequired = "context is required"
	authMethodJWT          = "jwt"
	authMethodCert         = "cert"
	certSourcePKCS11       = "pkcs11"
	certSourceSPIFFE       = "spiffe"
)

type runtimeBuilder struct {
	info      version.Info
	logWriter io.Writer
}

type serveDependencies struct {
	runtime   *appruntime.Runtime
	scheduler *status.Scheduler
}

func newServeCommand(runtimeConfig *config.Runtime, configPath *string, info version.Info) *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start the KMS provider",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadAndValidateConfig(runtimeConfig, *configPath, true)
			if err != nil {
				return err
			}
			builder := runtimeBuilder{info: info, logWriter: cmd.ErrOrStderr()}
			deps, err := builder.build(cmd.Context(), cfg)
			if err != nil {
				return cli.WithExitCode(cli.ExitRuntime, err)
			}
			if err := runServe(cmd.Context(), deps); err != nil {
				return cli.WithExitCode(cli.ExitRuntime, err)
			}
			return nil
		},
	}
}

func loadAndValidateConfig(
	runtimeConfig *config.Runtime,
	configPath string,
	checkFilesystem bool,
) (config.Config, error) {
	cfg, err := config.Load(runtimeConfig, config.LoadOptions{Path: configPath})
	if err != nil {
		return config.Config{}, cli.WithExitCode(cli.ExitConfig, err)
	}
	if err := config.Validate(cfg, config.ValidationOptions{
		ConfigFilePath:  configPath,
		CheckFilesystem: checkFilesystem,
	}); err != nil {
		return config.Config{}, cli.WithExitCode(cli.ExitConfig, err)
	}
	return cfg, nil
}

func (b runtimeBuilder) build(ctx context.Context, cfg config.Config) (serveDependencies, error) {
	if ctx == nil {
		return serveDependencies{}, errors.New(messageContextRequired)
	}
	logger, err := logging.New(logging.Options{
		Level:  cfg.Logging.Level,
		Format: cfg.Logging.Format,
		Output: b.logWriter,
	})
	if err != nil {
		return serveDependencies{}, err
	}
	metricsRecorder, err := metrics.NewRecorder()
	if err != nil {
		return serveDependencies{}, err
	}
	observer := observability{
		logger:  logger,
		metrics: metricsRecorder,
		correlation: newDebugCorrelation(
			cfg.Logging.DebugCorrelation,
			cfg.Logging.LogOpenBaoRequestIDs,
			time.Now(),
		),
	}

	mode, err := config.ParseSocketMode(cfg.Server.SocketMode)
	if err != nil {
		return serveDependencies{}, err
	}
	gid, err := lookupGroupID(cfg.Server.SocketGroup)
	if err != nil {
		return serveDependencies{}, err
	}

	authManager, err := buildAuthManager(ctx, cfg, observer)
	if err != nil {
		return serveDependencies{}, err
	}
	transitClient, err := openbao.NewClient(openbao.ClientConfig{
		Address:       cfg.OpenBao.Address,
		Namespace:     cfg.OpenBao.Namespace,
		CACertFile:    cfg.OpenBao.CACertFile,
		TLSServerName: cfg.OpenBao.TLSServerName,
		Timeout:       cfg.OpenBao.Timeout,
		TokenSource:   authManager,
		Observer:      observer,
	})
	if err != nil {
		return serveDependencies{}, err
	}

	store, controller, scheduler, err := buildStatusRuntime(cfg, transitClient, observer)
	if err != nil {
		return serveDependencies{}, err
	}
	if err := metricsRecorder.RegisterAuthProvider(authManager); err != nil {
		return serveDependencies{}, err
	}
	if err := metricsRecorder.RegisterStatusProvider(store); err != nil {
		return serveDependencies{}, err
	}
	if err := probeOnceWithBootstrapGrace(ctx, controller, cfg.Bootstrap); err != nil {
		return serveDependencies{}, fmt.Errorf("initialize status cache: %w", err)
	}

	kmsServer, err := kmsv2.NewServer(kmsv2.Options{
		StatusCache:    store,
		Registry:       store,
		Transit:        transitAdapter{client: transitClient, mountPath: cfg.Transit.MountPath, keyName: cfg.Transit.KeyName},
		PluginVersion:  b.info.Version,
		RequestTimeout: cfg.OpenBao.Timeout,
		Observer:       observer,
	})
	if err != nil {
		return serveDependencies{}, err
	}
	grpcServer := grpc.NewServer(
		grpc.MaxRecvMsgSize(kmsv2.MaxGRPCMessageBytes),
		grpc.MaxSendMsgSize(kmsv2.MaxGRPCMessageBytes),
	)
	kmsv2.Register(grpcServer, kmsServer)

	rt, err := appruntime.New(appruntime.Options{
		Socket: socket.Options{
			Path: cfg.Server.SocketPath,
			Mode: mode,
			GID:  gid,
			OnStaleSocketRemoved: func() {
				observer.ObserveSocketRestart(ctx)
			},
		},
		GRPCServer:     grpcServer,
		Readiness:      readinessAdapter{store: store},
		HealthAddress:  cfg.Server.HealthAddress,
		MetricsAddress: cfg.Server.MetricsAddress,
		MetricsHandler: metricsRecorder.Handler(),
	})
	if err != nil {
		return serveDependencies{}, err
	}

	return serveDependencies{runtime: rt, scheduler: scheduler}, nil
}

func buildStatusRuntime(
	cfg config.Config,
	transitClient openbao.TransitClient,
	probeObserver status.ProbeObserver,
) (*status.Store, *status.Controller, *status.Scheduler, error) {
	store, err := status.NewStore(status.StoreOptions{MaxStaleness: cfg.Status.StatusMaxStaleness})
	if err != nil {
		return nil, nil, nil, err
	}
	observer, err := status.NewObserver(status.SnapshotScope{
		ProviderName:        cfg.Transit.KeyIDScope.ProviderName,
		ClusterID:           cfg.Transit.KeyIDScope.ClusterID,
		OpenBaoInstanceID:   cfg.OpenBao.InstanceID,
		OpenBaoNamespace:    cfg.OpenBao.Namespace,
		TransitMountID:      cfg.Transit.KeyIDScope.TransitMountID,
		TransitKeyLineageID: cfg.Transit.KeyIDScope.KeyLineageID,
		AADMode:             keyregistry.AADModeRequired,
	}, status.RotationPolicy{
		ActivationDelay:               cfg.Rotation.ActivationDelay,
		RequireStableObservationCount: cfg.Rotation.RequireStableObservationCount,
		RejectVersionRollback:         cfg.Rotation.RejectVersionRollback,
	})
	if err != nil {
		return nil, nil, nil, err
	}
	controller, err := status.NewController(status.ControllerOptions{
		Store:         store,
		Observer:      observer,
		Transit:       transitClient,
		StateStore:    status.FileStateStore{Path: cfg.State.Path},
		MountPath:     cfg.Transit.MountPath,
		KeyName:       cfg.Transit.KeyName,
		ProbeObserver: probeObserver,
	})
	if err != nil {
		return nil, nil, nil, err
	}
	scheduler, err := status.NewScheduler(status.SchedulerOptions{
		Controller:        controller,
		ProbeInterval:     cfg.Status.ProbeInterval,
		DeepProbeInterval: cfg.Status.DeepProbeInterval,
	})
	if err != nil {
		return nil, nil, nil, err
	}
	return store, controller, scheduler, nil
}

func runServe(ctx context.Context, deps serveDependencies) error {
	if ctx == nil {
		return errors.New(messageContextRequired)
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	group, groupCtx := errgroup.WithContext(runCtx)
	group.Go(func() error {
		defer cancel()
		return deps.runtime.Run(groupCtx)
	})
	group.Go(func() error {
		defer cancel()
		err := deps.scheduler.Run(groupCtx)
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	})
	return group.Wait()
}

func authConfig(cfg config.Config) auth.ManagerConfig {
	return auth.ManagerConfig{
		MountPath:              cfg.Auth.JWT.MountPath,
		Role:                   cfg.Auth.JWT.Role,
		JWTFile:                cfg.Auth.JWT.JWTFile,
		MinJWTRemainingTTL:     cfg.Auth.JWT.MinRemainingTTL,
		ClockSkewLeeway:        cfg.Auth.JWT.ClockSkewLeeway,
		LoginBeforeTokenExpiry: cfg.Auth.LoginBeforeTokenExpiry,
		TokenRenewalIncrement:  cfg.Auth.TokenRenewalIncrement,
		ExpectedIssuer:         cfg.Auth.JWT.ExpectedIssuer,
		ExpectedAudience:       cfg.Auth.JWT.ExpectedAudience,
		ExpectedSubject:        cfg.Auth.JWT.ExpectedSubject,
	}
}

func buildAuthManager(
	ctx context.Context,
	cfg config.Config,
	observer authRuntimeObserver,
) (*auth.Manager, error) {
	switch cfg.Auth.Method {
	case authMethodJWT:
		authClient, err := openbao.NewAuthClient(openbao.AuthClientConfig{
			Address:       cfg.OpenBao.Address,
			Namespace:     cfg.OpenBao.Namespace,
			CACertFile:    cfg.OpenBao.CACertFile,
			TLSServerName: cfg.OpenBao.TLSServerName,
			Timeout:       authLoginTimeout(cfg),
			Observer:      observer,
		})
		if err != nil {
			return nil, err
		}
		return auth.NewManager(authConfig(cfg), authClient, auth.ManagerOptions{
			RenewalEnabled: true,
			Observer:       observer,
		})
	case authMethodCert:
		return newCertAuthManager(ctx, cfg, observer)
	default:
		return nil, fmt.Errorf("%w: unsupported auth method", auth.ErrAuthConfig)
	}
}

type authRuntimeObserver interface {
	auth.Observer
	openbao.RequestObserver
}

type bootstrapProbeController interface {
	ProbeOnce(context.Context) error
	DeepProbeOnce(context.Context) error
}

func probeOnceWithBootstrapGrace(
	ctx context.Context,
	controller bootstrapProbeController,
	cfg config.BootstrapConfig,
) error {
	return probeOnceWithBootstrapGraceAndSleep(ctx, controller, cfg, time.Now, sleepContext)
}

func probeOnceWithBootstrapGraceAndSleep(
	ctx context.Context,
	controller bootstrapProbeController,
	cfg config.BootstrapConfig,
	now func() time.Time,
	sleep func(context.Context, time.Duration) error,
) error {
	if cfg.GraceTimeout <= 0 {
		return runBootstrapProbe(ctx, controller)
	}
	deadline := now().Add(cfg.GraceTimeout)
	var lastErr error
	for {
		if err := runBootstrapProbe(ctx, controller); err == nil {
			return nil
		} else {
			lastErr = err
		}
		remaining := deadline.Sub(now())
		if remaining <= 0 {
			return fmt.Errorf("bootstrap probes did not succeed within %s: %w", cfg.GraceTimeout, lastErr)
		}
		delay := cfg.RetryInterval
		if delay <= 0 {
			delay = 5 * time.Second
		}
		if delay > remaining {
			delay = remaining
		}
		if err := sleep(ctx, delay); err != nil {
			return err
		}
	}
}

func runBootstrapProbe(ctx context.Context, controller bootstrapProbeController) error {
	if err := controller.ProbeOnce(ctx); err != nil {
		return err
	}
	return controller.DeepProbeOnce(ctx)
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func authLoginTimeout(cfg config.Config) time.Duration {
	if cfg.Auth.LoginTimeout > 0 {
		return cfg.Auth.LoginTimeout
	}
	if cfg.OpenBao.Timeout > 5*time.Second {
		return cfg.OpenBao.Timeout
	}
	return 5 * time.Second
}

func lookupGroupID(name string) (int, error) {
	if name == "" {
		return -1, nil
	}
	if gid, ok, err := parseNumericGroupID(name); ok || err != nil {
		if err != nil {
			return -1, err
		}
		return gid, nil
	}
	group, err := user.LookupGroup(name)
	if err != nil {
		return -1, fmt.Errorf("lookup socket group: %w", err)
	}
	gid64, err := strconv.ParseInt(group.Gid, 10, 32)
	if err != nil {
		return -1, fmt.Errorf("parse socket group id: %w", err)
	}
	return int(gid64), nil
}

func parseNumericGroupID(value string) (int, bool, error) {
	for _, char := range value {
		if char < '0' || char > '9' {
			return -1, false, nil
		}
	}
	gid64, err := strconv.ParseInt(value, 10, 32)
	if err != nil {
		return -1, true, fmt.Errorf("parse socket group id: %w", err)
	}
	return int(gid64), true, nil
}

type transitAdapter struct {
	client    openbao.TransitClient
	mountPath string
	keyName   string
}

func (a transitAdapter) Encrypt(
	ctx context.Context,
	req kmsv2.TransitEncryptRequest,
) (kmsv2.TransitEncryptResponse, error) {
	result, err := a.client.Encrypt(ctx, openbao.EncryptRequest{
		MountPath:      a.mountPath,
		KeyName:        a.keyName,
		Plaintext:      req.Plaintext,
		AssociatedData: req.AssociatedData,
		KeyVersion:     req.KeyVersion,
	})
	if err != nil {
		return kmsv2.TransitEncryptResponse{}, err
	}
	return kmsv2.TransitEncryptResponse{
		Ciphertext: []byte(result.Ciphertext),
		KeyVersion: result.KeyVersion,
	}, nil
}

func (a transitAdapter) Decrypt(
	ctx context.Context,
	req kmsv2.TransitDecryptRequest,
) (kmsv2.TransitDecryptResponse, error) {
	result, err := a.client.Decrypt(ctx, openbao.DecryptRequest{
		MountPath:      a.mountPath,
		KeyName:        a.keyName,
		Ciphertext:     string(req.Ciphertext),
		AssociatedData: req.AssociatedData,
	})
	if err != nil {
		return kmsv2.TransitDecryptResponse{}, err
	}
	return kmsv2.TransitDecryptResponse{Plaintext: result.Plaintext}, nil
}

type readinessAdapter struct {
	store *status.Store
}

func (r readinessAdapter) Ready(ctx context.Context) (status.Diagnostics, error) {
	return r.store.Diagnostics(ctx)
}

type diagnosticTransit struct {
	mu      sync.Mutex
	records map[string]diagnosticTransitRecord
}

type diagnosticTransitRecord struct {
	plaintext      []byte
	associatedData []byte
}

func (d *diagnosticTransit) Encrypt(
	_ context.Context,
	req kmsv2.TransitEncryptRequest,
) (kmsv2.TransitEncryptResponse, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.records == nil {
		d.records = make(map[string]diagnosticTransitRecord)
	}
	ciphertext := fmt.Sprintf("%s-%d", diagnosticCiphertext, len(d.records)+1)
	d.records[ciphertext] = diagnosticTransitRecord{
		plaintext:      append([]byte(nil), req.Plaintext...),
		associatedData: append([]byte(nil), req.AssociatedData...),
	}
	return kmsv2.TransitEncryptResponse{
		Ciphertext: []byte(ciphertext),
		KeyVersion: req.KeyVersion,
	}, nil
}

func (d *diagnosticTransit) Decrypt(
	_ context.Context,
	req kmsv2.TransitDecryptRequest,
) (kmsv2.TransitDecryptResponse, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	record, ok := d.records[string(req.Ciphertext)]
	if !ok {
		return kmsv2.TransitDecryptResponse{}, fmt.Errorf("diagnostic ciphertext not found")
	}
	if !bytes.Equal(record.associatedData, req.AssociatedData) {
		return kmsv2.TransitDecryptResponse{}, fmt.Errorf("diagnostic associated data mismatch")
	}
	return kmsv2.TransitDecryptResponse{
		Plaintext: append([]byte(nil), record.plaintext...),
	}, nil
}

func commandContext(cmd *cobra.Command) context.Context {
	return cmd.Context()
}

func withTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, timeout)
}
