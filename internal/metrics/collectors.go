package metrics

import (
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/status"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	rotationStateActive     = "active"
	rotationStatePending    = "pending"
	rotationStateUnknown    = "unknown"
	authMethodJWT           = "jwt"
	authMethodCert          = "cert"
	certificateSourceNone   = "none"
	certificateSourcePKCS11 = "pkcs11"
	certificateSourceSPIFFE = "spiffe"
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
	method   *prometheus.Desc
	source   *prometheus.Desc
	tokenTTL *prometheus.Desc
	certTTL  *prometheus.Desc
}

func newAuthCollector(provider AuthProvider) *authCollector {
	return &authCollector{
		provider: provider,
		method: prometheus.NewDesc(
			"openbao_kms_auth_method_info",
			"Configured bounded OpenBao auth method.",
			[]string{labelMethod},
			nil,
		),
		source: prometheus.NewDesc(
			"openbao_kms_certificate_source_info",
			"Configured bounded certificate auth source.",
			[]string{labelSource},
			nil,
		),
		tokenTTL: prometheus.NewDesc(
			"openbao_kms_token_ttl_seconds",
			"Remaining in-memory OpenBao token TTL.",
			nil,
			nil,
		),
		certTTL: prometheus.NewDesc(
			"openbao_kms_certificate_ttl_seconds",
			"Remaining cert-auth client certificate TTL. "+
				"Zero when certificate auth is not in use or no certificate has been observed.",
			nil,
			nil,
		),
	}
}

func (c *authCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.method
	ch <- c.source
	ch <- c.tokenTTL
	ch <- c.certTTL
}

func (c *authCollector) Collect(ch chan<- prometheus.Metric) {
	state := c.provider.State()
	ch <- prometheus.MustNewConstMetric(
		c.method,
		prometheus.GaugeValue,
		1,
		authMethodLabel(state.AuthMethod),
	)
	ch <- prometheus.MustNewConstMetric(
		c.source,
		prometheus.GaugeValue,
		1,
		certificateSourceLabel(state.CertificateSource),
	)
	ch <- prometheus.MustNewConstMetric(
		c.tokenTTL,
		prometheus.GaugeValue,
		nonNegativeSeconds(state.TokenTTL),
	)
	ch <- prometheus.MustNewConstMetric(
		c.certTTL,
		prometheus.GaugeValue,
		nonNegativeSeconds(state.CertTTL),
	)
}

type concurrencyCollector struct {
	provider ConcurrencyProvider
	inFlight *prometheus.Desc
}

func newConcurrencyCollector(provider ConcurrencyProvider) *concurrencyCollector {
	return &concurrencyCollector{
		provider: provider,
		inFlight: prometheus.NewDesc(
			"openbao_kms_grpc_in_flight",
			"Current KMS v2 gRPC handler count by method.",
			[]string{labelMethod},
			nil,
		),
	}
}

func (c *concurrencyCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.inFlight
}

func (c *concurrencyCollector) Collect(ch chan<- prometheus.Metric) {
	statusCount, encrypt, decrypt := c.provider.InFlightKMSRequests()
	ch <- prometheus.MustNewConstMetric(c.inFlight, prometheus.GaugeValue, float64(statusCount), "status")
	ch <- prometheus.MustNewConstMetric(c.inFlight, prometheus.GaugeValue, float64(encrypt), "encrypt")
	ch <- prometheus.MustNewConstMetric(c.inFlight, prometheus.GaugeValue, float64(decrypt), "decrypt")
}

func nonNegativeSeconds(value time.Duration) float64 {
	if value <= 0 {
		return 0
	}
	return value.Seconds()
}

func authMethodLabel(value string) string {
	switch normalize(value) {
	case authMethodJWT:
		return authMethodJWT
	case authMethodCert:
		return authMethodCert
	default:
		return statusUnknown
	}
}

func certificateSourceLabel(value string) string {
	switch normalize(value) {
	case "":
		return certificateSourceNone
	case certificateSourcePKCS11:
		return certificateSourcePKCS11
	case certificateSourceSPIFFE:
		return certificateSourceSPIFFE
	case statusUnknown:
		return certificateSourceNone
	default:
		return statusUnknown
	}
}

func circuitBreakerValue(state status.CircuitBreakerState) float64 {
	if state == status.CircuitBreakerOpen {
		return 1
	}
	return 0
}
