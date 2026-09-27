package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/config"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/kmsv2"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/logging"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/metrics"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/openbao"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/status"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/version"
)

func TestObservabilityOmitsCorrelationFieldsByDefault(t *testing.T) {
	var out bytes.Buffer
	observer := newTestObservability(t, &out, debugCorrelation{})

	observer.ObserveKMSRequest(context.Background(), kmsv2.RequestObservation{
		Method:         "decrypt",
		Status:         "ok",
		Duration:       time.Millisecond,
		RequestUIDHash: "safe-request-uid-hash",
	})

	output := out.String()
	if !strings.Contains(output, logMessageKMSRequest) {
		t.Fatalf("expected KMS request log, got:\n%s", output)
	}
	for _, forbidden := range []string{
		"safe-request-uid-hash",
		logging.FieldRequestUIDHash,
		logging.FieldCorrelationID,
	} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("correlation field %q logged while disabled:\n%s", forbidden, output)
		}
	}
}

func TestObservabilityEmitsCorrelationFieldsOnlyWhileActive(t *testing.T) {
	startedAt := time.Date(2026, 5, 9, 10, 0, 0, 0, time.UTC)
	correlation := newDebugCorrelation(config.DebugCorrelationConfig{
		Enabled:    true,
		TTL:        time.Minute,
		IncidentID: "INC-123",
	}, true, startedAt)
	correlation.now = func() time.Time {
		return startedAt.Add(10 * time.Second)
	}

	var out bytes.Buffer
	observer := newTestObservability(t, &out, correlation)
	observer.ObserveKMSRequest(context.Background(), kmsv2.RequestObservation{
		Method:         "decrypt",
		Status:         "ok",
		Duration:       time.Millisecond,
		RequestUIDHash: "safe-request-uid-hash",
	})
	observer.ObserveOpenBaoRequest(context.Background(), openbao.RequestObservation{
		Operation: "transit decrypt",
		Status:    "ok",
		Duration:  time.Millisecond,
		RequestID: "req-123",
	})

	output := out.String()
	for _, want := range []string{
		"safe-request-uid-hash",
		logging.FieldRequestUIDHash,
		logging.FieldOpenBaoRequestID,
		"req-123",
		logging.FieldCorrelationID,
		"INC-123",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("correlation output missing %q:\n%s", want, output)
		}
	}
}

func TestObservabilityStopsCorrelationAfterExpiry(t *testing.T) {
	startedAt := time.Date(2026, 5, 9, 10, 0, 0, 0, time.UTC)
	correlation := newDebugCorrelation(config.DebugCorrelationConfig{
		Enabled:    true,
		TTL:        time.Minute,
		IncidentID: "INC-123",
	}, true, startedAt)
	correlation.now = func() time.Time {
		return startedAt.Add(2 * time.Minute)
	}

	var out bytes.Buffer
	observer := newTestObservability(t, &out, correlation)
	observer.ObserveOpenBaoRequest(context.Background(), openbao.RequestObservation{
		Operation: "transit decrypt",
		Status:    "ok",
		Duration:  time.Millisecond,
		RequestID: "req-123",
	})

	output := out.String()
	for _, forbidden := range []string{
		logging.FieldOpenBaoRequestID,
		"req-123",
		logging.FieldCorrelationID,
		"INC-123",
	} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("correlation field %q logged after expiry:\n%s", forbidden, output)
		}
	}
}

func TestObservabilityLogsPanicRecoveryWithoutPanicValue(t *testing.T) {
	var out bytes.Buffer
	observer := newTestObservability(t, &out, debugCorrelation{})

	observer.ObserveKMSRequest(context.Background(), kmsv2.RequestObservation{
		Method:         "encrypt",
		Status:         "internal",
		Duration:       time.Millisecond,
		ErrorClass:     "panic",
		PanicRecovered: true,
		PanicType:      "string",
	})

	output := out.String()
	for _, want := range []string{
		logging.FieldPanicRecovered,
		logging.FieldPanicType,
		"string",
		`"error_class":"panic"`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("panic recovery output missing %q:\n%s", want, output)
		}
	}
	for _, forbidden := range []string{
		"secret kube payload",
		"fake transit encrypt panic",
		logging.RedactedValue,
	} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("panic recovery log exposed %q:\n%s", forbidden, output)
		}
	}
}

func newTestObservability(t *testing.T, out *bytes.Buffer, correlation debugCorrelation) observability {
	t.Helper()

	logger, err := logging.New(logging.Options{
		Level:  "debug",
		Format: logging.FormatJSON,
		Output: out,
	})
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}
	recorder, err := metrics.NewRecorder(version.Info{})
	if err != nil {
		t.Fatalf("new metrics recorder: %v", err)
	}
	return observability{
		logger:      logger,
		metrics:     recorder,
		correlation: correlation,
	}
}

func TestObservabilityLogsProbeCausesAndPromotionsAtDefaultLevel(t *testing.T) {
	var out bytes.Buffer
	observer := newTestObservability(t, &out, debugCorrelation{})
	logger, err := logging.New(logging.Options{Level: "info", Format: logging.FormatJSON, Output: &out})
	if err != nil {
		t.Fatal(err)
	}
	observer.logger = logger
	observer.ObserveStatusProbe(context.Background(), status.ProbeObservation{
		Kind: status.ProbeKindMetadata, Status: "tls_failed", Reason: status.ReasonMetadataReadFailed,
		ErrorClass: "tls_failed",
	})
	observer.ObserveKeyPromotion(context.Background(), status.PromotionObservation{
		PreviousKeyIDHash: "old-hash", KeyIDHash: "new-hash", PreviousTransitVersion: 1, TransitVersion: 2,
	})
	observer.ObserveStatusProbe(context.Background(), status.ProbeObservation{Kind: status.ProbeKindDeep, Status: "ok"})
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("unexpected log count: %s", out.String())
	}
	for _, want := range []string{
		`"message":"status.probe"`, `"level":"WARN"`,
		`"reason":"metadata_read_failed"`, `"error_class":"tls_failed"`,
	} {
		if !strings.Contains(lines[0], want) {
			t.Fatalf("missing %s: %s", want, lines[0])
		}
	}
	for _, want := range []string{
		`"message":"key.promoted"`, `"level":"INFO"`,
		`"previous_key_id_hash":"old-hash"`, `"key_id_hash":"new-hash"`,
		`"previous_transit_key_version":1`, `"transit_key_version":2`,
	} {
		if !strings.Contains(lines[1], want) {
			t.Fatalf("missing %s: %s", want, lines[1])
		}
	}
}

func TestClockRegressionDiagnosticIsVisibleAtDefaultLevel(t *testing.T) {
	var out bytes.Buffer
	observer := newTestObservability(t, &out, debugCorrelation{})
	logger, err := logging.New(logging.Options{Level: "info", Format: logging.FormatJSON, Output: &out})
	if err != nil {
		t.Fatal(err)
	}
	observer.logger = logger
	observer.ObserveClockRegression(t.Context())
	output := out.String()
	for _, value := range []string{"clock.regressed", "timestamp_order_preserved", "WARN"} {
		if !strings.Contains(output, value) {
			t.Fatalf("missing clock regression diagnostic %q: %s", value, output)
		}
	}
}
