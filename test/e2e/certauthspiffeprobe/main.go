//go:build certauth_spiffe

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/auth"
)

type probeConfig struct {
	WorkloadAPISocket string
	SPIFFEID          string
	TrustDomain       string
	Timeout           time.Duration
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := probeConfig{}
	flag.StringVar(&cfg.WorkloadAPISocket, "workload-api-socket", "unix:///run/spire/sockets/agent.sock", "SPIFFE Workload API socket")
	flag.StringVar(&cfg.SPIFFEID, "spiffe-id", "spiffe://example.org/openbao-kms/workload-a", "expected SPIFFE ID")
	flag.StringVar(&cfg.TrustDomain, "trust-domain", "example.org", "expected SPIFFE trust domain")
	flag.DurationVar(&cfg.Timeout, "timeout", 15*time.Second, "probe timeout")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()

	provider, err := auth.NewSPIFFECertificateProvider(ctx, auth.SPIFFEProviderConfig{
		WorkloadAPISocket: cfg.WorkloadAPISocket,
		SPIFFEID:          cfg.SPIFFEID,
		TrustDomain:       cfg.TrustDomain,
	})
	if err != nil {
		return fmt.Errorf("initialize SPIFFE certificate provider: %w", err)
	}
	defer func() {
		_ = provider.Close()
	}()

	source, err := auth.NewCertLoginSource(auth.CertLoginSourceConfig{
		MountPath:        "auth/e2e-cert",
		Name:             "openbao-kms-control-plane",
		Source:           "spiffe",
		MinRemainingTTL:  time.Second,
		ClockSkewLeeway:  30 * time.Second,
		ExpectedSPIFFEID: cfg.SPIFFEID,
		TrustDomain:      cfg.TrustDomain,
	}, provider)
	if err != nil {
		return fmt.Errorf("initialize certificate login source: %w", err)
	}
	if _, err := source.ValidateLocal(ctx, auth.RealClock{}); err != nil {
		return fmt.Errorf("validate SPIFFE certificate source: %w", err)
	}
	return nil
}
