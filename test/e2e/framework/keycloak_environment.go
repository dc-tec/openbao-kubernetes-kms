//go:build e2e

package framework

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/openbao"
)

const (
	EnvKeycloakImage = "E2E_KEYCLOAK_IMAGE"
	//nolint:lll // Keep the complete image pin visible to version-policy checks.
	DefaultKeycloakImage = "quay.io/keycloak/keycloak:26.6.3@sha256:9b0330756022422149aa6502eb2def8cd47c6e1b000c7c65cdb13e7c0133e992"
	keycloakClientUUID   = "d12811dc-0458-4fcb-ab31-dda7479d60bf"
	keycloakSubject      = "e1749286-52b7-4a0e-adfa-c39d53f51964"
)

// KeycloakEnvironment is an isolated HTTPS issuer outside the protected cluster.
// All credentials are ephemeral test data and must stay out of test output.
type KeycloakEnvironment struct {
	Issuer, TokenURL, ClientID, ClientSecret, Audience, Subject, CACertFile string
	container, docker, dir, address, adminPassword                          string
	http                                                                    *http.Client
}

func StartKeycloakEnvironment(ctx context.Context, network string) (_ *KeycloakEnvironment, resultErr error) {
	dir, err := os.MkdirTemp("", "bao-kms-keycloak-")
	if err != nil {
		return nil, err
	}
	f := &KeycloakEnvironment{
		dir: dir, docker: EnvDefault(EnvDockerBinary, "docker"),
		container: fmt.Sprintf("obk-keycloak-%d", time.Now().UnixNano()),
		ClientID:  "kms-provider", Audience: "bao-kms-provider", Subject: keycloakSubject,
	}
	defer func() {
		if resultErr != nil {
			cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			_ = f.Close(cleanup)
		}
	}()
	credential := make([]byte, 32)
	if _, err := rand.Read(credential); err != nil {
		return nil, err
	}
	f.ClientSecret = hex.EncodeToString(credential)
	if _, err := rand.Read(credential); err != nil {
		return nil, err
	}
	f.adminPassword = hex.EncodeToString(credential)
	f.Issuer = "https://" + f.container + ":8443/realms/kms-e2e"
	f.TokenURL = f.Issuer + "/protocol/openid-connect/token"
	f.CACertFile = filepath.Join(dir, "ca.pem")
	if err := writeOpenBaoServerTLSFilesForHosts(dir, []string{f.container}); err != nil {
		return nil, err
	}
	if err := f.writeRealm(); err != nil {
		return nil, err
	}
	envFile := filepath.Join(dir, "admin.env")
	if err := os.WriteFile(
		envFile,
		[]byte("KC_BOOTSTRAP_ADMIN_USERNAME=admin\nKC_BOOTSTRAP_ADMIN_PASSWORD="+f.adminPassword+"\n"),
		0o600,
	); err != nil {
		return nil, err
	}
	// #nosec G204 -- test harness selects Docker and generated fixture arguments without a shell.
	_, err = exec.CommandContext(ctx, f.docker, "run", "--detach", "--name", f.container,
		"--network", network, "--user", "0:0", "--publish", "127.0.0.1::8443", "--env-file", envFile,
		"--volume", dir+":/fixture:ro",
		"--volume", filepath.Join(dir, "kms-e2e-realm.json")+":/opt/keycloak/data/import/kms-e2e-realm.json:ro",
		EnvDefault(EnvKeycloakImage, DefaultKeycloakImage), "start-dev", "--import-realm", "--http-enabled=false",
		"--https-certificate-file=/fixture/server.crt", "--https-certificate-key-file=/fixture/server.key",
		"--hostname=https://"+f.container+":8443").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("start Keycloak container: %w", err)
	}
	// #nosec G204 -- query the generated fixture container through the configured Docker binary.
	port, err := exec.CommandContext(ctx, f.docker, "port", f.container, "8443/tcp").Output()
	if err != nil {
		return nil, err
	}
	f.address = "https://" + strings.TrimSpace(string(port))
	f.http, err = openbao.NewHTTPClient(f.CACertFile, "localhost", 5*time.Second)
	if err != nil {
		return nil, err
	}
	if err := f.WaitReady(ctx); err != nil {
		return nil, err
	}
	return f, nil
}

func (f *KeycloakEnvironment) WaitReady(ctx context.Context) error {
	deadline, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	for {
		req, err := http.NewRequestWithContext(deadline, http.MethodGet,
			f.address+"/realms/kms-e2e/.well-known/openid-configuration", nil)
		if err != nil {
			return err
		}
		// #nosec G704 -- requests target the generated local HTTPS fixture; redirects are disabled.
		response, err := f.http.Do(req)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return nil
			}
		}
		select {
		case <-deadline.Done():
			return errors.New("keycloak discovery did not become ready")
		case <-time.After(time.Second):
		}
	}
}

// RotateClientSecret invalidates the previous secret and returns its replacement.
func (f *KeycloakEnvironment) RotateClientSecret(ctx context.Context) (string, error) {
	form := url.Values{
		"grant_type": {"password"}, "client_id": {"admin-cli"},
		"username": {"admin"}, "password": {f.adminPassword},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		f.address+"/realms/master/protocol/openid-connect/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	content, err := f.request(req)
	if err != nil {
		return "", err
	}
	var token struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(content, &token); err != nil || token.AccessToken == "" {
		return "", errors.New("keycloak admin token unavailable")
	}
	req, err = http.NewRequestWithContext(ctx, http.MethodPost,
		f.address+"/admin/realms/kms-e2e/clients/"+keycloakClientUUID+"/client-secret", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	content, err = f.request(req)
	if err != nil {
		return "", err
	}
	var secret struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(content, &secret); err != nil || secret.Value == "" {
		return "", errors.New("keycloak rotated credential unavailable")
	}
	f.ClientSecret = secret.Value
	return secret.Value, nil
}

func (f *KeycloakEnvironment) request(req *http.Request) ([]byte, error) {
	// #nosec G704 -- request targets the isolated local HTTPS issuer; the client rejects redirects.
	response, err := f.http.Do(req)
	if err != nil {
		return nil, errors.New("keycloak request failed")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("keycloak request rejected: HTTP %d", response.StatusCode)
	}
	return io.ReadAll(io.LimitReader(response.Body, 1<<20))
}

func (f *KeycloakEnvironment) Stop(ctx context.Context) error {
	// #nosec G204 -- the test harness selects the executable and fixture arguments; no shell expansion is used.
	return exec.CommandContext(ctx, f.docker, "stop", "--time", "10", f.container).Run()
}

func (f *KeycloakEnvironment) Start(ctx context.Context) error {
	// #nosec G204 -- the test harness selects the executable and fixture arguments; no shell expansion is used.
	if err := exec.CommandContext(ctx, f.docker, "start", f.container).Run(); err != nil {
		return err
	}
	// #nosec G204 -- the test harness selects the executable and fixture arguments; no shell expansion is used.
	port, err := exec.CommandContext(ctx, f.docker, "port", f.container, "8443/tcp").Output()
	if err != nil {
		return err
	}
	f.address = "https://" + strings.TrimSpace(string(port))
	return f.WaitReady(ctx)
}

func (f *KeycloakEnvironment) Close(ctx context.Context) error {
	if f.http != nil {
		f.http.CloseIdleConnections()
	}
	// #nosec G204 -- executable and fixture arguments come from the test harness, including fixed setup scripts.
	removeErr := exec.CommandContext(ctx, f.docker, "rm", "--force", f.container).Run()
	return errors.Join(removeErr, os.RemoveAll(f.dir))
}
