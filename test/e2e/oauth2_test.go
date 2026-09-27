//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/auth"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/oauth2"
	"github.com/dc-tec/openbao-kubernetes-kms/test/e2e/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("OAuth client credentials", func() {
	It("authenticates without Kubernetes and recovers after issuer and credential failures", func(ctx SpecContext) {
		if !framework.OpenBaoCIEnabled() {
			Skip("E2E_OPENBAO_CI=true is required")
		}
		environment, err := framework.StartOpenBaoEnvironment(ctx, framework.OpenBaoEnvironmentConfig{})
		if errors.Is(err, framework.ErrDockerUnavailable) {
			Skip(err.Error())
		}
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			Expect(environment.Close(cleanupCtx)).To(Succeed())
		})
		fixture := startOAuthIssuer(environment)
		DeferCleanup(fixture.server.Close)
		dir, err := os.MkdirTemp("", "bao-oauth-e2e-")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(os.RemoveAll(dir)).To(Succeed()) })
		caPath := filepath.Join(dir, "ca.pem")
		secretPath := filepath.Join(dir, "secret")
		caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: fixture.server.Certificate().Raw})
		Expect(os.WriteFile(caPath, caPEM, 0o600)).To(Succeed())
		Expect(os.WriteFile(secretPath, []byte("initial-secret"), 0o600)).To(Succeed())
		source, err := auth.NewOAuth2LoginSource(auth.ManagerConfig{
			MountPath: environment.AuthMount, Role: environment.AuthRole,
			MinJWTRemainingTTL: time.Minute, LoginBeforeTokenExpiry: 30 * time.Second, TokenRenewalIncrement: time.Hour,
			ExpectedIssuer: environment.JWTIssuer(), ExpectedAudience: []string{environment.JWTAudience()},
			ExpectedSubject: environment.JWTSubject(),
		}, oauth2.Config{
			TokenURL: fixture.server.URL + "/token", ClientID: "kms-provider", ClientSecretFile: secretPath,
			AuthMethod: oauth2.ClientSecretBasic, CACertFile: caPath, Timeout: 5 * time.Second,
		})
		Expect(err).NotTo(HaveOccurred())
		bao, err := environment.NewAuthClient()
		Expect(err).NotTo(HaveOccurred())
		manager, err := auth.NewManagerWithSource(auth.LifecycleConfig{
			LoginBeforeTokenExpiry: 30 * time.Second, TokenRenewalIncrement: time.Hour,
		},
			source, bao, auth.ManagerOptions{LifecycleContext: ctx, RefreshTimeout: 5 * time.Second})
		Expect(err).NotTo(HaveOccurred())
		client, err := environment.NewClientWithTokenSource(manager)
		Expect(err).NotTo(HaveOccurred())
		validateOpenBaoTransit(ctx, client, environment.TransitMount, environment.TransitKey)
		Expect(fixture.requests.Load()).To(Equal(int32(1)))
		fixture.unavailable.Store(true)
		Expect(manager.Refresh(ctx)).To(MatchError(ContainSubstring(oauth2.ErrRejected.Error())))
		// A fresh process has no OpenBao token to use during an issuer outage.
		_, err = source.Login(ctx, bao, auth.RealClock{})
		Expect(errors.Is(err, oauth2.ErrRejected)).To(BeTrue())
		fixture.unavailable.Store(false)
		fixture.secret.Store("replacement-secret")
		_, err = source.Login(ctx, bao, auth.RealClock{})
		Expect(errors.Is(err, oauth2.ErrRejected)).To(BeTrue())
		Expect(os.WriteFile(secretPath+".new", []byte("replacement-secret"), 0o600)).To(Succeed())
		Expect(os.Rename(secretPath+".new", secretPath)).To(Succeed())
		Eventually(func() error { return manager.Refresh(ctx) }, 10*time.Second, 100*time.Millisecond).Should(Succeed())
		validateOpenBaoTransit(ctx, client, environment.TransitMount, environment.TransitKey)
		fixture.badSignature.Store(true)
		_, err = source.Login(ctx, bao, auth.RealClock{})
		Expect(err).To(HaveOccurred())
		Expect(errors.Is(err, auth.ErrAuthFailed)).To(BeTrue())
	}, SpecTimeout(2*time.Minute))
}, Label(framework.LabelOpenBao, framework.LabelTransit, framework.LabelCI))

type oauthIssuerFixture struct {
	server       *httptest.Server
	secret       atomic.Value
	unavailable  atomic.Bool
	badSignature atomic.Bool
	requests     atomic.Int32
}

func startOAuthIssuer(environment *framework.OpenBaoEnvironment) *oauthIssuerFixture {
	f := &oauthIssuerFixture{}
	f.secret.Store("initial-secret")
	f.server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests.Add(1)
		if f.unavailable.Load() {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		clientID, secret, ok := r.BasicAuth()
		if !ok || clientID != "kms-provider" || secret != url.QueryEscape(f.secret.Load().(string)) {
			http.Error(w, "invalid_client", http.StatusUnauthorized)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		if err := r.ParseForm(); err != nil || r.PostForm.Get("grant_type") != "client_credentials" {
			http.Error(w, "invalid_request", http.StatusBadRequest)
			return
		}
		jwt, err := environment.IssueJWT(time.Now(), 5*time.Minute, framework.JWTClaimsOptions{})
		if err != nil {
			http.Error(w, "issuance failed", http.StatusInternalServerError)
			return
		}
		if f.badSignature.Load() {
			parts := strings.Split(jwt, ".")
			jwt = strings.Join(parts[:2], ".") + ".YmFk"
		}
		w.Header().Set("Content-Type", "application/json")
		// #nosec G117 -- the OAuth fixture intentionally returns its ephemeral token in the protocol response.
		_ = json.NewEncoder(w).Encode(struct {
			AccessToken string `json:"access_token"`
			TokenType   string `json:"token_type"`
			ExpiresIn   int    `json:"expires_in"`
		}{jwt, "Bearer", 300})
	}))
	return f
}
