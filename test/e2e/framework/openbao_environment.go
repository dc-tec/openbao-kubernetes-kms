//go:build e2e

package framework

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
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
)

var ErrDockerUnavailable = errors.New("docker is not available")

type OpenBaoEnvironmentConfig struct {
	Image        string
	TransitMount string
	TransitKey   string
	StartupWait  time.Duration
	DockerBinary string
}

type OpenBaoEnvironment struct {
	Address      string
	CACertFile   string
	Token        string
	TransitMount string
	TransitKey   string

	containerName string
	certDir       string
	dockerBinary  string
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

type environmentSetupPayload interface {
	environmentSetupPayload()
}

func (mountRequestBody) environmentSetupPayload()         {}
func (disableUpsertRequestBody) environmentSetupPayload() {}
func (transitKeyRequestBody) environmentSetupPayload()    {}

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
		Token:         token,
		TransitMount:  cfg.TransitMount,
		TransitKey:    cfg.TransitKey,
		containerName: containerName,
		certDir:       certDir,
		dockerBinary:  dockerPath,
	}
	if err := environment.startContainer(ctx, cfg.Image); err != nil {
		_ = environment.Close(context.Background())
		return nil, err
	}
	if err := environment.waitUntilReady(ctx, cfg.StartupWait); err != nil {
		_ = environment.Close(context.Background())
		return nil, err
	}
	if err := environment.bootstrapTransit(ctx); err != nil {
		_ = environment.Close(context.Background())
		return nil, err
	}
	return environment, nil
}

func (f *OpenBaoEnvironment) NewClient() (*openbao.Client, error) {
	return openbao.NewClient(openbao.ClientConfig{
		Address:       f.Address,
		CACertFile:    f.CACertFile,
		TLSServerName: openBaoTLSServerName,
		Timeout:       5 * time.Second,
		TokenSource:   openbao.StaticTokenSource{TokenValue: f.Token},
	})
}

func (f *OpenBaoEnvironment) Close(ctx context.Context) error {
	var stopErr error
	if f.containerName != "" && f.dockerBinary != "" {
		cmd := exec.CommandContext(ctx, f.dockerBinary, "rm", "-f", f.containerName)
		if output, err := cmd.CombinedOutput(); err != nil {
			stopErr = fmt.Errorf("remove OpenBao environment container: %w: %s", err, strings.TrimSpace(string(output)))
		}
	}
	if !strings.EqualFold(os.Getenv(EnvSkipCleanup), "true") && f.certDir != "" {
		if err := os.RemoveAll(f.certDir); err != nil && stopErr == nil {
			stopErr = fmt.Errorf("remove OpenBao environment TLS directory: %w", err)
		}
	}
	return stopErr
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
	args := []string{
		"run",
		"--rm",
		"--detach",
		"--name", f.containerName,
		"--publish", "127.0.0.1::8200",
		"--volume", f.certDir + ":/bao/tls",
		image,
		"server",
		"-dev",
		"-dev-root-token-id=" + f.Token,
		"-dev-listen-address=" + openBaoListenAddress,
		"-dev-tls",
		"-dev-tls-cert-dir=/bao/tls",
	}
	cmd := exec.CommandContext(ctx, f.dockerBinary, args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("start OpenBao environment container: %w: %s", err, strings.TrimSpace(string(output)))
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
	if response.StatusCode >= http.StatusInternalServerError {
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
