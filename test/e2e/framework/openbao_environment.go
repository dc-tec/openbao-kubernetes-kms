//go:build e2e

package framework

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
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
	"net/url"
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

	DefaultOpenBaoImage = "ghcr.io/openbao/openbao:2.5.5@sha256:6150c4a6b62067db6141c8da7a6a6b5763f4f47c315343d0c848b40fecdfd452"

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
	openBaoEndpointProbeWait   = 5 * time.Second

	openBaoCertAuthMount       = "auth/k8s-workload-a-cert"
	openBaoCertAuthRole        = "openbao-kms-control-plane"
	openBaoCertAuthSPIFFEID    = "spiffe://example.org/openbao-kms/workload-a"
	openBaoCertAuthTokenTTL    = "10m"
	openBaoCertAuthTokenMaxTTL = "30m"
)

var ErrDockerUnavailable = errors.New("docker is not available")

type OpenBaoEnvironmentConfig struct {
	Image                   string
	Namespace               string
	TransitMount            string
	TransitKey              string
	TransitKeyType          string
	StartupWait             time.Duration
	DockerBinary            string
	NetworkName             string
	StorageVolume           string
	JWTTTL                  time.Duration
	JWTTokenTTL             string
	JWTMaxTTL               string
	JWTIssuer               string
	JWTAudience             string
	JWTSubject              string
	CertAuth                bool
	CertAuthAliasNameSource string
}

type OpenBaoEnvironment struct {
	Address        string
	CACertFile     string
	TLSServerName  string
	Token          string
	TransitMount   string
	TransitKey     string
	Namespace      string
	AuthMount      string
	AuthRole       string
	JWTFile        string
	CertAuthMount  string
	CertAuthRole   string
	CertSPIFFEID   string
	ClientCertFile string
	ClientKeyFile  string

	image                   string
	containerName           string
	certDir                 string
	dockerBinary            string
	networkName             string
	storageVolume           string
	transitKeyType          string
	unsealKey               string
	jwtPrivateKey           *rsa.PrivateKey
	jwtPublicKey            string
	jwtPublicKeys           []string
	jwtTTL                  time.Duration
	jwtTokenTTL             string
	jwtMaxTTL               string
	jwtIssuer               string
	jwtAudience             string
	jwtSubject              string
	certAuthAliasNameSource string
	certAuth                bool
	managedStorageVolume    bool
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

type transitKeyConfigRequestBody struct {
	MinDecryptionVersion int `json:"min_decryption_version,omitempty"`
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

type certAuthConfigRequestBody struct {
	DisableBinding bool `json:"disable_binding"`
}

type certAuthRoleRequestBody struct {
	DisplayName     string   `json:"display_name"`
	Policies        []string `json:"policies"`
	Certificate     string   `json:"certificate"`
	AllowedURISANs  []string `json:"allowed_uri_sans,omitempty"`
	AliasNameSource string   `json:"alias_name_source,omitempty"`
	TTL             string   `json:"ttl"`
	MaxTTL          string   `json:"max_ttl"`
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

type JWTClaimsOptions struct {
	Issuer   string
	Subject  string
	Audience []string
}

type environmentSetupPayload interface {
	environmentSetupPayload()
}

func (mountRequestBody) environmentSetupPayload()            {}
func (disableUpsertRequestBody) environmentSetupPayload()    {}
func (transitKeyRequestBody) environmentSetupPayload()       {}
func (transitKeyConfigRequestBody) environmentSetupPayload() {}
func (policyRequestBody) environmentSetupPayload()           {}
func (emptyRequestBody) environmentSetupPayload()            {}
func (jwtAuthConfigRequestBody) environmentSetupPayload()    {}
func (jwtAuthRoleRequestBody) environmentSetupPayload()      {}
func (certAuthConfigRequestBody) environmentSetupPayload()   {}
func (certAuthRoleRequestBody) environmentSetupPayload()     {}

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
	// OpenBao dev TLS material is generated inside Docker; rootless or userns-remapped
	// runners need write access to this host-mounted test directory.
	// #nosec G302 -- e2e Docker container needs write access to generated dev TLS files.
	if err := os.Chmod(certDir, 0o777); err != nil {
		_ = os.RemoveAll(certDir)
		return nil, fmt.Errorf("prepare OpenBao environment TLS directory permissions: %w", err)
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
	managedStorageVolume := false
	if cfg.CertAuth && cfg.StorageVolume == "" {
		cfg.StorageVolume = "bao-kms-e2e-certauth-" + suffix
		managedStorageVolume = true
		if err := createOpenBaoStorageVolume(ctx, dockerPath, cfg.StorageVolume); err != nil {
			_ = os.RemoveAll(certDir)
			return nil, err
		}
	}

	environment := &OpenBaoEnvironment{
		TLSServerName:           openBaoTLSServerName,
		Token:                   token,
		Namespace:               cfg.Namespace,
		TransitMount:            cfg.TransitMount,
		TransitKey:              cfg.TransitKey,
		AuthMount:               openBaoJWTAuthMount,
		AuthRole:                openBaoJWTAuthRole,
		CertAuthMount:           openBaoCertAuthMount,
		CertAuthRole:            openBaoCertAuthRole,
		CertSPIFFEID:            openBaoCertAuthSPIFFEID,
		image:                   cfg.Image,
		containerName:           containerName,
		certDir:                 certDir,
		dockerBinary:            dockerPath,
		networkName:             cfg.NetworkName,
		storageVolume:           cfg.StorageVolume,
		transitKeyType:          cfg.TransitKeyType,
		jwtTTL:                  cfg.JWTTTL,
		jwtTokenTTL:             cfg.JWTTokenTTL,
		jwtMaxTTL:               cfg.JWTMaxTTL,
		jwtIssuer:               cfg.JWTIssuer,
		jwtAudience:             cfg.JWTAudience,
		jwtSubject:              cfg.JWTSubject,
		certAuthAliasNameSource: cfg.CertAuthAliasNameSource,
		certAuth:                cfg.CertAuth,
		managedStorageVolume:    managedStorageVolume,
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
		if err := environment.bootstrapNamespace(ctx); err != nil {
			_ = environment.Close(context.Background())
			return nil, err
		}
		if err := environment.bootstrapTransit(ctx); err != nil {
			_ = environment.Close(context.Background())
			return nil, err
		}
		if err := environment.bootstrapJWTAuth(ctx); err != nil {
			_ = environment.Close(context.Background())
			return nil, err
		}
		if err := environment.bootstrapCertAuth(ctx); err != nil {
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
		Namespace:     f.Namespace,
	})
}

func (f *OpenBaoEnvironment) NewAuthClient() (*openbao.AuthClient, error) {
	return openbao.NewAuthClient(openbao.AuthClientConfig{
		Address:       f.Address,
		CACertFile:    f.CACertFile,
		TLSServerName: f.TLSServerName,
		Timeout:       5 * time.Second,
		Namespace:     f.Namespace,
	})
}

func (f *OpenBaoEnvironment) NewCertAuthClient() (*openbao.AuthClient, error) {
	if f.ClientCertFile == "" || f.ClientKeyFile == "" {
		return nil, fmt.Errorf("OpenBao environment cert-auth client certificate is not initialized")
	}
	cert, err := tls.LoadX509KeyPair(f.ClientCertFile, f.ClientKeyFile)
	if err != nil {
		return nil, fmt.Errorf("load OpenBao cert-auth client certificate: %w", err)
	}
	return openbao.NewAuthClient(openbao.AuthClientConfig{
		Address:       f.Address,
		CACertFile:    f.CACertFile,
		TLSServerName: f.TLSServerName,
		Timeout:       5 * time.Second,
		Namespace:     f.Namespace,
		GetClientCertificate: func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
			return &cert, nil
		},
	})
}

func (f *OpenBaoEnvironment) ConfigureCertAuthRole(
	ctx context.Context,
	certificatePEM []byte,
	allowedURISAN string,
) error {
	if len(certificatePEM) == 0 {
		return fmt.Errorf("OpenBao cert-auth CA certificate is required")
	}
	if strings.TrimSpace(allowedURISAN) == "" {
		return fmt.Errorf("OpenBao cert-auth allowed URI SAN is required")
	}
	httpClient, err := openbao.NewHTTPClient(f.CACertFile, openBaoTLSServerName, 5*time.Second)
	if err != nil {
		return err
	}
	return f.write(ctx, httpClient, path.Join(f.CertAuthMount, "certs", f.CertAuthRole), certAuthRoleRequestBody{
		DisplayName:     f.CertAuthRole,
		Policies:        []string{openBaoJWTPolicyName},
		Certificate:     string(certificatePEM),
		AllowedURISANs:  []string{allowedURISAN},
		AliasNameSource: f.certAuthAliasNameSource,
		TTL:             openBaoCertAuthTokenTTL,
		MaxTTL:          openBaoCertAuthTokenMaxTTL,
	})
}

func (f *OpenBaoEnvironment) WriteJWTFile(now time.Time, ttl time.Duration) error {
	return f.WriteJWTFileAt(f.JWTFile, now, ttl)
}

func (f *OpenBaoEnvironment) WriteJWTFileAt(filePath string, now time.Time, ttl time.Duration) error {
	return f.WriteJWTFileAtWithClaims(filePath, now, ttl, JWTClaimsOptions{})
}

func (f *OpenBaoEnvironment) WriteJWTFileWithClaims(now time.Time, ttl time.Duration, opts JWTClaimsOptions) error {
	return f.WriteJWTFileAtWithClaims(f.JWTFile, now, ttl, opts)
}

func (f *OpenBaoEnvironment) WriteJWTFileAtWithClaims(
	filePath string,
	now time.Time,
	ttl time.Duration,
	opts JWTClaimsOptions,
) error {
	jwt, err := f.IssueJWT(now, ttl, opts)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filePath, []byte(jwt), 0o600); err != nil {
		return fmt.Errorf("write OpenBao environment JWT file: %w", err)
	}
	return nil
}

func (f *OpenBaoEnvironment) IssueJWT(now time.Time, ttl time.Duration, opts JWTClaimsOptions) (string, error) {
	if f.jwtPrivateKey == nil {
		return "", fmt.Errorf("OpenBao environment JWT signer is not initialized")
	}
	claims := f.jwtClaims(now, ttl, opts)
	jwt, err := signJWT(f.jwtPrivateKey, claims)
	if err != nil {
		return "", err
	}
	return jwt, nil
}

func (f *OpenBaoEnvironment) JWTIssuer() string {
	return f.jwtIssuer
}

func (f *OpenBaoEnvironment) JWTAudience() string {
	return f.jwtAudience
}

func (f *OpenBaoEnvironment) JWTSubject() string {
	return f.jwtSubject
}

func (f *OpenBaoEnvironment) RotateJWTSigningKey(ctx context.Context, keepPrevious bool) error {
	privateKey, publicKeyPEM, err := generateJWTSigningKey()
	if err != nil {
		return err
	}
	f.jwtPrivateKey = privateKey
	f.jwtPublicKey = publicKeyPEM
	if keepPrevious {
		f.jwtPublicKeys = append(f.jwtPublicKeys, publicKeyPEM)
	} else {
		f.jwtPublicKeys = []string{publicKeyPEM}
	}
	return f.writeJWTAuthConfig(ctx)
}

func (f *OpenBaoEnvironment) PrunePreviousJWTSigningKeys(ctx context.Context) error {
	if f.jwtPublicKey == "" {
		return fmt.Errorf("OpenBao environment current JWT public key is not initialized")
	}
	f.jwtPublicKeys = []string{f.jwtPublicKey}
	return f.writeJWTAuthConfig(ctx)
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
	return f.writeRoot(ctx, httpClient, "sys/seal", emptyRequestBody{})
}

func (f *OpenBaoEnvironment) RotateTransitKey(ctx context.Context) error {
	httpClient, err := openbao.NewHTTPClient(f.CACertFile, openBaoTLSServerName, 5*time.Second)
	if err != nil {
		return err
	}
	return f.write(ctx, httpClient, path.Join(f.TransitMount, "keys", f.TransitKey, "rotate"), emptyRequestBody{})
}

func (f *OpenBaoEnvironment) SetTransitMinDecryptionVersion(ctx context.Context, version int) error {
	if version <= 0 {
		return fmt.Errorf("OpenBao Transit min_decryption_version must be positive")
	}
	httpClient, err := openbao.NewHTTPClient(f.CACertFile, openBaoTLSServerName, 5*time.Second)
	if err != nil {
		return err
	}
	return f.write(ctx, httpClient, path.Join(f.TransitMount, "keys", f.TransitKey, "config"), transitKeyConfigRequestBody{
		MinDecryptionVersion: version,
	})
}

func (f *OpenBaoEnvironment) Close(ctx context.Context) error {
	stopErr := f.StopContainer(ctx)
	skipCleanup := strings.EqualFold(os.Getenv(EnvSkipCleanup), "true")
	if !skipCleanup && f.certDir != "" {
		if err := os.RemoveAll(f.certDir); err != nil && stopErr == nil {
			stopErr = fmt.Errorf("remove OpenBao environment TLS directory: %w", err)
		}
	}
	if !skipCleanup && f.managedStorageVolume && f.storageVolume != "" {
		if err := removeOpenBaoStorageVolume(ctx, f.dockerBinary, f.storageVolume); err != nil && stopErr == nil {
			stopErr = err
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
	if cfg.TransitKeyType == "" {
		cfg.TransitKeyType = openBaoTransitKeyType
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
	if cfg.JWTIssuer == "" {
		cfg.JWTIssuer = openBaoJWTIssuer
	}
	if cfg.JWTAudience == "" {
		cfg.JWTAudience = openBaoJWTAudience
	}
	if cfg.JWTSubject == "" {
		cfg.JWTSubject = openBaoJWTSubject
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
	if f.certAuth {
		clientCertFile, clientKeyFile, err := writeOpenBaoClientTLSFiles(f.certDir, f.CertSPIFFEID)
		if err != nil {
			return err
		}
		f.ClientCertFile = clientCertFile
		f.ClientKeyFile = clientKeyFile
	}
	if err := writeOpenBaoRaftStorageConfig(f.certDir, f.certAuth); err != nil {
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
	return prepareOpenBaoStorageVolume(ctx, f.dockerBinary, image, f.storageVolume)
}

func prepareOpenBaoStorageVolume(ctx context.Context, dockerBinary string, image string, storageVolume string) error {
	cmd := exec.CommandContext(ctx, dockerBinary,
		"run", "--rm",
		"--entrypoint", "/bin/sh",
		"--volume", storageVolume+":/bao/data",
		image,
		"-c", "chown -R 100:1000 /bao/data",
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("prepare OpenBao storage volume: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func createOpenBaoStorageVolume(ctx context.Context, dockerBinary string, storageVolume string) error {
	cmd := exec.CommandContext(ctx, dockerBinary, "volume", "create", storageVolume)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("create OpenBao storage volume: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func removeOpenBaoStorageVolume(ctx context.Context, dockerBinary string, storageVolume string) error {
	cmd := exec.CommandContext(ctx, dockerBinary, "volume", "rm", "-f", storageVolume)
	if output, err := cmd.CombinedOutput(); err != nil && !strings.Contains(string(output), "No such volume") {
		return fmt.Errorf("remove OpenBao storage volume: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func writeOpenBaoRaftStorageConfig(dir string, requestClientCerts bool) error {
	clientCertConfig := ""
	if requestClientCerts {
		clientCertConfig = `  tls_disable_client_certs = false
`
	}
	raw := fmt.Sprintf(`disable_mlock = true
api_addr = "https://localhost:8200"
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
%s
}
`, clientCertConfig)
	if err := os.WriteFile(filepath.Join(dir, "openbao.hcl"), []byte(raw), 0o644); err != nil {
		return fmt.Errorf("write OpenBao raft storage config: %w", err)
	}
	return nil
}

func writeOpenBaoServerTLSFiles(dir string) error {
	return writeOpenBaoServerTLSFilesForHosts(dir, []string{openBaoTLSServerName})
}

func writeOpenBaoServerTLSFilesForHosts(dir string, dnsNames []string) error {
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
		DNSNames:              uniqueDNSNames(append(dnsNames, openBaoTLSServerName)),
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

func writeOpenBaoClientTLSFiles(dir string, spiffeID string) (string, string, error) {
	caFile := filepath.Join(dir, "client-ca.pem")
	certFile := filepath.Join(dir, "client.crt")
	keyFile := filepath.Join(dir, "client.key")
	if _, err := os.Stat(caFile); err == nil {
		if _, err := os.Stat(certFile); err != nil {
			return "", "", fmt.Errorf("stat OpenBao client certificate: %w", err)
		}
		if _, err := os.Stat(keyFile); err != nil {
			return "", "", fmt.Errorf("stat OpenBao client key: %w", err)
		}
		return certFile, keyFile, nil
	}

	parsedSPIFFEID, err := url.Parse(spiffeID)
	if err != nil {
		return "", "", fmt.Errorf("parse OpenBao cert-auth SPIFFE ID: %w", err)
	}
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", fmt.Errorf("generate OpenBao client CA key: %w", err)
	}
	clientKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", fmt.Errorf("generate OpenBao client key: %w", err)
	}
	now := time.Now().UTC()
	caTemplate, err := certificateTemplate("openbao-kms-e2e-client-ca", now)
	if err != nil {
		return "", "", err
	}
	caTemplate.IsCA = true
	caTemplate.KeyUsage = x509.KeyUsageCertSign | x509.KeyUsageCRLSign

	clientTemplate, err := certificateTemplate(openBaoCertAuthRole, now)
	if err != nil {
		return "", "", err
	}
	clientTemplate.KeyUsage = x509.KeyUsageDigitalSignature
	clientTemplate.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	clientTemplate.URIs = []*url.URL{parsedSPIFFEID}

	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return "", "", fmt.Errorf("create OpenBao client CA certificate: %w", err)
	}
	clientDER, err := x509.CreateCertificate(rand.Reader, clientTemplate, caTemplate, &clientKey.PublicKey, caKey)
	if err != nil {
		return "", "", fmt.Errorf("create OpenBao client certificate: %w", err)
	}
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: clientDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(clientKey)})
	if err := os.WriteFile(caFile, caPEM, 0o644); err != nil {
		return "", "", fmt.Errorf("write OpenBao client CA certificate: %w", err)
	}
	if err := os.WriteFile(certFile, certPEM, 0o644); err != nil {
		return "", "", fmt.Errorf("write OpenBao client certificate: %w", err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		return "", "", fmt.Errorf("write OpenBao client key: %w", err)
	}
	return certFile, keyFile, nil
}

func certificateTemplate(commonName string, now time.Time) (*x509.Certificate, error) {
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return nil, fmt.Errorf("generate OpenBao TLS serial: %w", err)
	}
	return &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName: commonName,
		},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(24 * time.Hour),
		BasicConstraintsValid: true,
	}, nil
}

func uniqueDNSNames(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	names := make([]string, 0, len(values))
	for _, value := range values {
		name := strings.TrimSpace(value)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	return names
}

func (f *OpenBaoEnvironment) waitUntilEndpoint(ctx context.Context, timeout time.Duration) error {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	var lastErr error
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
					lastErr = responseErr
				} else {
					lastErr = requestErr
				}
			} else {
				lastErr = clientErr
			}
		} else {
			lastErr = err
		}

		select {
		case <-waitCtx.Done():
			return openBaoReadinessTimeoutError(
				"OpenBao environment endpoint did not become reachable",
				waitCtx.Err(),
				lastErr,
			)
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

	var lastErr error
	for {
		if err := f.refreshEndpoint(waitCtx); err == nil {
			if err := f.probeHealth(waitCtx); err == nil {
				return nil
			} else {
				lastErr = err
			}
		} else {
			lastErr = err
		}

		select {
		case <-waitCtx.Done():
			return openBaoReadinessTimeoutError(
				"OpenBao environment did not become ready",
				waitCtx.Err(),
				lastErr,
			)
		case <-ticker.C:
		}
	}
}

func (f *OpenBaoEnvironment) refreshEndpoint(ctx context.Context) error {
	probeCtx, cancel := context.WithTimeout(ctx, openBaoEndpointProbeWait)
	defer cancel()

	cmd := exec.CommandContext(probeCtx, f.dockerBinary, "port", f.containerName, "8200/tcp")
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
	f.Address = "https://" + net.JoinHostPort("127.0.0.1", port)
	f.CACertFile = caFile
	return nil
}

func openBaoReadinessTimeoutError(message string, timeoutErr error, lastErr error) error {
	if lastErr == nil {
		return fmt.Errorf("%s: %w", message, timeoutErr)
	}
	return fmt.Errorf("%s: %w (last readiness error: %v)", message, timeoutErr, lastErr)
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

func (f *OpenBaoEnvironment) bootstrapNamespace(ctx context.Context) error {
	if f.Namespace == "" {
		return nil
	}
	httpClient, err := openbao.NewHTTPClient(f.CACertFile, openBaoTLSServerName, 5*time.Second)
	if err != nil {
		return err
	}
	for _, namespace := range namespacePrefixes(f.Namespace) {
		if err := f.writeRoot(ctx, httpClient, "sys/namespaces/"+namespace, emptyRequestBody{}); err != nil {
			return err
		}
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
	if err := f.write(ctx, httpClient, f.TransitMount+"/keys/"+f.TransitKey, transitKeyRequestBody{Type: f.transitKeyType}); err != nil {
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
	privateKey, publicKeyPEM, err := generateJWTSigningKey()
	if err != nil {
		return err
	}
	f.jwtPrivateKey = privateKey
	f.jwtPublicKey = publicKeyPEM
	f.jwtPublicKeys = []string{publicKeyPEM}
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
	if err := f.writeJWTAuthConfig(ctx); err != nil {
		return err
	}
	if err := f.write(ctx, httpClient, path.Join(f.AuthMount, "role", f.AuthRole), jwtAuthRoleRequestBody{
		RoleType:             "jwt",
		UserClaim:            "sub",
		BoundAudiences:       []string{f.jwtAudience},
		BoundSubject:         f.jwtSubject,
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

func (f *OpenBaoEnvironment) bootstrapCertAuth(ctx context.Context) error {
	if !f.certAuth {
		return nil
	}
	httpClient, err := openbao.NewHTTPClient(f.CACertFile, openBaoTLSServerName, 5*time.Second)
	if err != nil {
		return err
	}
	caPEM, err := os.ReadFile(filepath.Join(f.certDir, "client-ca.pem"))
	if err != nil {
		return fmt.Errorf("read OpenBao cert-auth client CA: %w", err)
	}
	if err := f.write(ctx, httpClient, "sys/auth/"+authMountName(f.CertAuthMount), mountRequestBody{Type: "cert"}); err != nil {
		return err
	}
	if err := f.write(ctx, httpClient, path.Join(f.CertAuthMount, "config"), certAuthConfigRequestBody{
		DisableBinding: false,
	}); err != nil {
		return err
	}
	return f.ConfigureCertAuthRole(ctx, caPEM, f.CertSPIFFEID)
}

func (f *OpenBaoEnvironment) writeJWTAuthConfig(ctx context.Context) error {
	httpClient, err := openbao.NewHTTPClient(f.CACertFile, openBaoTLSServerName, 5*time.Second)
	if err != nil {
		return err
	}
	return f.write(ctx, httpClient, path.Join(f.AuthMount, "config"), jwtAuthConfigRequestBody{
		JWTValidationPubKeys: f.jwtPublicKeys,
		BoundIssuer:          f.jwtIssuer,
	})
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

func generateJWTSigningKey() (*rsa.PrivateKey, string, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, "", fmt.Errorf("generate JWT signing key: %w", err)
	}
	publicKeyPEM, err := jwtPublicKeyPEM(&privateKey.PublicKey)
	if err != nil {
		return nil, "", err
	}
	return privateKey, publicKeyPEM, nil
}

func jwtPublicKeyPEM(publicKey *rsa.PublicKey) (string, error) {
	encoded, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return "", fmt.Errorf("marshal JWT public key: %w", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: encoded})), nil
}

func (f *OpenBaoEnvironment) jwtClaims(now time.Time, ttl time.Duration, opts JWTClaimsOptions) jwtClaims {
	issuer := opts.Issuer
	if issuer == "" {
		issuer = f.jwtIssuer
	}
	subject := opts.Subject
	if subject == "" {
		subject = f.jwtSubject
	}
	audience := opts.Audience
	if len(audience) == 0 {
		audience = []string{f.jwtAudience}
	}
	return jwtClaims{
		Issuer:    issuer,
		Subject:   subject,
		Audience:  audience,
		ExpiresAt: now.Add(ttl).Unix(),
		NotBefore: now.Add(-time.Minute).Unix(),
		IssuedAt:  now.Unix(),
	}
}

func signJWT(privateKey *rsa.PrivateKey, claims jwtClaims) (string, error) {
	header, err := encodeJWTHeader(jwtHeader{Algorithm: "RS256", Type: "JWT"})
	if err != nil {
		return "", err
	}
	encodedClaims, err := encodeJWTClaims(claims)
	if err != nil {
		return "", err
	}
	signingInput := header + "." + encodedClaims
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
	return f.writeWithNamespace(ctx, httpClient, apiPath, body, f.Namespace)
}

func (f *OpenBaoEnvironment) writeRoot(ctx context.Context, httpClient *http.Client, apiPath string, body environmentSetupPayload) error {
	return f.writeWithNamespace(ctx, httpClient, apiPath, body, "")
}

func (f *OpenBaoEnvironment) writeWithNamespace(
	ctx context.Context,
	httpClient *http.Client,
	apiPath string,
	body environmentSetupPayload,
	namespace string,
) error {
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
	if namespace != "" {
		request.Header.Set("X-Vault-Namespace", namespace)
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return err
	}
	responseBody, readErr := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if readErr != nil {
		return fmt.Errorf("read OpenBao environment setup response: %w", readErr)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf(
			"OpenBao environment setup %q status %d: %s",
			apiPath,
			response.StatusCode,
			strings.TrimSpace(string(responseBody)),
		)
	}
	return nil
}

func namespacePrefixes(namespace string) []string {
	segments := strings.Split(namespace, "/")
	prefixes := make([]string, 0, len(segments))
	for i := range segments {
		prefixes = append(prefixes, strings.Join(segments[:i+1], "/"))
	}
	return prefixes
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
