// Package metrics owns Prometheus metrics with bounded labels.
package metrics

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/auth"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/status"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	statusUnknown = "unknown"

	reasonUnknown = "unknown"
	// PathMetrics is the HTTP path served for Prometheus scrapes.
	PathMetrics = "/metrics"

	labelHash      = "hash"
	labelMethod    = "method"
	labelOperation = "operation"
	labelReason    = "reason"
	labelSource    = "source"
	labelState     = "state"
	labelStatus    = "status"
)

var requestDurationBuckets = prometheus.ExponentialBuckets(0.001, 2, 12)

// StatusProvider exposes redacted status diagnostics for collection.
type StatusProvider interface {
	DiagnosticsSnapshot() status.Diagnostics
}

// AuthProvider exposes redacted auth state for collection.
type AuthProvider interface {
	State() auth.State
}

// Recorder owns all provider Prometheus collectors.
type Recorder struct {
	registry *prometheus.Registry

	grpcRequests         *prometheus.CounterVec
	grpcDuration         *prometheus.HistogramVec
	openbaoRequests      *prometheus.CounterVec
	openbaoDuration      *prometheus.HistogramVec
	authRenewal          *prometheus.CounterVec
	authLogin            *prometheus.CounterVec
	socketRestarts       prometheus.Counter
	metadataObservation  *prometheus.CounterVec
	aadValidationErrors  *prometheus.CounterVec
	decryptKeyIDErrors   *prometheus.CounterVec
	statusProviderLoaded bool
	authProviderLoaded   bool
}

// NewRecorder creates an isolated registry with provider metrics.
func NewRecorder() (*Recorder, error) {
	recorder := &Recorder{
		registry: prometheus.NewRegistry(),
		grpcRequests: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "openbao_kms_grpc_requests_total",
				Help: "Total KMS v2 gRPC requests by method and bounded status.",
			},
			[]string{labelMethod, labelStatus},
		),
		grpcDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "openbao_kms_grpc_duration_seconds",
				Help:    "KMS v2 gRPC request duration by method.",
				Buckets: requestDurationBuckets,
			},
			[]string{labelMethod},
		),
		openbaoRequests: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "openbao_kms_openbao_requests_total",
				Help: "Total OpenBao HTTP requests by operation and bounded status.",
			},
			[]string{labelOperation, labelStatus},
		),
		openbaoDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "openbao_kms_openbao_duration_seconds",
				Help:    "OpenBao HTTP request duration by operation.",
				Buckets: requestDurationBuckets,
			},
			[]string{labelOperation},
		),
		authRenewal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "openbao_kms_auth_renewal_total",
				Help: "Total OpenBao token renewal attempts by bounded status.",
			},
			[]string{labelStatus},
		),
		authLogin: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "openbao_kms_auth_login_total",
				Help: "Total OpenBao auth-method login attempts by bounded status.",
			},
			[]string{labelStatus},
		),
		socketRestarts: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "openbao_kms_socket_restarts_total",
				Help: "Total verified-dead Unix sockets safely removed before bind.",
			},
		),
		metadataObservation: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "openbao_kms_transit_metadata_observation_total",
				Help: "Total Transit metadata probe observations by bounded status.",
			},
			[]string{labelStatus},
		),
		aadValidationErrors: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "openbao_kms_aad_validation_errors_total",
				Help: "Total decrypt-side AAD or annotation validation errors by bounded reason.",
			},
			[]string{labelReason},
		),
		decryptKeyIDErrors: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "openbao_kms_decrypt_key_id_errors_total",
				Help: "Total decrypt-side key ID validation errors by bounded reason.",
			},
			[]string{labelReason},
		),
	}
	if err := recorder.registerBaseCollectors(); err != nil {
		return nil, err
	}
	return recorder, nil
}

// Handler returns the Prometheus scrape handler for this registry.
func (r *Recorder) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle(PathMetrics, promhttp.HandlerFor(r.registry, promhttp.HandlerOpts{}))
	return mux
}

// RegisterStatusProvider registers redacted status collectors.
func (r *Recorder) RegisterStatusProvider(provider StatusProvider) error {
	if provider == nil {
		return fmt.Errorf("status provider is required")
	}
	if r.statusProviderLoaded {
		return fmt.Errorf("status provider already registered")
	}
	if err := r.registry.Register(newDiagnosticsCollector(provider)); err != nil {
		return err
	}
	r.statusProviderLoaded = true
	return nil
}

// RegisterAuthProvider registers redacted auth collectors.
func (r *Recorder) RegisterAuthProvider(provider AuthProvider) error {
	if provider == nil {
		return fmt.Errorf("auth provider is required")
	}
	if r.authProviderLoaded {
		return fmt.Errorf("auth provider already registered")
	}
	if err := r.registry.Register(newAuthCollector(provider)); err != nil {
		return err
	}
	r.authProviderLoaded = true
	return nil
}

// RecordGRPCRequest records one KMS v2 gRPC request.
func (r *Recorder) RecordGRPCRequest(method string, requestStatus string, duration time.Duration) {
	methodLabel := normalize(method)
	r.grpcRequests.WithLabelValues(methodLabel, normalizeStatus(requestStatus)).Inc()
	r.grpcDuration.WithLabelValues(methodLabel).Observe(duration.Seconds())
}

// RecordOpenBaoRequest records one OpenBao HTTP request.
func (r *Recorder) RecordOpenBaoRequest(operation string, requestStatus string, duration time.Duration) {
	operationLabel := normalize(operation)
	r.openbaoRequests.WithLabelValues(operationLabel, normalizeStatus(requestStatus)).Inc()
	r.openbaoDuration.WithLabelValues(operationLabel).Observe(duration.Seconds())
}

// RecordAuthLogin records one auth-method login attempt.
func (r *Recorder) RecordAuthLogin(requestStatus string) {
	r.authLogin.WithLabelValues(normalizeStatus(requestStatus)).Inc()
}

// RecordAuthRenewal records one token renewal attempt.
func (r *Recorder) RecordAuthRenewal(requestStatus string) {
	r.authRenewal.WithLabelValues(normalizeStatus(requestStatus)).Inc()
}

// RecordSocketRestart records one verified-dead stale socket cleanup.
func (r *Recorder) RecordSocketRestart() {
	r.socketRestarts.Inc()
}

// RecordTransitMetadataObservation records one status-controller metadata observation.
func (r *Recorder) RecordTransitMetadataObservation(requestStatus string) {
	r.metadataObservation.WithLabelValues(normalizeStatus(requestStatus)).Inc()
}

// RecordAADValidationError records one bounded AAD validation reason.
func (r *Recorder) RecordAADValidationError(reason string) {
	r.aadValidationErrors.WithLabelValues(normalizeReason(reason)).Inc()
}

// RecordDecryptKeyIDError records one bounded key ID validation reason.
func (r *Recorder) RecordDecryptKeyIDError(reason string) {
	r.decryptKeyIDErrors.WithLabelValues(normalizeReason(reason)).Inc()
}

func (r *Recorder) registerBaseCollectors() error {
	collectors := []prometheus.Collector{
		r.grpcRequests,
		r.grpcDuration,
		r.openbaoRequests,
		r.openbaoDuration,
		r.authRenewal,
		r.authLogin,
		r.socketRestarts,
		r.metadataObservation,
		r.aadValidationErrors,
		r.decryptKeyIDErrors,
	}
	for _, collector := range collectors {
		if err := r.registry.Register(collector); err != nil {
			return err
		}
	}
	return nil
}

func normalize(value string) string {
	trimmed := strings.TrimSpace(strings.ToLower(value))
	if trimmed == "" {
		return statusUnknown
	}
	var builder strings.Builder
	validChars := 0
	for _, char := range trimmed {
		if isLabelChar(char) {
			builder.WriteRune(char)
			validChars++
			continue
		}
		builder.WriteByte('_')
	}
	if validChars == 0 {
		return statusUnknown
	}
	return builder.String()
}

func normalizeStatus(value string) string {
	return normalize(value)
}

func normalizeReason(value string) string {
	normalized := normalize(value)
	if normalized == statusUnknown {
		return reasonUnknown
	}
	return normalized
}

func isLabelChar(char rune) bool {
	return char >= 'a' && char <= 'z' ||
		char >= '0' && char <= '9' ||
		char == '_'
}
