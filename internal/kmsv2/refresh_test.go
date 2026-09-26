package kmsv2_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/kmsv2"
	"google.golang.org/grpc/codes"
	kmsapi "k8s.io/kms/apis/v2"
)

type refreshFunc func(context.Context, string) error

func (f refreshFunc) RefreshForDecrypt(ctx context.Context, keyID string) error {
	return f(ctx, keyID)
}

func TestDecryptRefreshFailureCodesAndRedaction(t *testing.T) {
	for _, test := range []struct {
		name  string
		err   error
		code  codes.Code
		class string
	}{
		{"unavailable", errors.New("sensitive backend detail"), codes.Unavailable, "key_metadata_refresh_failed"},
		{"timeout", context.DeadlineExceeded, codes.DeadlineExceeded, "timeout"},
		{"canceled", context.Canceled, codes.Canceled, "canceled"},
		{"still unknown", nil, codes.NotFound, "key_id_unknown"},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			observer := &fakeObserver{}
			server, _, transit, active := newTestServerWithOptions(t, kmsv2.Options{
				Observer:     observer,
				KeyRefresher: refreshFunc(func(context.Context, string) error { calls++; return test.err }),
			})
			active.TransitVersion++
			active.KubernetesKeyID = ""
			unknown, err := keyregistry.DeriveKeyID(active)
			if err != nil {
				t.Fatal(err)
			}
			_, err = server.Decrypt(context.Background(), &kmsapi.DecryptRequest{
				KeyId: unknown, Ciphertext: []byte("ciphertext"),
			})
			assertCode(t, err, test.code)
			if calls != 1 || transit.DecryptCalls() != 0 || strings.Contains(err.Error(), "sensitive") {
				t.Fatalf("refresh failed its validation or redaction boundary: calls=%d, %v", calls, err)
			}
			if observer.requests[0].ErrorClass != test.class {
				t.Fatalf("wrong error class: %+v", observer.requests[0])
			}
		})
	}
}

func TestDecryptRefreshUsesRequestTimeoutAndSkipsKnownOrMalformedIDs(t *testing.T) {
	calls := 0
	server, _, _, active := newTestServerWithOptions(t, kmsv2.Options{
		RequestTimeout: 10 * time.Millisecond,
		KeyRefresher:   refreshFunc(func(ctx context.Context, _ string) error { calls++; <-ctx.Done(); return ctx.Err() }),
	})
	sealed, err := server.Encrypt(context.Background(), &kmsapi.EncryptRequest{Plaintext: []byte("DEK")})
	if err != nil {
		t.Fatal(err)
	}
	_, err = server.Decrypt(context.Background(), &kmsapi.DecryptRequest{
		KeyId: sealed.GetKeyId(), Ciphertext: sealed.GetCiphertext(), Annotations: sealed.GetAnnotations(),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = server.Decrypt(context.Background(), &kmsapi.DecryptRequest{KeyId: "bad", Ciphertext: []byte("ciphertext")})
	assertCode(t, err, codes.InvalidArgument)
	if calls != 0 {
		t.Fatal("known or malformed key triggered metadata I/O")
	}
	active.TransitVersion++
	active.KubernetesKeyID = ""
	unknown, err := keyregistry.DeriveKeyID(active)
	if err != nil {
		t.Fatal(err)
	}
	_, err = server.Decrypt(context.Background(), &kmsapi.DecryptRequest{
		KeyId: unknown, Ciphertext: []byte("ciphertext"),
	})
	assertCode(t, err, codes.DeadlineExceeded)
	if calls != 1 {
		t.Fatalf("refresh calls=%d", calls)
	}
}
