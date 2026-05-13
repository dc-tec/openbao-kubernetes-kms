package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/auth"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/config"
	"github.com/dc-tec/openbao-kubernetes-kms/test/fakes"
)

func TestProbeOnceWithBootstrapGraceRetriesUntilSuccess(t *testing.T) {
	probe := &fakeBootstrapProbe{failures: 2, err: errors.New("openbao unavailable")}
	now := time.Unix(100, 0).UTC()
	var sleeps []time.Duration
	err := probeOnceWithBootstrapGraceAndSleep(
		context.Background(),
		probe,
		config.BootstrapConfig{GraceTimeout: 30 * time.Second, RetryInterval: 5 * time.Second},
		func() time.Time { return now },
		func(_ context.Context, delay time.Duration) error {
			sleeps = append(sleeps, delay)
			now = now.Add(delay)
			return nil
		},
	)
	if err != nil {
		t.Fatalf("probe with grace: %v", err)
	}
	if probe.calls != 3 {
		t.Fatalf("expected three probe attempts, got %d", probe.calls)
	}
	if len(sleeps) != 2 || sleeps[0] != 5*time.Second || sleeps[1] != 5*time.Second {
		t.Fatalf("unexpected sleeps: %#v", sleeps)
	}
}

func TestProbeOnceWithBootstrapGraceStopsAtDeadline(t *testing.T) {
	probe := &fakeBootstrapProbe{failures: 10, err: errors.New("jwt file not ready")}
	now := time.Unix(100, 0).UTC()
	var sleeps []time.Duration
	err := probeOnceWithBootstrapGraceAndSleep(
		context.Background(),
		probe,
		config.BootstrapConfig{GraceTimeout: 6 * time.Second, RetryInterval: 5 * time.Second},
		func() time.Time { return now },
		func(_ context.Context, delay time.Duration) error {
			sleeps = append(sleeps, delay)
			now = now.Add(delay)
			return nil
		},
	)
	if err == nil {
		t.Fatal("expected bootstrap grace timeout")
	}
	if !strings.Contains(err.Error(), "6s") || !strings.Contains(err.Error(), "jwt file not ready") {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sleeps) != 2 || sleeps[0] != 5*time.Second || sleeps[1] != time.Second {
		t.Fatalf("unexpected sleeps: %#v", sleeps)
	}
}

func TestAuthLoginTimeoutDefaultsToFiveSecondsMinimum(t *testing.T) {
	cfg := config.Config{}
	cfg.OpenBao.Timeout = 2 * time.Second
	if got := authLoginTimeout(cfg); got != 5*time.Second {
		t.Fatalf("expected 5s login timeout, got %s", got)
	}
	cfg.OpenBao.Timeout = 8 * time.Second
	if got := authLoginTimeout(cfg); got != 8*time.Second {
		t.Fatalf("expected OpenBao timeout fallback, got %s", got)
	}
	cfg.Auth.LoginTimeout = 3 * time.Second
	if got := authLoginTimeout(cfg); got != 3*time.Second {
		t.Fatalf("expected explicit login timeout, got %s", got)
	}
}

func TestBuildAuthManagerRejectsCertAuthInDefaultBuild(t *testing.T) {
	cfg := config.Config{}
	cfg.Auth.Method = "cert"

	_, err := buildAuthManager(cfg, &fakes.OpenBaoAuthClient{}, nil)
	if !errors.Is(err, auth.ErrAuthConfig) {
		t.Fatalf("expected auth config error, got %v", err)
	}
	if !strings.Contains(err.Error(), "certauth build variant") {
		t.Fatalf("expected certauth build variant error, got %v", err)
	}
}

type fakeBootstrapProbe struct {
	failures int
	err      error
	calls    int
}

func (f *fakeBootstrapProbe) ProbeOnce(context.Context) error {
	f.calls++
	if f.calls <= f.failures {
		return f.err
	}
	return nil
}
