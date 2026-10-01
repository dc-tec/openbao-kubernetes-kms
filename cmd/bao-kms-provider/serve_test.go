package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/config"
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
	if probe.deepCalls != 1 {
		t.Fatalf("expected one deep probe after metadata recovery, got %d", probe.deepCalls)
	}
	if len(sleeps) != 2 || sleeps[0] != 5*time.Second || sleeps[1] != 5*time.Second {
		t.Fatalf("unexpected sleeps: %#v", sleeps)
	}
}

func TestProbeOnceWithBootstrapGraceRetriesDeepProbeUntilSuccess(t *testing.T) {
	probe := &fakeBootstrapProbe{deepFailures: 2, err: errors.New("Transit data path unavailable")}
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
	if probe.calls != 3 || probe.deepCalls != 3 {
		t.Fatalf("unexpected probe calls: metadata=%d deep=%d", probe.calls, probe.deepCalls)
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

type fakeBootstrapProbe struct {
	failures     int
	deepFailures int
	err          error
	calls        int
	deepCalls    int
}

func (f *fakeBootstrapProbe) ProbeOnce(context.Context) error {
	f.calls++
	if f.calls <= f.failures {
		return f.err
	}
	return nil
}

func (f *fakeBootstrapProbe) DeepProbeOnce(context.Context) error {
	f.deepCalls++
	if f.deepCalls <= f.deepFailures {
		return f.err
	}
	return nil
}

func TestBuildFailureLogsLifecycleWithoutRawError(t *testing.T) {
	var out bytes.Buffer
	builder := runtimeBuilder{logWriter: &out}
	cfg := config.Config{Logging: config.LoggingConfig{Level: "info", Format: "json"}}
	cfg.Server.SocketMode = "sensitive invalid mode"
	if _, err := builder.build(context.Background(), cfg); err == nil {
		t.Fatal("build unexpectedly succeeded")
	}
	logs := out.String()
	for _, want := range []string{
		`"message":"serve.start"`, `"message":"serve.shutdown"`, `"error_class":"startup_failed"`,
	} {
		if !strings.Contains(logs, want) {
			t.Fatalf("missing %s: %s", want, logs)
		}
	}
	if strings.Contains(logs, "serve.ready") || strings.Contains(logs, "sensitive") {
		t.Fatalf("failed startup claimed readiness or exposed input: %s", logs)
	}
}

func TestCheckServePlatformAllowsOnlyLinux(t *testing.T) {
	if err := checkServePlatform("linux"); err != nil {
		t.Fatalf("checkServePlatform(linux) = %v, want nil", err)
	}
	err := checkServePlatform("darwin")
	if err == nil || !strings.Contains(err.Error(), "only on Linux") {
		t.Fatalf("checkServePlatform(darwin) = %v, want Linux-only error", err)
	}
}
