package metrics_test

import (
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/auth"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/kmsv2"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/metrics"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/status"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/version"
)

func TestRecorderScrapeExposesBoundedMetrics(t *testing.T) {
	recorder, err := metrics.NewRecorder(version.Info{})
	if err != nil {
		t.Fatalf("new recorder: %v", err)
	}
	if err := recorder.RegisterStatusProvider(fakeStatusProvider{}); err != nil {
		t.Fatalf("register status: %v", err)
	}
	if err := recorder.RegisterAuthProvider(fakeAuthProvider{}); err != nil {
		t.Fatalf("register auth: %v", err)
	}
	if err := recorder.RegisterConcurrencyProvider(fakeConcurrencyProvider{}); err != nil {
		t.Fatalf("register concurrency: %v", err)
	}

	recorder.RecordGRPCRequest("decrypt", "not_found", 2*time.Millisecond)
	recorder.RecordGRPCConcurrencyRejection("decrypt")
	recorder.RecordOpenBaoRequest("transit decrypt", "permission_denied", 3*time.Millisecond)
	recorder.RecordAuthLogin("ok")
	recorder.RecordAuthRenewal("auth_failed")
	recorder.RecordTransitMetadataObservation("ok")
	recorder.RecordAADValidationError("annotation_invalid")
	recorder.RecordDecryptKeyIDError("key_id_unknown")
	recorder.RecordPanicRecovery("encrypt")
	recorder.RecordSocketRestart()

	output := scrapeMetrics(t, recorder.Handler())
	for _, want := range []string{
		`openbao_kms_grpc_requests_total{method="decrypt",status="not_found"} 1`,
		`openbao_kms_grpc_concurrency_rejections_total{method="decrypt"} 1`,
		`openbao_kms_grpc_in_flight{method="status"} 1`,
		`openbao_kms_grpc_in_flight{method="encrypt"} 2`,
		`openbao_kms_grpc_in_flight{method="decrypt"} 3`,
		`openbao_kms_openbao_requests_total{operation="transit_decrypt",status="permission_denied"} 1`,
		`openbao_kms_auth_login_total{status="ok"} 1`,
		`openbao_kms_auth_renewal_total{status="auth_failed"} 1`,
		`openbao_kms_transit_metadata_observation_total{status="ok"} 1`,
		`openbao_kms_aad_validation_errors_total{reason="annotation_invalid"} 1`,
		`openbao_kms_decrypt_key_id_errors_total{reason="key_id_unknown"} 1`,
		`openbao_kms_panic_recoveries_total{method="encrypt"} 1`,
		`openbao_kms_socket_restarts_total 1`,
		`openbao_kms_status_key_id_hash{hash="safe-key-id-hash"} 1`,
		`openbao_kms_key_version 7`,
		`openbao_kms_auth_method_info{method="cert"} 1`,
		`openbao_kms_certificate_source_info{source="spiffe"} 1`,
		`openbao_kms_token_ttl_seconds 300`,
		`openbao_kms_certificate_ttl_seconds 7200`,
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

func TestRecorderRuntimeCollectorsAreIsolated(t *testing.T) {
	for _, buildVersion := range []string{"test-one", "test-two"} {
		recorder, err := metrics.NewRecorder(version.Info{
			Version: buildVersion, Commit: "commit", BuildDate: "date", Dirty: "false",
		})
		if err != nil {
			t.Fatalf("new recorder: %v", err)
		}
		output := scrapeMetrics(t, recorder.Handler())
		for _, metric := range []string{"go_goroutines ", "go_info{", "go_memstats_alloc_bytes "} {
			if !strings.Contains(output, metric) {
				t.Errorf("scrape missing runtime metric %q", metric)
			}
		}
		want := `openbao_kms_build_info{build_date="date",commit="commit",dirty="false",version="` + buildVersion + `"} 1`
		if !strings.Contains(output, want) || strings.Count(output, "\nopenbao_kms_build_info{") != 1 {
			t.Errorf("build metadata was not isolated to this recorder: %s", output)
		}
		if runtime.GOOS == "linux" {
			for _, metric := range []string{
				"process_start_time_seconds ", "process_cpu_seconds_total ", "process_resident_memory_bytes ",
			} {
				if !strings.Contains(output, metric) {
					t.Errorf("scrape missing Linux process metric %q", metric)
				}
			}
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

type mutableStatusProvider struct{ diagnostics status.Diagnostics }

func (p *mutableStatusProvider) DiagnosticsSnapshot() status.Diagnostics { return p.diagnostics }

func TestPersistenceWarningMetricTracksRecovery(t *testing.T) {
	recorder, err := metrics.NewRecorder(version.Info{})
	if err != nil {
		t.Fatal(err)
	}
	provider := &mutableStatusProvider{}
	if err := recorder.RegisterStatusProvider(provider); err != nil {
		t.Fatal(err)
	}
	for _, degraded := range []bool{false, true, false} {
		provider.diagnostics.PersistenceDegraded = degraded
		want := "openbao_kms_rotation_persistence_degraded 0"
		if degraded {
			want = "openbao_kms_rotation_persistence_degraded 1"
		}
		if output := scrapeMetrics(t, recorder.Handler()); !strings.Contains(output, want) {
			t.Fatalf("missing persistence gauge: %s", want)
		}
	}
}

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
		AuthMethod:        "cert",
		CertificateSource: "spiffe",
		Status:            auth.StatusAuthenticated,
		TokenTTL:          5 * time.Minute,
		CertTTL:           2 * time.Hour,
	}
}

type fakeConcurrencyProvider struct{}

func (fakeConcurrencyProvider) InFlightKMSRequests() (statusCount int, encryptCount int, decryptCount int) {
	return 1, 2, 3
}
