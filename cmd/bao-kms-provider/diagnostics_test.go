package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/auth"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/cli"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/config"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/kmsv2"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/openbao"
)

func TestTransitDiagnosticsFlagsDangerousKeyProfile(t *testing.T) {
	cfg := loadCommandConfig(t)
	client := fakeDiagnosticTransitClient{
		profile: commandTestProfile(func(profile *openbao.KeyProfile) {
			profile.Exportable = true
		}),
		disableUpsert: true,
		capabilities:  hotPathCapabilities(cfg),
	}
	report := cli.Report{Name: "doctor"}

	_, ok := runTransitDiagnostics(
		context.Background(),
		&report,
		cfg,
		client,
		true,
	)
	if ok {
		t.Fatal("expected unsafe profile to prevent key ID diagnostics")
	}
	if !report.HasFailures() {
		t.Fatal("expected unsafe profile to fail diagnostics")
	}
	if !reportContains(report, "key material export is enabled") {
		t.Fatalf("expected export finding in report: %#v", report.Checks)
	}
	if !reportContains(report, "cryptographic_safety") {
		t.Fatalf("expected finding impact in report: %#v", report.Checks)
	}
}

func TestTransitDiagnosticsPassesWithSafeFakeOpenBao(t *testing.T) {
	cfg := loadCommandConfig(t)
	client := fakeDiagnosticTransitClient{
		profile:       commandTestProfile(nil),
		disableUpsert: true,
		capabilities:  hotPathCapabilities(cfg),
	}
	report := cli.Report{Name: "doctor"}

	_, ok := runTransitDiagnostics(
		context.Background(),
		&report,
		cfg,
		client,
		true,
	)
	if !ok {
		t.Fatalf("expected safe diagnostics to produce state: %#v", report.Checks)
	}
	if report.HasFailures() {
		t.Fatalf("expected safe diagnostics to pass: %#v", report.Checks)
	}
}

func TestTransitDiagnosticsFlagsDangerousCapabilities(t *testing.T) {
	cfg := loadCommandConfig(t)
	capabilities := hotPathCapabilities(cfg)
	paths := transitCapabilityPaths(cfg)
	capabilities.ByPath[paths.metadata] = append(capabilities.ByPath[paths.metadata], capabilityDelete)
	client := fakeDiagnosticTransitClient{
		profile:       commandTestProfile(nil),
		disableUpsert: true,
		capabilities:  capabilities,
	}
	report := cli.Report{Name: reportNameDoctor}

	_, ok := runTransitDiagnostics(
		context.Background(),
		&report,
		cfg,
		client,
		true,
	)
	if !ok {
		t.Fatalf("dangerous policy should not block metadata-derived state: %#v", report.Checks)
	}
	if !report.HasFailures() {
		t.Fatal("expected dangerous capabilities to fail diagnostics")
	}
	if !reportContains(report, "non-hot-path key management") {
		t.Fatalf("expected dangerous capability finding in report: %#v", report.Checks)
	}
}

func TestRegistryStateReportsAutoBootstrapDecisionWhenMissing(t *testing.T) {
	cfg := loadCommandConfig(t)
	cfg.State.Path = filepath.Join(t.TempDir(), "missing-key-registry.json")

	initialReport := cli.Report{Name: reportNameVerifyKey}
	checkRegistryVersionRestrictions(&initialReport, cfg, commandTestProfile(nil))
	if !reportContains(initialReport, "auto-bootstrap eligible=true") {
		t.Fatalf("expected initial metadata to report bootstrap eligibility: %#v", initialReport.Checks)
	}

	rotatedReport := cli.Report{Name: reportNameVerifyKey}
	checkRegistryVersionRestrictions(&rotatedReport, cfg, commandTestProfile(func(profile *openbao.KeyProfile) {
		profile.LatestVersion = 2
		profile.VersionCreationTimes = append(profile.VersionCreationTimes, openbao.KeyVersion{
			Version:   2,
			CreatedAt: time.Unix(1_778_277_660, 0).UTC(),
		})
	}))
	if !reportContains(rotatedReport, "auto-bootstrap eligible=false") ||
		!reportContains(rotatedReport, "latest_version=2") {
		t.Fatalf("expected rotated metadata to report bootstrap denial: %#v", rotatedReport.Checks)
	}
}

func TestDoctorJWTLocalCheckUsesExpectedClaims(t *testing.T) {
	cfg := loadCommandConfig(t)
	cfg.Auth.JWT.JWTFile = copyJWTFixture(t)
	cfg.Auth.JWT.ExpectedSubject = "system:serviceaccount:secret-namespace:other-sa"

	_, err := auth.ReadAndValidateJWT(cfg.Auth.JWT.JWTFile, jwtValidationOptions(cfg))
	if !errors.Is(err, auth.ErrJWTSubjectMismatch) {
		t.Fatalf("expected local JWT subject mismatch, got %v", err)
	}
	if strings.Contains(err.Error(), "system:openbao-kms:workload-a") {
		t.Fatalf("JWT validation leaked raw subject: %v", err)
	}
}

func TestDiagnosticTransitLoopbackDecrypt(t *testing.T) {
	transit := &diagnosticTransit{}
	plaintext := []byte("diagnostic plaintext")
	associatedData := []byte("diagnostic aad")

	encrypted, err := transit.Encrypt(context.Background(), kmsv2.TransitEncryptRequest{
		Plaintext:      plaintext,
		AssociatedData: associatedData,
		KeyVersion:     3,
	})
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	plaintext[0] = 'X'
	associatedData[0] = 'X'

	decrypted, err := transit.Decrypt(context.Background(), kmsv2.TransitDecryptRequest{
		Ciphertext:     encrypted.Ciphertext,
		AssociatedData: []byte("diagnostic aad"),
	})
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if !bytes.Equal(decrypted.Plaintext, []byte("diagnostic plaintext")) {
		t.Fatalf("unexpected loopback plaintext: %q", decrypted.Plaintext)
	}
	if _, err := transit.Decrypt(context.Background(), kmsv2.TransitDecryptRequest{
		Ciphertext:     encrypted.Ciphertext,
		AssociatedData: []byte("wrong aad"),
	}); err == nil {
		t.Fatal("expected associated data mismatch to fail")
	}
}

func loadCommandConfig(t *testing.T) config.Config {
	t.Helper()
	cfg, err := config.Load(config.NewRuntime(), config.LoadOptions{Path: "../../test/testdata/config/valid.yaml"})
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	return cfg
}

func copyJWTFixture(t *testing.T) string {
	t.Helper()

	content, err := os.ReadFile(filepath.Join("../../test/testdata/auth", "valid.jwt"))
	if err != nil {
		t.Fatalf("read JWT fixture: %v", err)
	}
	path := filepath.Join(t.TempDir(), "identity.jwt")
	// #nosec G306,G703 -- test fixture is copied to a t.TempDir path controlled by this test.
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write JWT fixture: %v", err)
	}
	return path
}

func commandTestProfile(mutate func(*openbao.KeyProfile)) openbao.KeyProfile {
	profile := openbao.KeyProfile{
		Name:                 "k8s-workload-a-etcd",
		Type:                 openbao.TransitKeyTypeAES256GCM96,
		LatestVersion:        1,
		MinAvailableVersion:  0,
		MinEncryptionVersion: 0,
		MinDecryptionVersion: 1,
		VersionCreationTimes: []openbao.KeyVersion{{
			Version:   1,
			CreatedAt: time.Unix(1_778_277_600, 0).UTC(),
		}},
		SupportsEncryption: true,
		SupportsDecryption: true,
	}
	if mutate != nil {
		mutate(&profile)
	}
	return profile
}

func hotPathCapabilities(cfg config.Config) openbao.CapabilitiesResult {
	paths := transitCapabilityPaths(cfg)
	return openbao.CapabilitiesResult{ByPath: map[string][]string{
		paths.metadata: {capabilityRead},
		paths.encrypt:  {capabilityUpdate},
		paths.decrypt:  {capabilityUpdate},
	}}
}

func reportContains(report cli.Report, want string) bool {
	for _, check := range report.Checks {
		if strings.Contains(check.Message, want) {
			return true
		}
	}
	return false
}

type fakeDiagnosticTransitClient struct {
	profile       openbao.KeyProfile
	disableUpsert bool
	capabilities  openbao.CapabilitiesResult
}

func (f fakeDiagnosticTransitClient) ReadKeyProfile(
	context.Context,
	string,
	string,
) (openbao.KeyProfile, error) {
	return f.profile, nil
}

func (f fakeDiagnosticTransitClient) ReadDisableUpsert(context.Context, string) (bool, error) {
	return f.disableUpsert, nil
}

func (f fakeDiagnosticTransitClient) Encrypt(
	context.Context,
	openbao.EncryptRequest,
) (openbao.EncryptResponse, error) {
	return openbao.EncryptResponse{Ciphertext: "vault:v1:test", KeyVersion: 1}, nil
}

func (f fakeDiagnosticTransitClient) Decrypt(
	context.Context,
	openbao.DecryptRequest,
) (openbao.DecryptResponse, error) {
	return openbao.DecryptResponse{Plaintext: []byte("diagnostic")}, nil
}

func (f fakeDiagnosticTransitClient) BatchDecrypt(
	context.Context,
	openbao.BatchDecryptRequest,
) (openbao.BatchDecryptResponse, error) {
	return openbao.BatchDecryptResponse{}, nil
}

func (f fakeDiagnosticTransitClient) Capabilities(
	context.Context,
	[]string,
) (openbao.CapabilitiesResult, error) {
	return f.capabilities, nil
}

func (f fakeDiagnosticTransitClient) ProbeEncryptDecrypt(
	context.Context,
	openbao.ProbeRequest,
) (openbao.ProbeResult, error) {
	return openbao.ProbeResult{Ciphertext: []byte("vault:v1:test"), KeyVersion: 1}, nil
}
