package main

import (
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/auth"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/cli"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/config"
)

func TestInitOAuth2OmitsJWTFileAndMountsCredentialDirectory(t *testing.T) {
	values := strings.Replace(valuesWithLineage(), "source: file", `source: oauth2
    oauth2:
      tokenUrl: https://issuer.example/token
      clientId: kms-provider
      authMethod: client_secret_basic
      clientSecretFile: /etc/openbao-kms/credentials/client-secret`, 1)
	out := filepath.Join(t.TempDir(), "generated")
	_, err := executeCommand(t, "init", "--values", writeValues(t, values), "--out", out,
		"--model", "static-pod", "--image", initImage, "--socket-gid", "1234")
	if err != nil {
		t.Fatal(err)
	}
	rendered := readGenerated(t, filepath.Join(out, "config.yaml"))
	if strings.Contains(rendered, "jwtFile:") {
		t.Fatal("init injected a JWT file into OAuth configuration")
	}
	pod := readGenerated(t, filepath.Join(out, "bao-kms-provider.yaml"))
	if !strings.Contains(pod, "oauth2-credentials") || strings.Contains(pod, "identity.jwt") {
		t.Fatal("OAuth deployment mounts incorrect credentials")
	}
}

func TestOAuth2CommandWiringAndDiagnostics(t *testing.T) {
	cfg := loadCommandConfig(t)
	raw, err := os.ReadFile("../../test/testdata/auth/valid.jwt")
	if err != nil {
		t.Fatal(err)
	}
	claims, err := auth.ParseClaims(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	var rejected atomic.Bool
	var acquisitions atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/token" {
			acquisitions.Add(1)
			if rejected.Load() {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error_description":"private issuer detail"}`))
				return
			}
			// #nosec G117 -- test endpoint returns a synthetic JWT fixture through the token protocol.
			_ = json.NewEncoder(w).Encode(struct {
				AccessToken string `json:"access_token"`
				TokenType   string `json:"token_type"`
			}{strings.TrimSpace(string(raw)), "Bearer"})
			return
		}
		if r.URL.Path == "/v1/"+cfg.Auth.JWT.MountPath+"/login" {
			_, _ = w.Write([]byte(`{"auth":{"client_token":"test-openbao-token","lease_duration":3600,"renewable":false}}`))
			return
		}
		t.Error("unexpected authentication endpoint")
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)
	dir := t.TempDir()
	ca := filepath.Join(dir, "issuer-ca.pem")
	secret := filepath.Join(dir, "client-secret")
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	if err := os.WriteFile(ca, caPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secret, []byte("synthetic-client-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.OpenBao.Address = server.URL
	cfg.OpenBao.CACertFile = ca
	cfg.OpenBao.TLSServerName = "example.com"
	cfg.Auth.JWT.Source = config.JWTSourceOAuth2
	cfg.Auth.JWT.JWTFile = ""
	cfg.Auth.JWT.ExpectedIssuer = claims.Issuer
	cfg.Auth.JWT.ExpectedAudience = claims.Audience
	cfg.Auth.JWT.OAuth2 = config.OAuth2Config{
		TokenURL: server.URL + "/token", ClientID: "provider", ClientSecretFile: secret,
		AuthMethod: "client_secret_basic", CACertFile: ca,
	}
	report := cli.Report{Name: reportNameDoctor}
	if !checkLocalAuthForDoctor(t.Context(), &report, cfg) {
		t.Fatal("local OAuth credential check failed")
	}
	if _, ok := authenticateForDiagnostics(t.Context(), &report, cfg); !ok {
		t.Fatalf("OAuth diagnostic login failed: %v", report.Checks)
	}
	if acquisitions.Load() != 1 {
		t.Fatal("doctor acquired more than one token")
	}
	rejected.Store(true)
	report = cli.Report{Name: reportNameDoctor}
	if _, ok := authenticateForDiagnostics(t.Context(), &report, cfg); ok {
		t.Fatal("doctor accepted a rejected token request")
	}
	if !hasFailedAcquisition(report) || reportContains(report, "private issuer detail") {
		t.Fatal("doctor lost acquisition classification or leaked response data")
	}
}

func hasFailedAcquisition(report cli.Report) bool {
	for _, check := range report.Checks {
		if check.ID == "oauth2.acquire" && check.Status == cli.CheckFail {
			return true
		}
	}
	return false
}
