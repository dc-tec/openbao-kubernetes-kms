//go:build e2e

package e2e

import (
	"context"
	"crypto/tls"
	"errors"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/auth"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/openbao"
	"github.com/dc-tec/openbao-kubernetes-kms/test/e2e/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("OpenBao Cert Auth CI", Label(framework.LabelOpenBao, framework.LabelCertAuth, framework.LabelCI), func() {
	It("validates TLS certificate auth against an ephemeral OpenBao environment", func(ctx SpecContext) {
		if !framework.OpenBaoCIEnabled() {
			Skip("E2E_OPENBAO_CI=true is required")
		}

		environment, err := framework.StartOpenBaoEnvironment(ctx, framework.OpenBaoEnvironmentConfig{
			CertAuth: true,
		})
		if errors.Is(err, framework.ErrDockerUnavailable) {
			Skip(err.Error())
		}
		Expect(err).NotTo(HaveOccurred())
		defer func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			Expect(environment.Close(cleanupCtx)).To(Succeed())
		}()

		cert, err := tls.LoadX509KeyPair(environment.ClientCertFile, environment.ClientKeyFile)
		Expect(err).NotTo(HaveOccurred())
		provider := staticClientCertificateProvider{cert: cert}
		source, err := auth.NewCertLoginSource(auth.CertLoginSourceConfig{
			MountPath:        environment.CertAuthMount,
			Name:             environment.CertAuthRole,
			Source:           "spiffe",
			MinRemainingTTL:  time.Minute,
			ClockSkewLeeway:  30 * time.Second,
			ExpectedSPIFFEID: environment.CertSPIFFEID,
			TrustDomain:      "example.org",
		}, provider)
		Expect(err).NotTo(HaveOccurred())

		authClient, err := environment.NewCertAuthClient()
		Expect(err).NotTo(HaveOccurred())
		login, err := source.Login(ctx, authClient, auth.RealClock{})
		Expect(err).NotTo(HaveOccurred())
		Expect(login.AuthToken.ClientToken).NotTo(BeEmpty())
		Expect(login.AuthToken.Renewable).To(BeTrue())

		client, err := environment.NewClientWithTokenSource(openbao.StaticTokenSource{
			TokenValue: login.AuthToken.ClientToken,
		})
		Expect(err).NotTo(HaveOccurred())
		validateOpenBaoTransit(ctx, client, environment.TransitMount, environment.TransitKey)

		_, err = authClient.LoginCert(ctx, openbao.CertLoginRequest{
			MountPath: environment.CertAuthMount,
			Name:      "wrong-role",
		})
		Expect(err).To(HaveOccurred())
	}, SpecTimeout(2*time.Minute))
})

type staticClientCertificateProvider struct {
	cert tls.Certificate
}

func (p staticClientCertificateProvider) CurrentCertificate(context.Context) (tls.Certificate, error) {
	return p.cert, nil
}
