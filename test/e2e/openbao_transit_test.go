//go:build e2e

package e2e

import (
	"context"
	"errors"
	"path"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/auth"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/openbao"
	"github.com/dc-tec/openbao-kubernetes-kms/test/e2e/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("OpenBao Transit CI", Label(framework.LabelOpenBao, framework.LabelTransit, framework.LabelCI), func() {
	It("validates Transit behavior against an ephemeral OpenBao environment", func(ctx SpecContext) {
		if !framework.OpenBaoCIEnabled() {
			Skip("E2E_OPENBAO_CI=true is required")
		}

		environment, err := framework.StartOpenBaoEnvironment(ctx, framework.OpenBaoEnvironmentConfig{})
		if errors.Is(err, framework.ErrDockerUnavailable) {
			Skip(err.Error())
		}
		Expect(err).NotTo(HaveOccurred())
		defer func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			Expect(environment.Close(cleanupCtx)).To(Succeed())
		}()

		client, err := environment.NewClient()
		Expect(err).NotTo(HaveOccurred())
		validateOpenBaoTransit(ctx, client, environment.TransitMount, environment.TransitKey)
	}, SpecTimeout(90*time.Second))

	It("validates JWT auth bootstrap against an ephemeral OpenBao environment", func(ctx SpecContext) {
		if !framework.OpenBaoCIEnabled() {
			Skip("E2E_OPENBAO_CI=true is required")
		}

		environment, err := framework.StartOpenBaoEnvironment(ctx, framework.OpenBaoEnvironmentConfig{})
		if errors.Is(err, framework.ErrDockerUnavailable) {
			Skip(err.Error())
		}
		Expect(err).NotTo(HaveOccurred())
		defer func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			Expect(environment.Close(cleanupCtx)).To(Succeed())
		}()

		authClient, err := environment.NewAuthClient()
		Expect(err).NotTo(HaveOccurred())
		manager, err := auth.NewManager(auth.ManagerConfig{
			MountPath:              environment.AuthMount,
			Role:                   environment.AuthRole,
			JWTFile:                environment.JWTFile,
			MinJWTRemainingTTL:     2 * time.Minute,
			ClockSkewLeeway:        30 * time.Second,
			LoginBeforeTokenExpiry: 30 * time.Second,
			TokenRenewalIncrement:  time.Hour,
		}, authClient, auth.ManagerOptions{})
		Expect(err).NotTo(HaveOccurred())

		token, err := manager.Token(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(token).NotTo(BeEmpty())
		Expect(manager.State().Status).To(Equal(auth.StatusAuthenticated))

		client, err := environment.NewClientWithTokenSource(manager)
		Expect(err).NotTo(HaveOccurred())
		validateOpenBaoTransit(ctx, client, environment.TransitMount, environment.TransitKey)
	}, SpecTimeout(90*time.Second))
})

func validateOpenBaoTransit(ctx SpecContext, client *openbao.Client, mountPath string, keyName string) {
	By("reading Transit key metadata")
	profile, err := client.ReadKeyProfile(ctx, mountPath, keyName)
	Expect(err).NotTo(HaveOccurred())
	Expect(profile.LatestVersion).To(BeNumerically(">", 0))

	By("checking disable_upsert")
	disableUpsert, err := client.ReadDisableUpsert(ctx, mountPath)
	Expect(err).NotTo(HaveOccurred())
	Expect(disableUpsert).To(BeTrue())

	By("checking capabilities for the Transit key path")
	keyPath := path.Join(mountPath, "keys", keyName)
	capabilities, err := client.Capabilities(ctx, []string{keyPath})
	Expect(err).NotTo(HaveOccurred())
	Expect(capabilities.ByPath[keyPath]).NotTo(BeEmpty())

	By("encrypting and decrypting with required AAD")
	aad := []byte("ws03-e2e-aad")
	encrypted, err := client.Encrypt(ctx, openbao.EncryptRequest{
		MountPath:      mountPath,
		KeyName:        keyName,
		Plaintext:      []byte("ws03-e2e-plaintext"),
		AssociatedData: aad,
		KeyVersion:     profile.LatestVersion,
	})
	Expect(err).NotTo(HaveOccurred())
	Expect(encrypted.KeyVersion).To(Equal(profile.LatestVersion))

	decrypted, err := client.Decrypt(ctx, openbao.DecryptRequest{
		MountPath:      mountPath,
		KeyName:        keyName,
		Ciphertext:     encrypted.Ciphertext,
		AssociatedData: aad,
	})
	Expect(err).NotTo(HaveOccurred())
	Expect(string(decrypted.Plaintext)).To(Equal("ws03-e2e-plaintext"))

	By("rejecting decrypt with mismatched AAD")
	_, err = client.Decrypt(ctx, openbao.DecryptRequest{
		MountPath:      mountPath,
		KeyName:        keyName,
		Ciphertext:     encrypted.Ciphertext,
		AssociatedData: []byte("wrong-aad"),
	})
	var openBaoErr *openbao.Error
	Expect(errors.As(err, &openBaoErr)).To(BeTrue())
	Expect(openBaoErr.Class).To(Equal(openbao.ErrorClassDecryptFailed))

	By("batch decrypting while preserving per-item AAD")
	batch, err := client.BatchDecrypt(ctx, openbao.BatchDecryptRequest{
		MountPath: mountPath,
		KeyName:   keyName,
		Items: []openbao.BatchDecryptItem{{
			Ciphertext:     encrypted.Ciphertext,
			AssociatedData: aad,
			Reference:      "e2e-1",
		}},
	})
	Expect(err).NotTo(HaveOccurred())
	Expect(batch.Results).To(HaveLen(1))
	Expect(string(batch.Results[0].Plaintext)).To(Equal("ws03-e2e-plaintext"))

	By("running a non-secret encrypt/decrypt probe")
	err = client.ProbeEncryptDecrypt(ctx, openbao.ProbeRequest{
		MountPath:      mountPath,
		KeyName:        keyName,
		KeyVersion:     profile.LatestVersion,
		AssociatedData: []byte("ws03-probe-aad"),
	})
	Expect(err).NotTo(HaveOccurred())
}
