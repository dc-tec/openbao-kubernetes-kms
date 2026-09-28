package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestActiveVersionRequiresValidatedPromotion(t *testing.T) {
	for _, tc := range []struct {
		file string
		want int
	}{
		{"state-initial-v1.json", 1},
		{"state-discovered-v2.json", 1},
		{"state-rotated-v1-v2.json", 2},
		{"state-unbound-v1alpha1.json", 0},
	} {
		t.Run(tc.file, func(t *testing.T) {
			// #nosec G304 -- repository-owned registry fixtures, selected by this test.
			data, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "testdata", "keyregistry", tc.file))
			if err != nil {
				t.Fatal(err)
			}
			version, err := activeVersionFromState(data)
			if version != tc.want || (err != nil) != (tc.want == 0) {
				t.Fatalf("active version = %d, error = %v; want %d", version, err, tc.want)
			}
		})
	}
	for _, data := range []string{"", "{}", `{"generation":2}`, `{"snapshots":[{"transitVersion":2}]}`} {
		if _, err := activeVersionFromState([]byte(data)); err == nil {
			t.Fatal("accepted corrupt or incomplete registry as promotion evidence")
		}
	}
}

func TestColdOutageRequiresKMSEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, counter, health string
		wantError             bool
	}{
		{"new Encrypt failure", "4", "connection refused", false},
		{"unhealthy Status", "3", "kmsv2 Provider openbao-kms-workload-a is not healthy, " +
			"error: got unexpected healthz status: unhealthy", false},
		{"unchanged counter", "3", "ok", true},
		{"connection refused", "3", "connection refused", true},
		{"forbidden", "3", "Forbidden", true},
		{"timeout", "3", "context deadline exceeded", true},
		{"unrelated failure", "3", "etcd health check failed", true},
		{"other KMS provider", "3", "kmsv2 Provider other is not healthy, " +
			"error: got unexpected healthz status: unhealthy", true},
		{"invalid KMS version", "3", "kmsv2 Provider openbao-kms-workload-a is not healthy, " +
			"error: expected KMSv2 API version v2", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			ssh := "#!/bin/sh\necho 'openbao_kms_grpc_requests_total{method=\"encrypt\",status=\"unavailable\"} " +
				tc.counter + "'\n"
			kubectl := "#!/bin/sh\ntest \"$2\" = '--raw=/readyz/kms-providers' || exit 2\n" +
				"echo '" + tc.health + "' >&2\nexit 1\n"
			for name, script := range map[string]string{"ssh": ssh, "kubectl": kubectl} {
				// #nosec G306 -- executable command stubs in an isolated test directory.
				if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()
			err := waitForKMSOutage(ctx, &labConfig{root: dir}, kubeadmCheck{host: "test-host"}, 3)
			if (err != nil) != tc.wantError {
				t.Fatalf("cold outage evidence error = %v, wantError %v", err, tc.wantError)
			}
		})
	}
}

func TestFailedEncryptionCount(t *testing.T) {
	for _, tc := range []struct {
		name, metrics string
		want          float64
		wantError     bool
	}{
		{"no metrics", "", 0, true},
		{"unrelated error", `openbao_kms_openbao_requests_total{status="connection_failed"} 1`, 0, true},
		{"healthy Encrypt", `openbao_kms_grpc_requests_total{method="encrypt",status="ok"} 4`, 0, false},
		{"Decrypt failure", `openbao_kms_grpc_requests_total{method="decrypt",status="unavailable"} 2`, 0, false},
		{"Encrypt failure", `openbao_kms_grpc_requests_total{method="encrypt",status="unavailable"} 3`, 3, false},
		{"bad count", `openbao_kms_grpc_requests_total{method="encrypt",status="unavailable"} NaN`, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			count, err := failedEncryptionCount(tc.metrics)
			if count != tc.want || (err != nil) != tc.wantError {
				t.Fatalf("count = %v, error = %v; want %v, error %v", count, err, tc.want, tc.wantError)
			}
		})
	}
}

func TestCandidateVersionRejectsBaselineOrUnknown(t *testing.T) {
	for _, tc := range []struct {
		output, expected string
		wantError        bool
	}{
		{"version: candidate\ncommit: abc1234\ndirty: true", "abc1234", false},
		{"version: preview.2\ncommit: old1234", "abc1234", true},
		{"commit: unknown", "unknown", true},
		{"", "", true},
		{"commit: abc12345", "abc1234", true},
	} {
		if err := requireCandidateVersion(tc.output, tc.expected); (err != nil) != tc.wantError {
			t.Fatalf("candidate %q against %q: %v", tc.output, tc.expected, err)
		}
	}
}
