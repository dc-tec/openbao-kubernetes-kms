package metrics_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/auth"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/kmsv2"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/metrics"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/status"
)

func TestRecorderScrapeExposesBoundedMetrics(t *testing.T) {
	recorder, err := metrics.NewRecorder()
	if err != nil {
		t.Fatalf("new recorder: %v", err)
	}
	if err := recorder.RegisterStatusProvider(fakeStatusProvider{}); err != nil {
		t.Fatalf("register status: %v", err)
	}
	if err := recorder.RegisterAuthProvider(fakeAuthProvider{}); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	recorder.RecordGRPCRequest("decrypt", "not_found", 2*time.Millisecond)
	recorder.RecordOpenBaoRequest("transit decrypt", "permission_denied", 3*time.Millisecond)
	recorder.RecordAuthLogin("ok")
	recorder.RecordAuthRenewal("auth_failed")
	recorder.RecordTransitMetadataObservation("ok")
	recorder.RecordAADValidationError("annotation_invalid")
	recorder.RecordDecryptKeyIDError("key_id_unknown")
	recorder.RecordDecryptBatchSize(4)
	recorder.RecordSocketRestart()

	output := scrapeMetrics(t, recorder.Handler())
	for _, want := range []string{
		`openbao_kms_grpc_requests_total{method="decrypt",status="not_found"} 1`,
		`openbao_kms_openbao_requests_total{operation="transit_decrypt",status="permission_denied"} 1`,
		`openbao_kms_auth_login_total{status="ok"} 1`,
		`openbao_kms_auth_renewal_total{status="auth_failed"} 1`,
		`openbao_kms_transit_metadata_observation_total{status="ok"} 1`,
		`openbao_kms_aad_validation_errors_total{reason="annotation_invalid"} 1`,
		`openbao_kms_decrypt_key_id_errors_total{reason="key_id_unknown"} 1`,
		`openbao_kms_socket_restarts_total 1`,
		`openbao_kms_status_key_id_hash{hash="safe-key-id-hash"} 1`,
		`openbao_kms_key_version 7`,
		`openbao_kms_token_ttl_seconds 300`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("metrics output missing %q:\n%s", want, output)
		}
	}
	for _, forbidden := range []string{
		"raw-key-id", "transit/keys/k8s", "k8s-key-name", "eyJhbGciOi", "vault:v1:full-ciphertext",
	} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("metrics output leaked %q:\n%s", forbidden, output)
		}
	}
}

func scrapeMetrics(t *testing.T, handler http.Handler) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("unexpected scrape status: %d\n%s", resp.Code, resp.Body.String())
	}
	return resp.Body.String()
}

type fakeStatusProvider struct{}

func (fakeStatusProvider) DiagnosticsSnapshot() status.Diagnostics {
	return status.Diagnostics{
		Healthz:              kmsv2.HealthOK,
		CacheAge:             5 * time.Second,
		ActiveKeyIDHash:      "safe-key-id-hash",
		ActiveTransitVersion: 7,
		RotationState:        status.RotationStateActive,
		CircuitBreaker: status.CircuitBreakerSnapshot{
			State: status.CircuitBreakerClosed,
		},
	}
}

type fakeAuthProvider struct{}

func (fakeAuthProvider) State() auth.State {
	return auth.State{
		Status:   auth.StatusAuthenticated,
		TokenTTL: 5 * time.Minute,
	}
}
