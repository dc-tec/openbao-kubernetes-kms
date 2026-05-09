package metrics

import (
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/status"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	rotationStateActive  = "active"
	rotationStatePending = "pending"
	rotationStateUnknown = "unknown"
)

type diagnosticsCollector struct {
	provider StatusProvider

	statusKeyIDHash     *prometheus.Desc
	keyVersion          *prometheus.Desc
	statusCacheAge      *prometheus.Desc
	circuitBreakerState *prometheus.Desc
	rotationState       *prometheus.Desc
}

func newDiagnosticsCollector(provider StatusProvider) *diagnosticsCollector {
	return &diagnosticsCollector{
		provider: provider,
		statusKeyIDHash: prometheus.NewDesc(
			"openbao_kms_status_key_id_hash",
			"Current active Kubernetes key ID hash.",
			[]string{labelHash},
			nil,
		),
		keyVersion: prometheus.NewDesc(
			"openbao_kms_key_version",
			"Current active OpenBao Transit key version.",
			nil,
			nil,
		),
		statusCacheAge: prometheus.NewDesc(
			"openbao_kms_status_cache_age_seconds",
			"Age of the cached KMS Status probe result.",
			nil,
			nil,
		),
		circuitBreakerState: prometheus.NewDesc(
			"openbao_kms_circuit_breaker_state",
			"Status probe circuit breaker state: 0 closed, 1 open.",
			nil,
			nil,
		),
		rotationState: prometheus.NewDesc(
			"openbao_kms_rotation_state",
			"Current bounded rotation state.",
			[]string{labelState},
			nil,
		),
	}
}

func (c *diagnosticsCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.statusKeyIDHash
	ch <- c.keyVersion
	ch <- c.statusCacheAge
	ch <- c.circuitBreakerState
	ch <- c.rotationState
}

func (c *diagnosticsCollector) Collect(ch chan<- prometheus.Metric) {
	diagnostics := c.provider.DiagnosticsSnapshot()
	if diagnostics.ActiveKeyIDHash != "" {
		ch <- prometheus.MustNewConstMetric(
			c.statusKeyIDHash,
			prometheus.GaugeValue,
			1,
			diagnostics.ActiveKeyIDHash,
		)
	}
	ch <- prometheus.MustNewConstMetric(
		c.keyVersion,
		prometheus.GaugeValue,
		float64(diagnostics.ActiveTransitVersion),
	)
	ch <- prometheus.MustNewConstMetric(
		c.statusCacheAge,
		prometheus.GaugeValue,
		nonNegativeSeconds(diagnostics.CacheAge),
	)
	ch <- prometheus.MustNewConstMetric(
		c.circuitBreakerState,
		prometheus.GaugeValue,
		circuitBreakerValue(diagnostics.CircuitBreaker.State),
	)
	for _, stateName := range []string{rotationStateActive, rotationStatePending, rotationStateUnknown} {
		value := 0.0
		if normalize(string(diagnostics.RotationState)) == stateName {
			value = 1
		}
		ch <- prometheus.MustNewConstMetric(
			c.rotationState,
			prometheus.GaugeValue,
			value,
			stateName,
		)
	}
}

type authCollector struct {
	provider AuthProvider
	tokenTTL *prometheus.Desc
}

func newAuthCollector(provider AuthProvider) *authCollector {
	return &authCollector{
		provider: provider,
		tokenTTL: prometheus.NewDesc(
			"openbao_kms_token_ttl_seconds",
			"Remaining in-memory OpenBao token TTL.",
			nil,
			nil,
		),
	}
}

func (c *authCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.tokenTTL
}

func (c *authCollector) Collect(ch chan<- prometheus.Metric) {
	state := c.provider.State()
	ch <- prometheus.MustNewConstMetric(
		c.tokenTTL,
		prometheus.GaugeValue,
		nonNegativeSeconds(state.TokenTTL),
	)
}

func nonNegativeSeconds(value time.Duration) float64 {
	if value <= 0 {
		return 0
	}
	return value.Seconds()
}

func circuitBreakerValue(state status.CircuitBreakerState) float64 {
	if state == status.CircuitBreakerOpen {
		return 1
	}
	return 0
}
