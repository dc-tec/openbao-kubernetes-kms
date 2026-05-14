package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/auth"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/config"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/kmsv2"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/logging"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/metrics"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/openbao"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/status"
)

const (
	observationStatusOK = "ok"

	logMessageAuthLogin   = "auth.login"
	logMessageAuthRenewal = "auth.renewal"
	logMessageKMSRequest  = "kms.request"
	logMessageOpenBao     = "openbao.request"
	logMessageSocketStale = "socket.stale_removed"
	logMessageStatusProbe = "status.probe"

	logOperationAuthLogin   = "auth.login"
	logOperationAuthRenewal = "auth.renewal"
	logOperationKMSPrefix   = "kms."
	logOperationOpenBao     = "openbao.request"
	logOperationSocketStale = "socket.stale_removed"
	logOperationStatus      = "status.probe"
)

type observability struct {
	logger      *logging.Logger
	metrics     *metrics.Recorder
	correlation debugCorrelation
}

type debugCorrelation struct {
	enabled              bool
	expiresAt            time.Time
	incidentID           string
	logOpenBaoRequestIDs bool
	now                  func() time.Time
}

func newDebugCorrelation(
	cfg config.DebugCorrelationConfig,
	logOpenBaoRequestIDs bool,
	startedAt time.Time,
) debugCorrelation {
	return debugCorrelation{
		enabled:              cfg.Enabled,
		expiresAt:            startedAt.Add(cfg.TTL),
		incidentID:           cfg.IncidentID,
		logOpenBaoRequestIDs: logOpenBaoRequestIDs,
		now:                  time.Now,
	}
}

func (c debugCorrelation) active() bool {
	if !c.enabled {
		return false
	}
	now := time.Now
	if c.now != nil {
		now = c.now
	}
	return now().Before(c.expiresAt)
}

func (c debugCorrelation) appendFields(attrs []slog.Attr) []slog.Attr {
	if !c.active() {
		return attrs
	}
	attrs = appendStringAttr(attrs, logging.FieldCorrelationID, c.incidentID)
	if !c.expiresAt.IsZero() {
		attrs = append(attrs, logging.String(logging.FieldCorrelationExpiry, c.expiresAt.UTC().Format(time.RFC3339)))
	}
	return attrs
}

func (o observability) ObserveKMSRequest(ctx context.Context, obs kmsv2.RequestObservation) {
	o.metrics.RecordGRPCRequest(obs.Method, obs.Status, obs.Duration)
	if obs.PanicRecovered {
		o.metrics.RecordPanicRecovery(obs.Method)
	}

	attrs := []slog.Attr{
		logging.String(logging.FieldOperation, logOperationKMSPrefix+obs.Method),
		logging.String(logging.FieldStatus, obs.Status),
		logging.DurationMilliseconds(logging.FieldDurationMS, obs.Duration),
	}
	attrs = appendStringAttr(attrs, logging.FieldKeyIDHash, obs.KeyIDHash)
	if obs.PanicRecovered {
		attrs = append(attrs, logging.Bool(logging.FieldPanicRecovered, true))
		attrs = appendStringAttr(attrs, logging.FieldPanicType, obs.PanicType)
	}
	if obs.TransitKeyVersion > 0 {
		attrs = append(attrs, logging.Int(logging.FieldTransitKeyVersion, obs.TransitKeyVersion))
	}
	if o.correlation.active() {
		attrs = appendStringAttr(attrs, logging.FieldRequestUIDHash, obs.RequestUIDHash)
		attrs = o.correlation.appendFields(attrs)
	}
	attrs = appendStringAttr(attrs, logging.FieldErrorClass, obs.ErrorClass)
	attrs = appendStringAttr(attrs, logging.FieldHealthz, obs.Healthz)

	if obs.Status == observationStatusOK {
		o.logger.Debug(ctx, logMessageKMSRequest, attrs...)
		return
	}
	o.logger.Warn(ctx, logMessageKMSRequest, attrs...)
}

func (o observability) ObserveAADValidationError(reason string) {
	o.metrics.RecordAADValidationError(reason)
}

func (o observability) ObserveDecryptKeyIDError(reason string) {
	o.metrics.RecordDecryptKeyIDError(reason)
}

func (o observability) ObserveOpenBaoRequest(ctx context.Context, obs openbao.RequestObservation) {
	o.metrics.RecordOpenBaoRequest(obs.Operation, obs.Status, obs.Duration)

	attrs := []slog.Attr{
		logging.String(logging.FieldOperation, logOperationOpenBao),
		logging.String(logging.FieldOpenBaoOperation, obs.Operation),
		logging.String(logging.FieldStatus, obs.Status),
		logging.DurationMilliseconds(logging.FieldDurationMS, obs.Duration),
	}
	if obs.ErrorClass != "" {
		attrs = append(attrs, logging.String(logging.FieldErrorClass, string(obs.ErrorClass)))
	}
	if o.correlation.active() && o.correlation.logOpenBaoRequestIDs {
		attrs = appendStringAttr(attrs, logging.FieldOpenBaoRequestID, obs.RequestID)
		attrs = o.correlation.appendFields(attrs)
	}

	if obs.Status == observationStatusOK {
		o.logger.Debug(ctx, logMessageOpenBao, attrs...)
		return
	}
	o.logger.Warn(ctx, logMessageOpenBao, attrs...)
}

func (o observability) ObserveAuthLogin(ctx context.Context, authStatus string) {
	o.metrics.RecordAuthLogin(authStatus)
	attrs := []slog.Attr{
		logging.String(logging.FieldOperation, logOperationAuthLogin),
		logging.String(logging.FieldStatus, authStatus),
	}
	if authStatus == observationStatusOK {
		o.logger.Info(ctx, logMessageAuthLogin, attrs...)
		return
	}
	o.logger.Warn(ctx, logMessageAuthLogin, attrs...)
}

func (o observability) ObserveAuthRenewal(ctx context.Context, authStatus string) {
	o.metrics.RecordAuthRenewal(authStatus)
	attrs := []slog.Attr{
		logging.String(logging.FieldOperation, logOperationAuthRenewal),
		logging.String(logging.FieldStatus, authStatus),
	}
	if authStatus == observationStatusOK {
		o.logger.Debug(ctx, logMessageAuthRenewal, attrs...)
		return
	}
	o.logger.Warn(ctx, logMessageAuthRenewal, attrs...)
}

func (o observability) ObserveStatusProbe(ctx context.Context, obs status.ProbeObservation) {
	if obs.Kind == status.ProbeKindMetadata {
		o.metrics.RecordTransitMetadataObservation(obs.Status)
	}
	attrs := []slog.Attr{
		logging.String(logging.FieldOperation, logOperationStatus),
		logging.String(logging.FieldStatus, obs.Status),
		logging.String(logging.FieldProbeKind, string(obs.Kind)),
		logging.DurationMilliseconds(logging.FieldDurationMS, obs.Duration),
	}
	if obs.Status == observationStatusOK {
		o.logger.Debug(ctx, logMessageStatusProbe, attrs...)
		return
	}
	o.logger.Warn(ctx, logMessageStatusProbe, attrs...)
}

func (o observability) ObserveSocketRestart(ctx context.Context) {
	o.metrics.RecordSocketRestart()
	o.logger.Warn(ctx, logMessageSocketStale,
		logging.String(logging.FieldOperation, logOperationSocketStale),
		logging.String(logging.FieldStatus, observationStatusOK),
	)
}

func appendStringAttr(attrs []slog.Attr, key string, value string) []slog.Attr {
	if value == "" {
		return attrs
	}
	return append(attrs, logging.String(key, value))
}

var (
	_ kmsv2.Observer          = observability{}
	_ openbao.RequestObserver = observability{}
	_ auth.Observer           = observability{}
	_ status.ProbeObserver    = observability{}
)
