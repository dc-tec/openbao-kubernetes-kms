// Package logging owns structured, redacted process logging helpers.
package logging

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"
)

const (
	// FormatJSON emits newline-delimited JSON logs.
	FormatJSON = "json"
	// FormatText emits slog text logs for local debugging.
	FormatText = "text"

	fieldTimestamp         = "ts"
	fieldLevel             = "level"
	fieldMessage           = "message"
	FieldOperation         = "operation"
	FieldStatus            = "status"
	FieldDurationMS        = "duration_ms"
	FieldKeyIDHash         = "key_id_hash"
	FieldTransitKeyVersion = "transit_key_version"
	FieldRequestUIDHash    = "request_uid_hash"
	FieldErrorClass        = "error_class"
	FieldHealthz           = "healthz"
	FieldOpenBaoOperation  = "openbao_operation"
	FieldOpenBaoRequestID  = "openbao_request_id"
	FieldProbeKind         = "probe_kind"
	FieldCorrelationID     = "debug_correlation_incident"
	FieldCorrelationExpiry = "debug_correlation_expires_at"
	FieldPanicRecovered    = "panic_recovered"
	FieldPanicType         = "panic_type"

	// RedactedValue is the only placeholder used when a caller must acknowledge
	// sensitive material without logging its value.
	RedactedValue = "[redacted]"
)

var errInvalidConfig = errors.New("logging config invalid")

// Options controls structured logger construction.
type Options struct {
	Level  string
	Format string
	Output io.Writer
}

// Logger wraps slog with the provider's stable field policy.
type Logger struct {
	logger *slog.Logger
}

// New builds a structured logger with stable field names.
func New(opts Options) (*Logger, error) {
	if opts.Output == nil {
		return nil, fmt.Errorf("%w: output writer is required", errInvalidConfig)
	}
	level, err := parseLevel(opts.Level)
	if err != nil {
		return nil, err
	}
	handlerOpts := &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
			switch attr.Key {
			case slog.TimeKey:
				attr.Key = fieldTimestamp
			case slog.LevelKey:
				attr.Key = fieldLevel
			case slog.MessageKey:
				attr.Key = fieldMessage
			}
			return attr
		},
	}

	var handler slog.Handler
	switch opts.Format {
	case "", FormatJSON:
		handler = slog.NewJSONHandler(opts.Output, handlerOpts)
	case FormatText:
		handler = slog.NewTextHandler(opts.Output, handlerOpts)
	default:
		return nil, fmt.Errorf("%w: format must be json or text", errInvalidConfig)
	}
	return &Logger{logger: slog.New(handler)}, nil
}

// Log records one structured event.
func (l *Logger) Log(
	ctx context.Context,
	level slog.Level,
	message string,
	attrs ...slog.Attr,
) {
	if l == nil || l.logger == nil {
		return
	}
	l.logger.LogAttrs(ctx, level, message, attrs...)
}

// Debug records a debug event.
func (l *Logger) Debug(ctx context.Context, message string, attrs ...slog.Attr) {
	l.Log(ctx, slog.LevelDebug, message, attrs...)
}

// Info records an info event.
func (l *Logger) Info(ctx context.Context, message string, attrs ...slog.Attr) {
	l.Log(ctx, slog.LevelInfo, message, attrs...)
}

// Warn records a warning event.
func (l *Logger) Warn(ctx context.Context, message string, attrs ...slog.Attr) {
	l.Log(ctx, slog.LevelWarn, message, attrs...)
}

// Error records an error event.
func (l *Logger) Error(ctx context.Context, message string, attrs ...slog.Attr) {
	l.Log(ctx, slog.LevelError, message, attrs...)
}

// String records a stable string field.
func String(key string, value string) slog.Attr {
	return slog.String(key, value)
}

// RedactedString records a stable redaction placeholder.
func RedactedString(key string) slog.Attr {
	return slog.String(key, RedactedValue)
}

// Int records a stable integer field.
func Int(key string, value int) slog.Attr {
	return slog.Int(key, value)
}

// Bool records a stable boolean field.
func Bool(key string, value bool) slog.Attr {
	return slog.Bool(key, value)
}

// DurationMilliseconds records a duration as milliseconds.
func DurationMilliseconds(key string, value time.Duration) slog.Attr {
	return slog.Float64(key, float64(value.Microseconds())/1000)
}

func parseLevel(value string) (slog.Level, error) {
	switch value {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("%w: level must be debug, info, warn, or error", errInvalidConfig)
	}
}
