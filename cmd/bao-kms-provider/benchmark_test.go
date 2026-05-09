package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/openbao"
)

func TestRunBenchmarkRedactsProbeMaterial(t *testing.T) {
	cfg := loadCommandConfig(t)
	client := &fakeBenchmarkTransitClient{profile: commandTestProfile(nil)}

	result, err := runBenchmark(context.Background(), cfg, client, 1)
	if err != nil {
		t.Fatalf("run benchmark: %v", err)
	}
	var out bytes.Buffer
	printBenchmark(&out, result)
	output := out.String()
	for _, forbidden := range []string{"vault:", benchmarkSmokeAAD} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("benchmark output leaked %q:\n%s", forbidden, output)
		}
	}
}

func TestRunBenchmarkRejectsWrongDecryptPlaintext(t *testing.T) {
	cfg := loadCommandConfig(t)
	client := &fakeBenchmarkTransitClient{
		profile:        commandTestProfile(nil),
		wrongPlaintext: true,
	}

	_, err := runBenchmark(context.Background(), cfg, client, 1)
	if err == nil {
		t.Fatal("expected wrong decrypt plaintext to fail")
	}
}

type fakeBenchmarkTransitClient struct {
	profile        openbao.KeyProfile
	plaintext      []byte
	wrongPlaintext bool
}

func (f *fakeBenchmarkTransitClient) ReadKeyProfile(
	context.Context,
	string,
	string,
) (openbao.KeyProfile, error) {
	return f.profile, nil
}

func (f *fakeBenchmarkTransitClient) ReadDisableUpsert(context.Context, string) (bool, error) {
	return true, nil
}

func (f *fakeBenchmarkTransitClient) Encrypt(
	_ context.Context,
	req openbao.EncryptRequest,
) (openbao.EncryptResponse, error) {
	f.plaintext = append([]byte(nil), req.Plaintext...)
	return openbao.EncryptResponse{Ciphertext: "vault:v1:redacted-probe", KeyVersion: req.KeyVersion}, nil
}

func (f *fakeBenchmarkTransitClient) Decrypt(
	context.Context,
	openbao.DecryptRequest,
) (openbao.DecryptResponse, error) {
	if f.wrongPlaintext {
		return openbao.DecryptResponse{Plaintext: []byte("wrong")}, nil
	}
	return openbao.DecryptResponse{Plaintext: append([]byte(nil), f.plaintext...)}, nil
}

func (f *fakeBenchmarkTransitClient) BatchDecrypt(
	context.Context,
	openbao.BatchDecryptRequest,
) (openbao.BatchDecryptResponse, error) {
	return openbao.BatchDecryptResponse{}, nil
}

func (f *fakeBenchmarkTransitClient) Capabilities(
	context.Context,
	[]string,
) (openbao.CapabilitiesResult, error) {
	return openbao.CapabilitiesResult{}, nil
}

func (f *fakeBenchmarkTransitClient) ProbeEncryptDecrypt(
	context.Context,
	openbao.ProbeRequest,
) error {
	return nil
}
