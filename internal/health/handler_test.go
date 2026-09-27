package health_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/health"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/kmsv2"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/status"
)

const probeKeyHash = "key-hash-redacted"

func TestNewHandlerRejectsNilProbes(t *testing.T) {
	if _, err := health.NewHandler(nil, &readyStub{}); err == nil {
		t.Fatal("expected error for nil liveness probe")
	}
	if _, err := health.NewHandler(&liveStub{}, nil); err == nil {
		t.Fatal("expected error for nil readiness probe")
	}
}

func TestLiveReturns200WhenLive(t *testing.T) {
	handler := mustHandler(t, &liveStub{}, &readyStub{})

	resp := request(t, handler, http.MethodGet, health.PathLive)
	if resp.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", resp.Code)
	}
	if got := decodeStatus(t, resp); got != "ok" {
		t.Fatalf("unexpected status body: %q", got)
	}
}

func TestLiveReturns503WhenLivenessProbeFails(t *testing.T) {
	handler := mustHandler(t, &liveStub{err: health.ErrNotStarted}, &readyStub{})

	resp := request(t, handler, http.MethodGet, health.PathLive)
	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("unexpected status: %d", resp.Code)
	}
	if got := decodeStatus(t, resp); got != "unavailable" {
		t.Fatalf("unexpected status body: %q", got)
	}
}

func TestReadyReturns200WhenHealthyFreshAndActive(t *testing.T) {
	ready := &readyStub{diagnostics: status.Diagnostics{
		Healthz:              kmsv2.HealthOK,
		Stale:                false,
		ActiveKeyIDHash:      probeKeyHash,
		ActiveTransitVersion: 3,
		CacheAge:             5 * time.Second,
		RotationState:        status.RotationStateActive,
	}}
	handler := mustHandler(t, &liveStub{}, ready)

	resp := request(t, handler, http.MethodGet, health.PathReady)
	if resp.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", resp.Code)
	}
	if got := decodeStatus(t, resp); got != "ok" {
		t.Fatalf("unexpected status body: %q", got)
	}
}

func TestReadyReturns503WhenDiagnosticsUnhealthy(t *testing.T) {
	cases := []struct {
		name        string
		diagnostics status.Diagnostics
	}{
		{
			name: "unhealthy",
			diagnostics: status.Diagnostics{
				Healthz:         kmsv2.HealthUnhealthy,
				ActiveKeyIDHash: probeKeyHash,
			},
		},
		{
			name: "stale",
			diagnostics: status.Diagnostics{
				Healthz:         kmsv2.HealthOK,
				Stale:           true,
				ActiveKeyIDHash: probeKeyHash,
			},
		},
		{
			name: "no-active-snapshot",
			diagnostics: status.Diagnostics{
				Healthz:         kmsv2.HealthOK,
				Stale:           false,
				ActiveKeyIDHash: "",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler := mustHandler(t, &liveStub{}, &readyStub{diagnostics: tc.diagnostics})

			resp := request(t, handler, http.MethodGet, health.PathReady)
			if resp.Code != http.StatusServiceUnavailable {
				t.Fatalf("unexpected status: %d", resp.Code)
			}
			if got := decodeStatus(t, resp); got != "unavailable" {
				t.Fatalf("unexpected status body: %q", got)
			}
		})
	}
}

func TestReadyReturns503WhenProbeReturnsError(t *testing.T) {
	handler := mustHandler(t, &liveStub{}, &readyStub{err: errors.New("probe failed")})

	resp := request(t, handler, http.MethodGet, health.PathReady)
	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("unexpected status: %d", resp.Code)
	}
}

func TestNonGetMethodsAreRejected(t *testing.T) {
	handler := mustHandler(t, &liveStub{}, &readyStub{})

	for _, path := range []string{health.PathLive, health.PathReady} {
		resp := request(t, handler, http.MethodPost, path)
		if resp.Code != http.StatusMethodNotAllowed {
			t.Fatalf("path %s: unexpected status %d", path, resp.Code)
		}
	}
}

func TestUnknownPathReturns404(t *testing.T) {
	handler := mustHandler(t, &liveStub{}, &readyStub{})

	resp := request(t, handler, http.MethodGet, "/unknown")
	if resp.Code != http.StatusNotFound {
		t.Fatalf("unexpected status: %d", resp.Code)
	}
}

func mustHandler(t *testing.T, live health.LivenessProbe, ready health.ReadinessProbe) *health.Handler {
	t.Helper()
	handler, err := health.NewHandler(live, ready)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	return handler
}

func request(t *testing.T, handler http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func decodeStatus(t *testing.T, resp *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return body.Status
}

type liveStub struct {
	err error
}

func (l *liveStub) Live() error { return l.err }

type readyStub struct {
	diagnostics status.Diagnostics
	err         error
}

func (r *readyStub) Ready(_ context.Context) (status.Diagnostics, error) {
	return r.diagnostics, r.err
}

func TestReadyIncludesBoundedProbeReasonsAndClearsThem(t *testing.T) {
	ready := &readyStub{diagnostics: status.Diagnostics{
		Healthz: kmsv2.HealthUnhealthy, ActiveKeyIDHash: probeKeyHash,
		Reasons:            []status.HealthReason{status.ReasonMetadataReadFailed, status.ReasonDeepProbeFailed},
		MetadataErrorClass: "tls_failed", DeepErrorClass: "permission_denied",
	}}
	handler := mustHandler(t, &liveStub{}, ready)
	resp := request(t, handler, http.MethodGet, health.PathReady)
	var body struct {
		Reasons            []status.HealthReason `json:"reasons"`
		MetadataErrorClass string                `json:"metadata_error_class"`
		DeepErrorClass     string                `json:"deep_error_class"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if resp.Code != http.StatusServiceUnavailable || len(body.Reasons) != 2 ||
		body.MetadataErrorClass != "tls_failed" || body.DeepErrorClass != "permission_denied" {
		t.Fatalf("failed readiness body: %s", resp.Body.String())
	}
	ready.diagnostics.Healthz = kmsv2.HealthOK
	ready.diagnostics.Reasons = []status.HealthReason{}
	ready.diagnostics.MetadataErrorClass = ""
	ready.diagnostics.DeepErrorClass = ""
	resp = request(t, handler, http.MethodGet, health.PathReady)
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"reasons":[]`) ||
		strings.Contains(resp.Body.String(), "error_class") {
		t.Fatalf("recovered readiness body: %s", resp.Body.String())
	}
	ready.err = errors.New("sensitive token and backend URL")
	resp = request(t, handler, http.MethodGet, health.PathReady)
	if !strings.Contains(resp.Body.String(), "diagnostics_unavailable") ||
		strings.Contains(resp.Body.String(), "sensitive") {
		t.Fatalf("unredacted diagnostic failure: %s", resp.Body.String())
	}
}
