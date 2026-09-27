package main

import (
	"os"
	"path/filepath"
	"testing"
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
