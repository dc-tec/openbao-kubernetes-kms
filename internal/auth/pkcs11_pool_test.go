//go:build certauth_pkcs11

package auth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ThalesGroup/crypto11"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/config"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/openbao"
	"github.com/dc-tec/openbao-kubernetes-kms/test/fakes"
)

// TestPKCS11PoolTimeoutSoftHSM runs in the PKCS#11 E2E image against its test token.
func TestPKCS11PoolTimeoutSoftHSM(t *testing.T) {
	provider := softHSMTestProvider(t)
	clock := &fakeClock{now: time.Now()}
	client := &fakes.OpenBaoAuthClient{CertResponses: []openbao.AuthToken{
		{ClientToken: testBaoToken1, LeaseDuration: time.Minute},
		{ClientToken: testBaoToken2, LeaseDuration: time.Minute},
	}}
	source, err := NewCertLoginSource(CertLoginSourceConfig{
		MountPath: "auth/cert", Source: config.CertificateSourcePKCS11, MinRemainingTTL: time.Minute,
	}, provider)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewManagerWithSource(LifecycleConfig{
		LoginBeforeTokenExpiry: 30 * time.Second, TokenRenewalIncrement: time.Minute,
	}, source, client, ManagerOptions{
		LifecycleContext: t.Context(), Clock: clock, RefreshTimeout: time.Second,
		RefreshRetryJitter: noRetryJitter,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Token(t.Context()); err != nil {
		t.Fatal(err)
	}

	release := occupyPKCS11Pool(t, provider)
	assertPKCS11SignTimesOut(t, provider.signer, release)
	clock.advance(40 * time.Second)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	if token, err := manager.Token(ctx); err != nil || token != testBaoToken1 {
		t.Fatalf("pool exhaustion blocked a valid token: %v", err)
	}
	finishRefresh(t, manager)
	state := manager.State()
	if state.ConsecutiveFailures != 1 || state.LastError != ErrCertificateSignerProbe.Error()+": signer rejected probe" {
		t.Fatalf("pool exhaustion did not produce a redacted refresh failure: %#v", state)
	}
	if len(client.CertLogins()) != 1 {
		t.Fatal("failed signer probe reached OpenBao login")
	}

	clock.advance(21 * time.Second)
	if _, err := manager.Token(ctx); !errors.Is(err, ErrCertificateSignerProbe) {
		t.Fatalf("expired token did not fail closed on pool exhaustion: %v", err)
	}
	release()
	clock.advance(time.Minute)
	if token, err := manager.Token(ctx); err != nil || token != testBaoToken2 {
		t.Fatalf("login did not recover after session release: %v", err)
	}
	if manager.State().ConsecutiveFailures != 0 || len(client.CertLogins()) != 2 {
		t.Fatal("successful login did not clear pool exhaustion failure")
	}
}

func softHSMTestProvider(t *testing.T) *PKCS11CertificateProvider {
	t.Helper()
	module := os.Getenv("PKCS11_TEST_MODULE")
	if module == "" {
		t.Skip("PKCS11_TEST_MODULE is required; run the PKCS#11 SoftHSM E2E lane")
	}
	provider, err := NewPKCS11CertificateProvider(t.Context(), PKCS11ProviderConfig{
		ModulePath: module, CertificateFile: os.Getenv("PKCS11_TEST_CERTIFICATE"),
		TokenLabel: os.Getenv("PKCS11_TEST_TOKEN"), KeyLabel: os.Getenv("PKCS11_TEST_KEY"),
		PINFile: os.Getenv("PKCS11_TEST_PIN"), MaxSessions: 2, PoolWaitTimeout: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := provider.Close(); err != nil {
			t.Error(err)
		}
	})
	return provider.(*PKCS11CertificateProvider)
}

func occupyPKCS11Pool(t *testing.T, provider *PKCS11CertificateProvider) func() {
	t.Helper()
	// crypto11 reserves one persistent session. With MaxSessions=2, this CBC
	// operation holds the only pooled session until Close, without blocking in C.
	key, err := provider.ctx.GenerateSecretKey([]byte("pool-timeout-test"), 256, crypto11.CipherAES)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := key.Delete(); err != nil {
			t.Error(err)
		}
	})
	// No data is encrypted; the operation only acquires a session.
	held, err := key.NewCBCEncrypterCloser(make([]byte, crypto11.CipherAES.BlockSize))
	if err != nil {
		t.Fatal(err)
	}
	release := sync.OnceFunc(held.Close)
	t.Cleanup(release)
	return release
}

func assertPKCS11SignTimesOut(t *testing.T, signer crypto.Signer, release func()) {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		digest := sha256.Sum256([]byte(signerProbeMessage))
		_, err := signer.Sign(rand.Reader, digest[:], crypto.SHA256)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "resource pool timed out") {
			t.Fatalf("expected pool wait timeout: %v", err)
		}
	case <-time.After(2 * time.Second):
		// Release and drain before provider cleanup so a regression fails without
		// stranding a signing goroutine or closing an in-use PKCS#11 context.
		release()
		<-done
		t.Fatal("signing waited indefinitely for a pooled session")
	}
}
