package logging_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/logging"
)

func TestLoggerEmitsJSONWithStableFields(t *testing.T) {
	var out bytes.Buffer
	logger, err := logging.New(logging.Options{
		Level:  "debug",
		Format: logging.FormatJSON,
		Output: &out,
	})
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}

	logger.Info(context.Background(), "kms.request",
		logging.String(logging.FieldOperation, "kms.decrypt"),
		logging.String(logging.FieldStatus, "ok"),
		logging.String(logging.FieldKeyIDHash, "safe-key-hash"),
		logging.DurationMilliseconds(logging.FieldDurationMS, 1500*time.Microsecond),
	)

	var record struct {
		Timestamp string  `json:"ts"`
		Level     string  `json:"level"`
		Message   string  `json:"message"`
		Operation string  `json:"operation"`
		Status    string  `json:"status"`
		KeyIDHash string  `json:"key_id_hash"`
		Duration  float64 `json:"duration_ms"`
	}
	if err := json.Unmarshal(out.Bytes(), &record); err != nil {
		t.Fatalf("log output is not JSON: %v\n%s", err, out.String())
	}
	if record.Timestamp == "" || record.Level != "INFO" || record.Message != "kms.request" {
		t.Fatalf("unexpected base fields: %#v", record)
	}
	if record.Operation != "kms.decrypt" || record.Status != "ok" || record.KeyIDHash != "safe-key-hash" {
		t.Fatalf("unexpected structured fields: %#v", record)
	}
	if record.Duration != 1.5 {
		t.Fatalf("unexpected duration: %f", record.Duration)
	}
}

func TestRedactedStringDoesNotLogSensitiveValue(t *testing.T) {
	var out bytes.Buffer
	logger, err := logging.New(logging.Options{
		Level:  "debug",
		Format: logging.FormatJSON,
		Output: &out,
	})
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}

	logger.Warn(context.Background(), "auth.login",
		logging.String(logging.FieldOperation, "auth.login"),
		logging.String(logging.FieldStatus, "auth_failed"),
		logging.RedactedString("credential_present"),
	)

	output := out.String()
	if !strings.Contains(output, logging.RedactedValue) {
		t.Fatalf("redaction marker missing:\n%s", output)
	}
	for _, forbidden := range []string{
		"eyJhbGciOi", "bao-token-value", "vault:v1:full-ciphertext", "plaintext-value",
	} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("log output leaked %q:\n%s", forbidden, output)
		}
	}
}
