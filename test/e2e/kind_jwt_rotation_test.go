//go:build e2e

package e2e

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/test/e2e/framework"
)

// verifyKindJWTRotation runs in the smoke lane against the generated static pod.
func verifyKindJWTRotation(t *testing.T, ctx context.Context, docker, node string, bao *framework.OpenBaoEnvironment) {
	t.Helper()
	providerID := kindProviderContainerID(t, ctx, docker, node)
	dir := t.TempDir()
	client := filepath.Join(dir, "kms-client")
	buildKMSClient(t, ctx, client)
	dockerCopy(t, ctx, docker, client, node+":"+kindKMSClientPath)
	runDocker(t, ctx, docker, "exec", node, "mkdir", "-p", "/kms-sample")
	runDocker(t, ctx, docker, "exec", node, "chmod", "0755", kindKMSClientPath)
	runKindKMSClient(t, ctx, docker, node, kmsClientModeWriteSample)

	t.Log("retire the old JWT signing key and revoke provider tokens")
	if err := bao.RotateJWTSigningKey(ctx, false); err != nil {
		t.Fatal(err)
	}
	if err := bao.RevokeProviderTokens(ctx); err != nil {
		t.Fatal(err)
	}
	runKindKMSClient(t, ctx, docker, node, kmsClientModeExpectAuthFailure)

	t.Log("atomically replace the JWT and recover without restarting the provider")
	replacement := filepath.Join(dir, "identity.jwt")
	if err := bao.WriteJWTFileAt(replacement, time.Now(), time.Hour); err != nil {
		t.Fatal(err)
	}
	dockerCopy(t, ctx, docker, replacement, node+":"+kindProviderJWTPath+".new")
	runDocker(t, ctx, docker, "exec", node, "chown", "65532:65532", kindProviderJWTPath+".new")
	runDocker(t, ctx, docker, "exec", node, "chmod", "0600", kindProviderJWTPath+".new")
	runDocker(t, ctx, docker, "exec", node, "mv", kindProviderJWTPath+".new", kindProviderJWTPath)
	runKindKMSClient(t, ctx, docker, node, kmsClientModeReadSample)
	runKindKMSClient(t, ctx, docker, node, kmsClientModeFullStack)
	if kindProviderContainerID(t, ctx, docker, node) != providerID {
		t.Fatal("JWT replacement restarted the provider")
	}
	for _, path := range []string{bao.JWTFile, replacement} {
		credential, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		logs := kindContainerLogs(ctx, docker, node, "^bao-kms-provider$")
		if strings.Contains(logs, strings.TrimSpace(string(credential))) {
			t.Fatal("provider logs exposed a JWT")
		}
	}
}
