package kmsv2_test

import (
	"context"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/aad"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"
	"google.golang.org/grpc/codes"
	kmsapi "k8s.io/kms/apis/v2"
)

func TestDecryptPreflightContractRejectsBeforeTransit(t *testing.T) {
	server, _, transit, active := newTestServer(t)
	annotations, err := aad.BuildAnnotations(active, pluginVersion)
	if err != nil {
		t.Fatalf("build annotations: %v", err)
	}
	unknown := active
	unknown.TransitVersion++
	unknown.TransitVersionCreatedAt = active.TransitVersionCreatedAt.Add(time.Hour)
	unknown.KubernetesKeyID = ""
	unknownKeyID, err := keyregistry.DeriveKeyID(unknown)
	if err != nil {
		t.Fatalf("derive unknown key_id: %v", err)
	}

	tests := []struct {
		name        string
		keyID       string
		annotations map[string][]byte
		code        codes.Code
	}{
		{
			name:        "malformed key_id",
			keyID:       malformedKeyID,
			annotations: nil,
			code:        codes.InvalidArgument,
		},
		{
			name:        "unknown well-formed key_id",
			keyID:       unknownKeyID,
			annotations: nil,
			code:        codes.NotFound,
		},
		{
			name:        "annotation mismatch",
			keyID:       active.KubernetesKeyID,
			annotations: mismatchedProtoAnnotations(annotations, aad.KeyTransitMountHash),
			code:        codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := transit.DecryptCalls()
			_, err := server.Decrypt(context.Background(), &kmsapi.DecryptRequest{
				Ciphertext:  []byte(testCiphertext),
				KeyId:       tt.keyID,
				Annotations: tt.annotations,
			})
			assertCode(t, err, tt.code)
			if transit.DecryptCalls() != before {
				t.Fatalf("%s reached Transit decrypt", tt.name)
			}
		})
	}
}

func mismatchedProtoAnnotations(source map[string]string, key string) map[string][]byte {
	mutated := cloneStringAnnotations(source)
	mutated[key] = aad.HashValue("different-" + key)
	return protoAnnotations(mutated)
}

func cloneStringAnnotations(source map[string]string) map[string]string {
	cloned := make(map[string]string, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}
