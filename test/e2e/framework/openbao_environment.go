//go:build e2e

package framework

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/openbao"
)

const (
	EnvOpenBaoCI    = "E2E_OPENBAO_CI"
	EnvOpenBaoImage = "E2E_OPENBAO_IMAGE"
	EnvDockerBinary = "DOCKER"
	EnvSkipCleanup  = "E2E_SKIP_CLEANUP"

	DefaultOpenBaoImage = "ghcr.io/openbao/openbao:2.5.3"

	openBaoListenAddress  = "0.0.0.0:8200"
	openBaoTLSServerName  = "localhost"
	openBaoTransitKeyType = "aes256-gcm96"

	openBaoJWTAuthMount        = "auth/k8s-workload-a-jwt"
	openBaoJWTAuthRole         = "openbao-kms-control-plane"
	openBaoJWTPolicyName       = "openbao-kms-workload-a"
	openBaoJWTIssuer           = "https://issuer.example.internal"
	openBaoJWTAudience         = "bao-kms-provider"
	openBaoJWTSubject          = "system:openbao-kms:workload-a"
	openBaoJWTTTL              = 30 * time.Minute
	openBaoJWTTokenTTL         = "10m"
	openBaoJWTTokenMaxTTL      = "30m"
	openBaoJWTClockSkewLeeway  = "30s"
	openBaoJWTExpirationLeeway = "30s"
)

var ErrDockerUnavailable = errors.New("docker is not available")

type OpenBaoEnvironmentConfig struct {
	Image         string
	TransitMount  string
	TransitKey    string
	StartupWait   time.Duration
	DockerBinary  string
	NetworkName   string
	StorageVolume string
	JWTTTL        time.Duration
	JWTTokenTTL   string
	JWTMaxTTL     string
}

type OpenBaoEnvironment struct {
	Address       string
	CACertFile    string
	TLSServerName string
	Token         string
	TransitMount  string
	TransitKey    string
	AuthMount     string
	AuthRole      string
	JWTFile       string

	image         string
	containerName string
	certDir       string
	dockerBinary  string
	networkName   string
	storageVolume string
	unsealKey     string
	jwtPrivateKey *rsa.PrivateKey
	jwtTTL        time.Duration
	jwtTokenTTL   string
	jwtMaxTTL     string
}

type mountRequestBody struct {
	Type string `json:"type"`
}

type disableUpsertRequestBody struct {
	DisableUpsert bool `json:"disable_upsert"`
}

type transitKeyRequestBody struct {
	Type string `json:"type"`
}

type policyRequestBody struct {
	Policy string `json:"policy"`
}

type emptyRequestBody struct{}

type initStatusResponseBody struct {
	Initialized bool `json:"initialized"`
}

type initRequestBody struct {
	SecretShares    int `json:"secret_shares"`
	SecretThreshold int `json:"secret_threshold"`
}

type initResponseBody struct {
	RootToken string   `json:"root_token"`
	KeysB64   []string `json:"keys_base64"`
}

type unsealRequestBody struct {
	Key string `json:"key"`
}

type jwtAuthConfigRequestBody struct {
	JWTValidationPubKeys []string `json:"jwt_validation_pubkeys"`
	BoundIssuer          string   `json:"bound_issuer"`
}

type jwtAuthRoleRequestBody struct {
	RoleType             string   `json:"role_type"`
	UserClaim            string   `json:"user_claim"`
	BoundAudiences       []string `json:"bound_audiences"`
	BoundSubject         string   `json:"bound_subject"`
	TokenPolicies        []string `json:"token_policies"`
	TokenTTL             string   `json:"token_ttl"`
	TokenMaxTTL          string   `json:"token_max_ttl"`
	TokenNoDefaultPolicy bool     `json:"token_no_default_policy"`
	ClockSkewLeeway      string   `json:"clock_skew_leeway"`
	ExpirationLeeway     string   `json:"expiration_leeway"`
}

type jwtHeader struct {
	Algorithm string `json:"alg"`
	Type      string `json:"typ"`
}

type jwtClaims struct {
	Issuer    string   `json:"iss"`
	Subject   string   `json:"sub"`
	Audience  []string `json:"aud"`
	ExpiresAt int64    `json:"exp"`
	NotBefore int64    `json:"nbf"`
	IssuedAt  int64    `json:"iat"`
}

type environmentSetupPayload interface {
	environmentSetupPayload()
}

func (mountRequestBody) environmentSetupPayload()         {}
func (disableUpsertRequestBody) environmentSetupPayload() {}
func (transitKeyRequestBody) environmentSetupPayload()    {}
func (policyRequestBody) environmentSetupPayload()        {}
func (emptyRequestBody) environmentSetupPayload()         {}
func (jwtAuthConfigRequestBody) environmentSetupPayload() {}
func (jwtAuthRoleRequestBody) environmentSetupPayload()   {}

func OpenBaoCIEnabled() bool {
	return strings.EqualFold(os.Getenv(EnvOpenBaoCI), "true")
}

func StartOpenBaoEnvironment(ctx context.Context, cfg OpenBaoEnvironmentConfig) (*OpenBaoEnvironment, error) {
	cfg = defaultOpenBaoEnvironmentConfig(cfg)
	dockerPath, err := resolveDockerBinary(cfg.DockerBinary)
	if err != nil {
		return nil, err
	}
	if err := checkDocker(ctx, dockerPath); err != nil {
		return nil, err
	}

	artifactDir, err := EnsureArtifactDir()
	if err != nil {
		return nil, fmt.Errorf("prepare e2e artifact directory: %w", err)
	}
	artifactDir, err = filepath.Abs(artifactDir)
	if err != nil {
		return nil, fmt.Errorf("resolve e2e artifact directory: %w", err)
	}
	certDir, err := os.MkdirTemp(artifactDir, "openbao-ci-tls-")
	if err != nil {
		return nil, fmt.Errorf("create OpenBao environment TLS directory: %w", err)
	}
	token, err := randomHex(24)
	if err != nil {
		return nil, fmt.Errorf("generate OpenBao environment token: %w", err)
	}
	suffix, err := randomHex(6)
	if err != nil {
		return nil, fmt.Errorf("generate OpenBao environment name: %w", err)
	}
	containerName := "bao-kms-e2e-" + suffix

	environment := &OpenBaoEnvironment{
		TLSServerName: openBaoTLSServerName,
		Token:         token,
		TransitMount:  cfg.TransitMount,
		TransitKey:    cfg.TransitKey,
		AuthMount:     openBaoJWTAuthMount,
		AuthRole:      openBaoJWTAuthRole,
		image:         cfg.Image,
		containerName: containerName,
		certDir:       certDir,
		dockerBinary:  dockerPath,
		networkName:   cfg.NetworkName,
		storageVolume: cfg.StorageVolume,
		jwtTTL:        cfg.JWTTTL,
		jwtTokenTTL:   cfg.JWTTokenTTL,
		jwtMaxTTL:     cfg.JWTMaxTTL,
	}
	if err := environment.startContainer(ctx, cfg.Image); err != nil {
		_ = environment.Close(context.Background())
		return nil, err
	}
	bootstrapRequired := true
	if cfg.StorageVolume == "" {
		if err := environment.waitUntilReady(ctx, cfg.StartupWait); err != nil {
			_ = environment.Close(context.Background())
			return nil, err
		}
	} else {
		if err := environment.waitUntilEndpoint(ctx, cfg.StartupWait); err != nil {
			_ = environment.Close(context.Background())
			return nil, err
		}
		initializedNow, err := environment.initializeRaftStorage(ctx)
		if err != nil {
			_ = environment.Close(context.Background())
			return nil, err
		}
		bootstrapRequired = initializedNow
		if err := environment.waitUntilReady(ctx, cfg.StartupWait); err != nil {
			_ = environment.Close(context.Background())
			return nil, err
		}
	}
	if bootstrapRequired {
		if err := environment.bootstrapTransit(ctx); err != nil {
			_ = environment.Close(context.Background())
			return nil, err
		}
		if err := environment.bootstrapJWTAuth(ctx); err != nil {
			_ = environment.Close(context.Background())
			return nil, err
		}
	}
	return environment, nil
}

func (f *OpenBaoEnvironment) NewClient() (*openbao.Client, error) {
	return f.NewClientWithTokenSource(openbao.StaticTokenSource{TokenValue: f.Token})
}

func (f *OpenBaoEnvironment) ContainerAddress() string {
	return "https://" + f.containerName + ":8200"
}

func (f *OpenBaoEnvironment) StorageVolumeName() string {
	return f.storageVolume
}

func (f *OpenBaoEnvironment) NewClientWithTokenSource(tokenSource openbao.TokenSource) (*openbao.Client, error) {
	return openbao.NewClient(openbao.ClientConfig{
		Address:       f.Address,
		CACertFile:    f.CACertFile,
		TLSServerName: f.TLSServerName,
		Timeout:       5 * time.Second,
		TokenSource:   tokenSource,
	})
}

func (f *OpenBaoEnvironment) NewAuthClient() (*openbao.AuthClient, error) {
	return openbao.NewAuthClient(openbao.AuthClientConfig{
		Address:       f.Address,
		CACertFile:    f.CACertFile,
		TLSServerName: f.TLSServerName,
		Timeout:       5 * time.Second,
	})
}

func (f *OpenBaoEnvironment) WriteJWTFile(now time.Time, ttl time.Duration) error {
	return f.WriteJWTFileAt(f.JWTFile, now, ttl)
}

func (f *OpenBaoEnvironment) WriteJWTFileAt(filePath string, now time.Time, ttl time.Duration) error {
	if f.jwtPrivateKey == nil {
		return fmt.Errorf("OpenBao environment JWT signer is not initialized")
	}
	jwt, err := signJWT(f.jwtPrivateKey, now, ttl)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filePath, []byte(jwt), 0o600); err != nil {
		return fmt.Errorf("write OpenBao environment JWT file: %w", err)
	}
	return nil
}

func (f *OpenBaoEnvironment) InstallProviderPolicy(ctx context.Context, policy string) error {
	httpClient, err := openbao.NewHTTPClient(f.CACertFile, openBaoTLSServerName, 5*time.Second)
	if err != nil {
		return err
	}
	return f.write(ctx, httpClient, "sys/policies/acl/"+openBaoJWTPolicyName, policyRequestBody{Policy: policy})
}

func (f *OpenBaoEnvironment) MetadataOnlyProviderPolicy() string {
	return fmt.Sprintf(`path %q {
  capabilities = ["read"]
}

path "sys/capabilities-self" {
  capabilities = ["update"]
}
`,
		path.Join(f.TransitMount, "keys", f.TransitKey),
	)
}

func (f *OpenBaoEnvironment) Seal(ctx context.Context) error {
	httpClient, err := openbao.NewHTTPClient(f.CACertFile, openBaoTLSServerName, 5*time.Second)
	if err != nil {
		return err
	}
	return f.write(ctx, httpClient, "sys/seal", emptyRequestBody{})
}

func (f *OpenBaoEnvironment) Close(ctx context.Context) error {
	stopErr := f.StopContainer(ctx)
	if !strings.EqualFold(os.Getenv(EnvSkipCleanup), "true") && f.certDir != "" {
		if err := os.RemoveAll(f.certDir); err != nil && stopErr == nil {
			stopErr = fmt.Errorf("remove OpenBao environment TLS directory: %w", err)
		}
	}
	return stopErr
}

// StopContainer removes only the OpenBao container and keeps generated test artifacts intact.
func (f *OpenBaoEnvironment) StopContainer(ctx context.Context) error {
	return f.stopContainer(ctx, true)
}

// StopContainerKeepAddress removes the container but keeps the configured name for restart tests.
func (f *OpenBaoEnvironment) StopContainerKeepAddress(ctx context.Context) error {
	return f.stopContainer(ctx, false)
}

func (f *OpenBaoEnvironment) StartStoppedContainer(ctx context.Context) error {
	if f.storageVolume == "" {
		return fmt.Errorf("OpenBao environment restart requires file storage")
	}
	if f.containerName == "" {
		return fmt.Errorf("OpenBao environment container name is empty")
	}
	if err := f.startContainer(ctx, f.image); err != nil {
		return err
	}
	if err := f.waitUntilEndpoint(ctx, 45*time.Second); err != nil {
		return err
	}
	if _, err := f.initializeRaftStorage(ctx); err != nil {
		return err
	}
	return f.waitUntilReady(ctx, 45*time.Second)
}

func (f *OpenBaoEnvironment) RestoreRaftSnapshot(ctx context.Context, storageVolume string, snapshotPath string) error {
	if f.storageVolume == "" {
		return fmt.Errorf("OpenBao environment restore requires raft storage")
	}
	if storageVolume == "" {
		return fmt.Errorf("OpenBao environment restore volume is empty")
	}
	if snapshotPath == "" {
		return fmt.Errorf("OpenBao environment restore snapshot is empty")
	}
	originalToken := f.Token
	originalUnsealKey := f.unsealKey
	if originalToken == "" || originalUnsealKey == "" {
		return fmt.Errorf("OpenBao environment restore requires original root token and unseal key")
	}
	if err := f.StopContainerKeepAddress(ctx); err != nil {
		return err
	}
	f.storageVolume = storageVolume
	if err := f.startContainer(ctx, f.image); err != nil {
		return err
	}
	if err := f.waitUntilEndpoint(ctx, 45*time.Second); err != nil {
		return err
	}
	httpClient, err := openbao.NewHTTPClient(f.CACertFile, openBaoTLSServerName, 30*time.Second)
	if err != nil {
		return err
	}
	freshToken, freshUnsealKey, err := f.initializeForRestore(ctx, httpClient)
	if err != nil {
		return err
	}
	f.unsealKey = freshUnsealKey
	if err := f.unseal(ctx, httpClient); err != nil {
		return err
	}
	if err := f.copySnapshotIntoContainer(ctx, snapshotPath); err != nil {
		return err
	}
	if err := f.restoreRaftSnapshotInContainer(ctx, freshToken); err != nil {
		return err
	}
	f.Token = originalToken
	f.unsealKey = originalUnsealKey
	if err := f.unseal(ctx, httpClient); err != nil {
		return err
	}
	if err := f.StopContainerKeepAddress(ctx); err != nil {
		return err
	}
	if err := f.startContainer(ctx, f.image); err != nil {
		return err
	}
	if err := f.waitUntilEndpoint(ctx, 45*time.Second); err != nil {
		return err
	}
	httpClient, err = openbao.NewHTTPClient(f.CACertFile, openBaoTLSServerName, 30*time.Second)
	if err != nil {
		return err
	}
	if err := f.unseal(ctx, httpClient); err != nil {
		return err
	}
	return f.waitUntilReady(ctx, 45*time.Second)
}

func (f *OpenBaoEnvironment) stopContainer(ctx context.Context, clearName bool) error {
	if f.containerName == "" || f.dockerBinary == "" {
		return nil
	}

	cmd := exec.CommandContext(ctx, f.dockerBinary, "rm", "-f", f.containerName)
	output, err := cmd.CombinedOutput()
	if err != nil && !strings.Contains(string(output), "No such container") {
		return fmt.Errorf("remove OpenBao environment container: %w: %s", err, strings.TrimSpace(string(output)))
	}
	if clearName {
		f.containerName = ""
	}
	return nil
}

func defaultOpenBaoEnvironmentConfig(cfg OpenBaoEnvironmentConfig) OpenBaoEnvironmentConfig {
	if cfg.Image == "" {
		cfg.Image = EnvDefault(EnvOpenBaoImage, DefaultOpenBaoImage)
	}
	if cfg.TransitMount == "" {
		cfg.TransitMount = DefaultOpenBaoTransitMount
	}
	if cfg.TransitKey == "" {
		cfg.TransitKey = DefaultOpenBaoTransitKey
	}
	if cfg.StartupWait <= 0 {
		cfg.StartupWait = 45 * time.Second
	}
	if cfg.DockerBinary == "" {
		cfg.DockerBinary = EnvDefault(EnvDockerBinary, "docker")
	}
	if cfg.JWTTTL <= 0 {
		cfg.JWTTTL = openBaoJWTTTL
	}
	if cfg.JWTTokenTTL == "" {
		cfg.JWTTokenTTL = openBaoJWTTokenTTL
	}
	if cfg.JWTMaxTTL == "" {
		cfg.JWTMaxTTL = openBaoJWTTokenMaxTTL
	}
	return cfg
}

func resolveDockerBinary(binary string) (string, error) {
	if strings.Contains(binary, string(os.PathSeparator)) {
		if _, err := os.Stat(binary); err != nil {
			return "", fmt.Errorf("%w: %s", ErrDockerUnavailable, binary)
		}
		return binary, nil
	}
	path, err := exec.LookPath(binary)
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrDockerUnavailable, binary)
	}
	return path, nil
}

func checkDocker(ctx context.Context, dockerPath string) error {
	cmd := exec.CommandContext(ctx, dockerPath, "version", "--format", "{{.Server.Version}}")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", ErrDockerUnavailable, strings.TrimSpace(string(output)))
	}
	return nil
}

func (f *OpenBaoEnvironment) startContainer(ctx context.Context, image string) error {
	if f.storageVolume != "" {
		return f.startRaftStorageContainer(ctx, image)
	}
	return f.startDevContainer(ctx, image)
}

func (f *OpenBaoEnvironment) startDevContainer(ctx context.Context, image string) error {
	args := []string{
		"run",
		"--rm",
		"--detach",
		"--name", f.containerName,
		"--publish", "127.0.0.1::8200",
		"--volume", f.certDir + ":/bao/tls",
	}
	if f.networkName != "" {
		args = append(args, "--network", f.networkName)
	}
	args = append(args,
		image,
		"server",
		"-dev",
		"-dev-root-token-id="+f.Token,
		"-dev-listen-address="+openBaoListenAddress,
		"-dev-tls",
		"-dev-tls-cert-dir=/bao/tls",
	)
	cmd := exec.CommandContext(ctx, f.dockerBinary, args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("start OpenBao environment container: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (f *OpenBaoEnvironment) startRaftStorageContainer(ctx context.Context, image string) error {
	if err := writeOpenBaoServerTLSFiles(f.certDir); err != nil {
		return err
	}
	if err := writeOpenBaoRaftStorageConfig(f.certDir); err != nil {
		return err
	}
	if err := f.prepareStorageVolume(ctx, image); err != nil {
		return err
	}

	args := []string{
		"run",
		"--rm",
		"--detach",
		"--name", f.containerName,
		"--publish", "127.0.0.1::8200",
		"--volume", f.certDir + ":/bao/tls:ro",
		"--volume", f.storageVolume + ":/bao/data",
	}
	if f.networkName != "" {
		args = append(args, "--network", f.networkName)
	}
	args = append(args,
		image,
		"server",
		"-config=/bao/tls/openbao.hcl",
	)
	cmd := exec.CommandContext(ctx, f.dockerBinary, args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("start OpenBao raft-storage container: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (f *OpenBaoEnvironment) prepareStorageVolume(ctx context.Context, image string) error {
	cmd := exec.CommandContext(ctx, f.dockerBinary,
		"run", "--rm",
		"--entrypoint", "/bin/sh",
		"--volume", f.storageVolume+":/bao/data",
		image,
		"-c", "chown -R 100:1000 /bao/data",
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("prepare OpenBao storage volume: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func writeOpenBaoRaftStorageConfig(dir string) error {
	raw := `api_addr = "https://localhost:8200"
cluster_addr = "https://localhost:8201"

storage "raft" {
  path = "/bao/data"
  node_id = "node1"
}

listener "tcp" {
  address = "0.0.0.0:8200"
  cluster_address = "0.0.0.0:8201"
  tls_cert_file = "/bao/tls/server.crt"
  tls_key_file = "/bao/tls/server.key"
}
`
	if err := os.WriteFile(filepath.Join(dir, "openbao.hcl"), []byte(raw), 0o644); err != nil {
		return fmt.Errorf("write OpenBao raft storage config: %w", err)
	}
	return nil
}

func writeOpenBaoServerTLSFiles(dir string) error {
	if _, err := os.Stat(filepath.Join(dir, "ca.pem")); err == nil {
		if _, err := os.Stat(filepath.Join(dir, "server.crt")); err != nil {
			return fmt.Errorf("stat OpenBao TLS certificate: %w", err)
		}
		if _, err := os.Stat(filepath.Join(dir, "server.key")); err != nil {
			return fmt.Errorf("stat OpenBao TLS key: %w", err)
		}
		return nil
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("generate OpenBao TLS key: %w", err)
	}
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return fmt.Errorf("generate OpenBao TLS serial: %w", err)
	}
	now := time.Now().UTC()
	template := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName: openBaoTLSServerName,
		},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              []string{openBaoTLSServerName},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return fmt.Errorf("create OpenBao TLS certificate: %w", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	if err := os.WriteFile(filepath.Join(dir, "server.crt"), certPEM, 0o644); err != nil {
		return fmt.Errorf("write OpenBao TLS certificate: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ca.pem"), certPEM, 0o644); err != nil {
		return fmt.Errorf("write OpenBao TLS CA certificate: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "server.key"), keyPEM, 0o644); err != nil {
		return fmt.Errorf("write OpenBao TLS key: %w", err)
	}
	return nil
}

func (f *OpenBaoEnvironment) waitUntilEndpoint(ctx context.Context, timeout time.Duration) error {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	for {
		if err := f.refreshEndpoint(waitCtx); err == nil {
			httpClient, clientErr := openbao.NewHTTPClient(f.CACertFile, openBaoTLSServerName, 2*time.Second)
			if clientErr == nil {
				request, requestErr := http.NewRequestWithContext(waitCtx, http.MethodGet, f.Address+"/v1/sys/health", nil)
				if requestErr == nil {
					response, responseErr := httpClient.Do(request)
					if responseErr == nil {
						_, _ = io.Copy(io.Discard, response.Body)
						_ = response.Body.Close()
						return nil
					}
				}
			}
		}

		select {
		case <-waitCtx.Done():
			return fmt.Errorf("OpenBao environment endpoint did not become reachable: %w", waitCtx.Err())
		case <-ticker.C:
		}
	}
}

func (f *OpenBaoEnvironment) initializeRaftStorage(ctx context.Context) (bool, error) {
	httpClient, err := openbao.NewHTTPClient(f.CACertFile, openBaoTLSServerName, 30*time.Second)
	if err != nil {
		return false, err
	}
	initialized, err := f.isInitialized(ctx, httpClient)
	if err != nil {
		return false, err
	}
	initializedNow := false
	if !initialized {
		if err := f.initialize(ctx, httpClient); err != nil {
			return false, err
		}
		initializedNow = true
	}
	if f.unsealKey == "" {
		return false, fmt.Errorf("OpenBao raft-storage environment unseal key is not available")
	}
	if err := f.unseal(ctx, httpClient); err != nil {
		return false, err
	}
	return initializedNow, nil
}

func (f *OpenBaoEnvironment) SaveRaftSnapshot(ctx context.Context, snapshotPath string) error {
	if f.containerName == "" {
		return fmt.Errorf("OpenBao environment container is not running")
	}
	if snapshotPath == "" {
		return fmt.Errorf("OpenBao raft snapshot path is empty")
	}
	containerPath := "/tmp/openbao-raft.snap"
	args := []string{
		"exec",
		"--env", "BAO_ADDR=https://127.0.0.1:8200",
		"--env", "BAO_CACERT=/bao/tls/ca.pem",
		"--env", "BAO_TOKEN=" + f.Token,
		f.containerName,
		"bao", "operator", "raft", "snapshot", "save", containerPath,
	}
	cmd := exec.CommandContext(ctx, f.dockerBinary, args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("save OpenBao raft snapshot: %w: %s", err, strings.TrimSpace(string(output)))
	}
	cmd = exec.CommandContext(ctx, f.dockerBinary, "cp", f.containerName+":"+containerPath, snapshotPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("copy OpenBao raft snapshot: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (f *OpenBaoEnvironment) initializeForRestore(ctx context.Context, httpClient *http.Client) (string, string, error) {
	initialized, err := f.isInitialized(ctx, httpClient)
	if err != nil {
		return "", "", err
	}
	if initialized {
		return "", "", fmt.Errorf("OpenBao restore target is already initialized")
	}
	encoded, err := json.Marshal(initRequestBody{SecretShares: 1, SecretThreshold: 1})
	if err != nil {
		return "", "", fmt.Errorf("encode OpenBao restore init request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, f.Address+"/v1/sys/init", bytes.NewReader(encoded))
	if err != nil {
		return "", "", err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := httpClient.Do(request)
	if err != nil {
		return "", "", err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
	}()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", "", fmt.Errorf("OpenBao restore init returned %d", response.StatusCode)
	}
	var body initResponseBody
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		return "", "", fmt.Errorf("decode OpenBao restore init response: %w", err)
	}
	if body.RootToken == "" || len(body.KeysB64) == 0 || body.KeysB64[0] == "" {
		return "", "", fmt.Errorf("OpenBao restore init response was incomplete")
	}
	return body.RootToken, body.KeysB64[0], nil
}

func (f *OpenBaoEnvironment) copySnapshotIntoContainer(ctx context.Context, snapshotPath string) error {
	cmd := exec.CommandContext(ctx, f.dockerBinary, "cp", snapshotPath, f.containerName+":/tmp/openbao-raft.snap")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("copy OpenBao raft snapshot into restore container: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (f *OpenBaoEnvironment) restoreRaftSnapshotInContainer(ctx context.Context, token string) error {
	args := []string{
		"exec",
		"--env", "BAO_ADDR=https://127.0.0.1:8200",
		"--env", "BAO_CACERT=/bao/tls/ca.pem",
		"--env", "BAO_TOKEN=" + token,
		f.containerName,
		"bao", "operator", "raft", "snapshot", "restore", "-force", "/tmp/openbao-raft.snap",
	}
	cmd := exec.CommandContext(ctx, f.dockerBinary, args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("restore OpenBao raft snapshot: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (f *OpenBaoEnvironment) isInitialized(ctx context.Context, httpClient *http.Client) (bool, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, f.Address+"/v1/sys/init", nil)
	if err != nil {
		return false, err
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return false, err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
	}()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return false, fmt.Errorf("OpenBao init status returned %d", response.StatusCode)
	}
	var body initStatusResponseBody
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		return false, fmt.Errorf("decode OpenBao init status: %w", err)
	}
	return body.Initialized, nil
}

func (f *OpenBaoEnvironment) initialize(ctx context.Context, httpClient *http.Client) error {
	encoded, err := json.Marshal(initRequestBody{SecretShares: 1, SecretThreshold: 1})
	if err != nil {
		return fmt.Errorf("encode OpenBao init request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, f.Address+"/v1/sys/init", bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := httpClient.Do(request)
	if err != nil {
		return err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
	}()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("OpenBao init returned %d", response.StatusCode)
	}
	var body initResponseBody
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		return fmt.Errorf("decode OpenBao init response: %w", err)
	}
	if body.RootToken == "" || len(body.KeysB64) == 0 || body.KeysB64[0] == "" {
		return fmt.Errorf("OpenBao init response was incomplete")
	}
	f.Token = body.RootToken
	f.unsealKey = body.KeysB64[0]
	return nil
}

func (f *OpenBaoEnvironment) unseal(ctx context.Context, httpClient *http.Client) error {
	encoded, err := json.Marshal(unsealRequestBody{Key: f.unsealKey})
	if err != nil {
		return fmt.Errorf("encode OpenBao unseal request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, f.Address+"/v1/sys/unseal", bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := httpClient.Do(request)
	if err != nil {
		return err
	}
	body, readErr := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if readErr != nil {
		return fmt.Errorf("read OpenBao unseal response: %w", readErr)
	}
	if response.StatusCode == http.StatusBadRequest && bytes.Contains(body, []byte("already unsealed")) {
		return nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("OpenBao unseal returned %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func (f *OpenBaoEnvironment) waitUntilReady(ctx context.Context, timeout time.Duration) error {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	for {
		if err := f.refreshEndpoint(waitCtx); err == nil {
			if err := f.probeHealth(waitCtx); err == nil {
				return nil
			}
		}

		select {
		case <-waitCtx.Done():
			return fmt.Errorf("OpenBao environment did not become ready: %w", waitCtx.Err())
		case <-ticker.C:
		}
	}
}

func (f *OpenBaoEnvironment) refreshEndpoint(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, f.dockerBinary, "port", f.containerName, "8200/tcp")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("resolve OpenBao environment port: %w", err)
	}
	hostPort := strings.TrimSpace(string(output))
	if hostPort == "" {
		return fmt.Errorf("OpenBao environment port is not published yet")
	}
	lines := strings.Split(hostPort, "\n")
	_, port, err := net.SplitHostPort(strings.TrimSpace(lines[0]))
	if err != nil {
		return fmt.Errorf("parse OpenBao environment port: %w", err)
	}
	caFile, err := findOpenBaoEnvironmentCA(f.certDir)
	if err != nil {
		return err
	}
	f.Address = "https://localhost:" + port
	f.CACertFile = caFile
	return nil
}

func (f *OpenBaoEnvironment) probeHealth(ctx context.Context) error {
	httpClient, err := openbao.NewHTTPClient(f.CACertFile, openBaoTLSServerName, 2*time.Second)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, f.Address+"/v1/sys/health", nil)
	if err != nil {
		return err
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
	}()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("OpenBao environment health status %d", response.StatusCode)
	}
	return nil
}

func (f *OpenBaoEnvironment) bootstrapTransit(ctx context.Context) error {
	httpClient, err := openbao.NewHTTPClient(f.CACertFile, openBaoTLSServerName, 5*time.Second)
	if err != nil {
		return err
	}
	if err := f.write(ctx, httpClient, "sys/mounts/"+f.TransitMount, mountRequestBody{Type: "transit"}); err != nil {
		return err
	}
	if err := f.write(ctx, httpClient, f.TransitMount+"/keys/"+f.TransitKey, transitKeyRequestBody{Type: openBaoTransitKeyType}); err != nil {
		return err
	}
	if err := f.write(ctx, httpClient, f.TransitMount+"/config/keys", disableUpsertRequestBody{DisableUpsert: true}); err != nil {
		return err
	}
	return nil
}

func (f *OpenBaoEnvironment) bootstrapJWTAuth(ctx context.Context) error {
	httpClient, err := openbao.NewHTTPClient(f.CACertFile, openBaoTLSServerName, 5*time.Second)
	if err != nil {
		return err
	}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("generate JWT signing key: %w", err)
	}
	f.jwtPrivateKey = privateKey
	publicKeyPEM, err := jwtPublicKeyPEM(&privateKey.PublicKey)
	if err != nil {
		return err
	}
	jwtFile := filepath.Join(f.certDir, "identity.jwt")
	f.JWTFile = jwtFile
	if err := f.WriteJWTFile(time.Now().UTC(), f.jwtTTL); err != nil {
		return err
	}

	if err := f.write(ctx, httpClient, "sys/policies/acl/"+openBaoJWTPolicyName, policyRequestBody{
		Policy: f.providerPolicy(),
	}); err != nil {
		return err
	}
	if err := f.write(ctx, httpClient, "sys/auth/"+authMountName(f.AuthMount), mountRequestBody{Type: "jwt"}); err != nil {
		return err
	}
	if err := f.write(ctx, httpClient, path.Join(f.AuthMount, "config"), jwtAuthConfigRequestBody{
		JWTValidationPubKeys: []string{publicKeyPEM},
		BoundIssuer:          openBaoJWTIssuer,
	}); err != nil {
		return err
	}
	if err := f.write(ctx, httpClient, path.Join(f.AuthMount, "role", f.AuthRole), jwtAuthRoleRequestBody{
		RoleType:             "jwt",
		UserClaim:            "sub",
		BoundAudiences:       []string{openBaoJWTAudience},
		BoundSubject:         openBaoJWTSubject,
		TokenPolicies:        []string{openBaoJWTPolicyName},
		TokenTTL:             f.jwtTokenTTL,
		TokenMaxTTL:          f.jwtMaxTTL,
		TokenNoDefaultPolicy: true,
		ClockSkewLeeway:      openBaoJWTClockSkewLeeway,
		ExpirationLeeway:     openBaoJWTExpirationLeeway,
	}); err != nil {
		return err
	}
	return nil
}

func (f *OpenBaoEnvironment) providerPolicy() string {
	return fmt.Sprintf(`path %q {
  capabilities = ["read"]
}

path %q {
  capabilities = ["update"]
}

path %q {
  capabilities = ["update"]
}

path %q {
  capabilities = ["read"]
}

path "sys/capabilities-self" {
  capabilities = ["update"]
}
`,
		path.Join(f.TransitMount, "keys", f.TransitKey),
		path.Join(f.TransitMount, "encrypt", f.TransitKey),
		path.Join(f.TransitMount, "decrypt", f.TransitKey),
		path.Join(f.TransitMount, "config", "keys"),
	)
}

func authMountName(mountPath string) string {
	return strings.Trim(strings.TrimPrefix(strings.TrimSpace(mountPath), "auth/"), "/")
}

func jwtPublicKeyPEM(publicKey *rsa.PublicKey) (string, error) {
	encoded, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return "", fmt.Errorf("marshal JWT public key: %w", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: encoded})), nil
}

func signJWT(privateKey *rsa.PrivateKey, now time.Time, ttl time.Duration) (string, error) {
	header, err := encodeJWTHeader(jwtHeader{Algorithm: "RS256", Type: "JWT"})
	if err != nil {
		return "", err
	}
	claims, err := encodeJWTClaims(jwtClaims{
		Issuer:    openBaoJWTIssuer,
		Subject:   openBaoJWTSubject,
		Audience:  []string{openBaoJWTAudience},
		ExpiresAt: now.Add(ttl).Unix(),
		NotBefore: now.Add(-time.Minute).Unix(),
		IssuedAt:  now.Unix(),
	})
	if err != nil {
		return "", err
	}
	signingInput := header + "." + claims
	digest := sha256.Sum256([]byte(signingInput))
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, digest[:])
	if err != nil {
		return "", fmt.Errorf("sign JWT: %w", err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func encodeJWTHeader(value jwtHeader) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode JWT JSON: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

func encodeJWTClaims(value jwtClaims) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode JWT JSON: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

func (f *OpenBaoEnvironment) write(ctx context.Context, httpClient *http.Client, apiPath string, body environmentSetupPayload) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode OpenBao environment setup request: %w", err)
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		f.Address+"/v1/"+strings.TrimPrefix(apiPath, "/"),
		bytes.NewReader(encoded),
	)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Vault-Token", f.Token)
	response, err := httpClient.Do(request)
	if err != nil {
		return err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
	}()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("OpenBao environment setup %q status %d", apiPath, response.StatusCode)
	}
	return nil
}

func findOpenBaoEnvironmentCA(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".pem") {
			continue
		}
		if strings.Contains(strings.ToLower(name), "ca") {
			return filepath.Join(dir, name), nil
		}
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".pem") || strings.Contains(strings.ToLower(name), "key") {
			continue
		}
		return filepath.Join(dir, name), nil
	}
	return "", fmt.Errorf("OpenBao environment CA certificate was not generated")
}

func randomHex(bytesLen int) (string, error) {
	buf := make([]byte, bytesLen)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
