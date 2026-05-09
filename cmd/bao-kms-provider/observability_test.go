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
	recorder, err := metrics.NewRecorder()
	if err != nil {
		t.Fatalf("new metrics recorder: %v", err)
	}
	return observability{
		logger:      logger,
		metrics:     recorder,
		correlation: correlation,
	}
}
