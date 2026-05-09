// Package runtime composes the local KMS process: socket listener, gRPC
// server, optional health HTTP server, signal handling, and graceful
// shutdown.
//
// Run blocks until its context is canceled or SIGINT/SIGTERM is delivered,
// then drains in-flight requests within ShutdownTimeout. The Unix socket
// file is unlinked as part of listener Close, so a clean shutdown leaves
// no stale socket behind.
package runtime

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/health"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/socket"
)

// DefaultShutdownTimeout caps graceful shutdown when the caller does not specify one.
const DefaultShutdownTimeout = 15 * time.Second

const (
	healthReadHeaderTimeout = 5 * time.Second
	healthReadTimeout       = 10 * time.Second
	healthWriteTimeout      = 10 * time.Second
	healthIdleTimeout       = 30 * time.Second
)

var (
	// ErrInvalidConfig identifies invalid runtime options.
	ErrInvalidConfig = errors.New("runtime config invalid")
)

// Options controls runtime construction.
type Options struct {
	// Socket configures the Unix domain listener.
	Socket socket.Options
	// GRPCServer is the pre-built gRPC server. The caller is responsible for
	// registering the KMS v2 service before passing it to New.
	GRPCServer *grpc.Server
	// Readiness drives the /ready endpoint. Required when HealthAddress is set.
	Readiness health.ReadinessProbe
	// HealthAddress is a host:port for the /live and /ready HTTP endpoints.
	// An empty value disables the health listener.
	HealthAddress string
	// MetricsHandler serves Prometheus metrics. Required when MetricsAddress is set.
	MetricsHandler http.Handler
	// MetricsAddress is a host:port for the /metrics HTTP endpoint. An empty
	// value disables the metrics listener.
	MetricsAddress string
	// ShutdownTimeout bounds graceful shutdown. Zero applies DefaultShutdownTimeout.
	ShutdownTimeout time.Duration
}

// Runtime owns the bound listeners and the lifecycle of the local KMS process.
type Runtime struct {
	socketListener  *socket.Listener
	grpcServer      *grpc.Server
	healthListener  net.Listener
	healthServer    *http.Server
	metricsListener net.Listener
	metricsServer   *http.Server
	shutdownTimeout time.Duration

	live         runtimeLiveness
	shutdownOnce sync.Once
	running      atomic.Bool
}

// New binds the Unix socket and, when configured, the health HTTP listener.
// The runtime does not start serving until Run is called.
func New(opts Options) (*Runtime, error) {
	if opts.GRPCServer == nil {
		return nil, fmt.Errorf("%w: gRPC server is required", ErrInvalidConfig)
	}
	if opts.HealthAddress != "" && opts.Readiness == nil {
		return nil, fmt.Errorf("%w: readiness probe is required when health address is set", ErrInvalidConfig)
	}
	if opts.MetricsAddress != "" && opts.MetricsHandler == nil {
		return nil, fmt.Errorf("%w: metrics handler is required when metrics address is set", ErrInvalidConfig)
	}

	socketListener, err := socket.Listen(opts.Socket)
	if err != nil {
		return nil, fmt.Errorf("bind socket: %w", err)
	}

	r := &Runtime{
		socketListener:  socketListener,
		grpcServer:      opts.GRPCServer,
		shutdownTimeout: shutdownTimeoutOrDefault(opts.ShutdownTimeout),
	}
	if opts.HealthAddress != "" {
		handler, handlerErr := health.NewHandler(&r.live, opts.Readiness)
		if handlerErr != nil {
			_ = socketListener.Close()
			return nil, fmt.Errorf("build health handler: %w", handlerErr)
		}
		listener, server, healthErr := buildHTTPServer(opts.HealthAddress, handler)
		if healthErr != nil {
			_ = socketListener.Close()
			return nil, fmt.Errorf("bind health listener: %w", healthErr)
		}
		r.healthListener = listener
		r.healthServer = server
	}
	if opts.MetricsAddress != "" {
		listener, server, metricsErr := buildHTTPServer(opts.MetricsAddress, opts.MetricsHandler)
		if metricsErr != nil {
			_ = socketListener.Close()
			if r.healthListener != nil {
				_ = r.healthListener.Close()
			}
			return nil, fmt.Errorf("bind metrics listener: %w", metricsErr)
		}
		r.metricsListener = listener
		r.metricsServer = server
	}
	return r, nil
}

// SocketPath returns the bound socket path.
func (r *Runtime) SocketPath() string {
	return r.socketListener.Path()
}

// HealthAddr returns the bound health listener address, or nil when no health
// server is configured.
func (r *Runtime) HealthAddr() net.Addr {
	if r.healthListener == nil {
		return nil
	}
	return r.healthListener.Addr()
}

// MetricsAddr returns the bound metrics listener address, or nil when no metrics
// server is configured.
func (r *Runtime) MetricsAddr() net.Addr {
	if r.metricsListener == nil {
		return nil
	}
	return r.metricsListener.Addr()
}

// Run starts both servers and blocks until ctx is canceled, the process
// receives SIGINT/SIGTERM, or one of the servers exits unexpectedly.
//
// An unexpected serve exit initiates shutdown for the surviving servers. If the
// exiting server reported an error, Run returns it. Calling Run more than once
// returns an error.
func (r *Runtime) Run(ctx context.Context) error {
	if !r.running.CompareAndSwap(false, true) {
		return fmt.Errorf("%w: Run already invoked", ErrInvalidConfig)
	}

	sigCtx, stopSignals := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()

	r.live.markStarted()
	defer r.live.markStopped()

	servers := []serverEntry{{name: "grpc", serve: r.serveGRPC}}
	if r.healthServer != nil {
		servers = append(servers, serverEntry{name: "health", serve: r.serveHealth})
	}
	if r.metricsServer != nil {
		servers = append(servers, serverEntry{name: "metrics", serve: r.serveMetrics})
	}

	group, groupCtx := errgroup.WithContext(sigCtx)
	serverExited := make(chan struct{}, len(servers))
	for _, entry := range servers {
		group.Go(func() error {
			err := entry.serve()
			serverExited <- struct{}{}
			if err != nil {
				return fmt.Errorf("%s server: %w", entry.name, err)
			}
			return nil
		})
	}

	select {
	case <-sigCtx.Done():
	case <-groupCtx.Done():
	case <-serverExited:
	}

	r.shutdown(ctx)
	return group.Wait()
}

func (r *Runtime) serveGRPC() error {
	err := r.grpcServer.Serve(r.socketListener)
	if errors.Is(err, grpc.ErrServerStopped) {
		return nil
	}
	return err
}

func (r *Runtime) serveHealth() error {
	err := r.healthServer.Serve(r.healthListener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (r *Runtime) serveMetrics() error {
	err := r.metricsServer.Serve(r.metricsListener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// shutdown drains the gRPC and health servers within ShutdownTimeout.
//
// The shutdown context is derived with context.WithoutCancel so an already
// canceled parent ctx (e.g. signal-driven cancellation) does not pre-cancel
// graceful drain. The shutdown still has its own deadline.
func (r *Runtime) shutdown(parentCtx context.Context) {
	r.shutdownOnce.Do(func() {
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(parentCtx), r.shutdownTimeout)
		defer cancel()

		grpcDone := make(chan struct{})
		go func() {
			r.grpcServer.GracefulStop()
			close(grpcDone)
		}()
		select {
		case <-grpcDone:
		case <-shutdownCtx.Done():
			r.grpcServer.Stop()
			<-grpcDone
		}

		if r.healthServer != nil {
			_ = r.healthServer.Shutdown(shutdownCtx)
		}
		if r.metricsServer != nil {
			_ = r.metricsServer.Shutdown(shutdownCtx)
		}
	})
}

type serverEntry struct {
	name  string
	serve func() error
}

func buildHTTPServer(
	address string,
	handler http.Handler,
) (net.Listener, *http.Server, error) {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, nil, err
	}
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: healthReadHeaderTimeout,
		ReadTimeout:       healthReadTimeout,
		WriteTimeout:      healthWriteTimeout,
		IdleTimeout:       healthIdleTimeout,
	}
	return listener, server, nil
}

func shutdownTimeoutOrDefault(d time.Duration) time.Duration {
	if d <= 0 {
		return DefaultShutdownTimeout
	}
	return d
}

type runtimeLiveness struct {
	started atomic.Bool
}

func (l *runtimeLiveness) markStarted() {
	l.started.Store(true)
}

func (l *runtimeLiveness) markStopped() {
	l.started.Store(false)
}

// Live implements health.LivenessProbe.
func (l *runtimeLiveness) Live() error {
	if !l.started.Load() {
		return health.ErrNotStarted
	}
	return nil
}
