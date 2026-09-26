//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/auth"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/openbao"
	"github.com/dc-tec/openbao-kubernetes-kms/test/e2e/framework"
)

func TestProviderManagedTokenRecoveryE2E(t *testing.T) {
	requireOpenBaoCI(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	environment, manager, client, old := startManagedTokenRecovery(t, ctx)
	request := openbao.EncryptRequest{
		MountPath:      environment.TransitMount,
		KeyName:        environment.TransitKey,
		Plaintext:      []byte("auth recovery test"),
		AssociatedData: []byte("auth recovery scope"),
		KeyVersion:     1,
	}
	encrypted, err := client.Encrypt(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if err := environment.RevokeToken(ctx, old); err != nil {
		t.Fatal(err)
	}
	staleClient, err := environment.NewClientWithTokenSource(openbao.StaticTokenSource{TokenValue: old})
	if err != nil {
		t.Fatal(err)
	}
	// The pinned server reports token revocation with the same 403 class as ACL denial.
	_, err = staleClient.ReadKeyProfile(ctx, environment.TransitMount, environment.TransitKey)
	if !errors.Is(err, &openbao.Error{Class: openbao.ErrorClassPermissionDenied}) {
		t.Fatalf("revocation was not applied: %v", err)
	}
	decrypted, err := client.Decrypt(ctx, openbao.DecryptRequest{
		MountPath: request.MountPath, KeyName: request.KeyName,
		Ciphertext: encrypted.Ciphertext, AssociatedData: request.AssociatedData,
	})
	if err != nil || !bytes.Equal(decrypted.Plaintext, request.Plaintext) {
		t.Fatalf("decrypt did not recover before TTL expiry: %v", err)
	}
	replacement, err := manager.Token(ctx)
	if err != nil || replacement == old {
		t.Fatal("revoked token was not replaced")
	}
	if _, err := client.Encrypt(ctx, request); err != nil {
		t.Fatalf("encrypt after recovery: %v", err)
	}
	if err := environment.InstallProviderPolicy(ctx, environment.MetadataOnlyProviderPolicy()); err != nil {
		t.Fatal(err)
	}
	// A denied replacement remains a policy error, and repeated requests during
	// cooldown do not cause a new login for each request.
	for range 4 {
		_, err := client.Encrypt(ctx, request)
		if !errors.Is(err, &openbao.Error{Class: openbao.ErrorClassPermissionDenied}) {
			t.Fatalf("policy denial misclassified: %v", err)
		}
	}
	current, err := manager.Token(ctx)
	if err != nil || current != replacement {
		t.Fatal("policy denial bypassed recovery cooldown")
	}
}

func TestProviderTokenRevocationRecoveryE2E(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), providerFailureDefaultTimeout)
	defer cancel()
	stack := startProviderFailureStack(t, ctx, "obk-e2e-revocation", providerFailureStackOptions{})
	stack.runClient(ctx, "write-client", kmsClientModeWriteSample, sampleReadWrite)
	if err := stack.environment.RevokeProviderTokens(ctx); err != nil {
		t.Fatalf("revoke provider auth leases: %v", err)
	}
	stack.runClient(ctx, "read-client", kmsClientModeReadSample, sampleReadOnly)
	stack.runClient(ctx, "recovered-client", kmsClientModeFullStack, sampleNotMounted)
}

func startManagedTokenRecovery(
	t *testing.T,
	ctx context.Context,
) (*framework.OpenBaoEnvironment, *auth.Manager, *openbao.Client, string) {
	t.Helper()
	environment, err := framework.StartOpenBaoEnvironment(ctx, framework.OpenBaoEnvironmentConfig{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		if err := environment.Close(cleanupCtx); err != nil {
			t.Error(err)
		}
	})
	authClient, err := environment.NewAuthClient()
	if err != nil {
		t.Fatal(err)
	}
	manager, err := auth.NewManager(auth.ManagerConfig{
		MountPath: environment.AuthMount, Role: environment.AuthRole, JWTFile: environment.JWTFile,
		MinJWTRemainingTTL: time.Minute, LoginBeforeTokenExpiry: 30 * time.Second, TokenRenewalIncrement: time.Hour,
	}, authClient, auth.ManagerOptions{LifecycleContext: ctx, RefreshTimeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	old, err := manager.Token(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if manager.State().TokenTTL < time.Minute {
		t.Fatal("test requires token outside early refresh window")
	}
	client, err := environment.NewClientWithTokenSource(manager)
	if err != nil {
		t.Fatal(err)
	}
	return environment, manager, client, old
}
