package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/cli"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/config"
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

func loadCommandConfig(t *testing.T) config.Config {
	t.Helper()
	cfg, err := config.Load(config.NewRuntime(), config.LoadOptions{Path: "../../test/testdata/config/valid.yaml"})
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	return cfg
}

func commandTestProfile(mutate func(*openbao.KeyProfile)) openbao.KeyProfile {
	profile := openbao.KeyProfile{
		Name:                 "k8s-workload-a-etcd",
		Type:                 transitKeyTypeAESGCM,
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
) error {
	return nil
}
