package metrics_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/auth"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/config"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/metrics"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/version"
)

func TestTypedAuthValuesPreserveBoundedMetricLabels(t *testing.T) {
	for _, tc := range []struct {
		method      config.AuthMethod
		source      config.CertificateSource
		methodLabel string
		sourceLabel string
	}{
		{config.AuthMethodJWT, "", "jwt", "none"},
		{config.AuthMethodCert, config.CertificateSourcePKCS11, "cert", "pkcs11"},
		{config.AuthMethodCert, config.CertificateSourceSPIFFE, "cert", "spiffe"},
		{"sensitive-method", "sensitive-source", "unknown", "unknown"},
		{"unknown", "unknown", "unknown", "none"},
		{" JWT ", " PKCS11 ", "jwt", "pkcs11"},
	} {
		t.Run(string(tc.method)+"/"+string(tc.source), func(t *testing.T) {
			recorder, err := metrics.NewRecorder(version.Info{})
			if err != nil {
				t.Fatal(err)
			}
			provider := authStateProvider{auth.State{AuthMethod: tc.method, CertificateSource: tc.source}}
			if err := recorder.RegisterAuthProvider(provider); err != nil {
				t.Fatal(err)
			}
			output := scrapeMetrics(t, recorder.Handler())
			for _, want := range []string{
				fmt.Sprintf("openbao_kms_auth_method_info{method=%q} 1", tc.methodLabel),
				fmt.Sprintf("openbao_kms_certificate_source_info{source=%q} 1", tc.sourceLabel),
			} {
				if !strings.Contains(output, want) {
					t.Fatalf("missing bounded metric %s", want)
				}
			}
			if strings.Contains(output, "sensitive") {
				t.Fatal("metrics contain unrecognized auth values")
			}
		})
	}
}

type authStateProvider struct {
	state auth.State
}

func (p authStateProvider) State() auth.State { return p.state }
