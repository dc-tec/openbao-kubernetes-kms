package kmsv2_test

import (
	"context"
	"testing"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/kmsv2"
	"google.golang.org/grpc/codes"
	kmsapi "k8s.io/kms/apis/v2"
)

func TestStatusAndEncryptShareCachedSnapshotPreconditions(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*kmsv2.CachedStatus)
		health string
	}{
		{"healthy", func(*kmsv2.CachedStatus) {}, kmsv2.HealthOK},
		{"empty health", func(c *kmsv2.CachedStatus) { c.Healthz = "" }, kmsv2.HealthUnhealthy},
		{"unhealthy", func(c *kmsv2.CachedStatus) { c.Healthz = kmsv2.HealthUnhealthy }, kmsv2.HealthUnhealthy},
		{"custom unhealthy", func(c *kmsv2.CachedStatus) { c.Healthz = "stale" }, "stale"},
		{"missing snapshot", func(c *kmsv2.CachedStatus) { c.Active = keyregistry.KeySnapshot{} }, kmsv2.HealthUnhealthy},
		{
			"pending snapshot",
			func(c *kmsv2.CachedStatus) { c.Active.State = keyregistry.StatePending },
			kmsv2.HealthUnhealthy,
		},
		{"missing key ID", func(c *kmsv2.CachedStatus) { c.KeyID = "" }, kmsv2.HealthUnhealthy},
		{"key ID mismatch", func(c *kmsv2.CachedStatus) { c.KeyID = "foreign-key-id" }, kmsv2.HealthUnhealthy},
		{"derived snapshot ID", func(c *kmsv2.CachedStatus) { c.Active.KubernetesKeyID = "" }, kmsv2.HealthOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, cache, transit, active := newTestServer(t)
			cached := kmsv2.CachedStatus{Healthz: kmsv2.HealthOK, KeyID: active.KubernetesKeyID, Active: active}
			tc.mutate(&cached)
			cache.Set(cached)
			response, err := server.Status(context.Background(), &kmsapi.StatusRequest{})
			if err != nil || response.GetHealthz() != tc.health || response.GetVersion() != kmsv2.APIVersion {
				t.Fatalf("unexpected Status: %v, %v", response, err)
			}
			if tc.health == kmsv2.HealthOK {
				assertHealthyCachedEncrypt(t, server, response.GetKeyId(), cached.KeyID)
			} else {
				if response.GetKeyId() != "" {
					t.Fatal("unhealthy Status exposes a key ID")
				}
				_, err = server.Encrypt(context.Background(), &kmsapi.EncryptRequest{Plaintext: []byte(testPlaintext)})
				assertCode(t, err, codes.FailedPrecondition)
				if transit.EncryptCalls() != 0 {
					t.Fatal("unusable cached snapshot reached Transit")
				}
			}
			if cache.Calls() != 2 {
				t.Fatalf("Status and Encrypt read the cache %d times, want two", cache.Calls())
			}
		})
	}
}

func assertHealthyCachedEncrypt(t *testing.T, server *kmsv2.Server, statusKeyID, cachedKeyID string) {
	t.Helper()
	response, err := server.Encrypt(context.Background(), &kmsapi.EncryptRequest{Plaintext: []byte(testPlaintext)})
	if err != nil || response.GetKeyId() != statusKeyID || statusKeyID != cachedKeyID || statusKeyID == "" {
		t.Fatalf("healthy Status and Encrypt disagree: %q, %v, %v", statusKeyID, response, err)
	}
}

func TestDecryptRejectsInvalidAnnotationEncodingBeforeLookupAndTransit(t *testing.T) {
	for _, annotations := range []map[string][]byte{
		{string([]byte{0xff}): []byte("value")},
		{"key": {0xff}},
	} {
		observer := &fakeObserver{}
		lookup := &countingSnapshotLookup{}
		server, _, transit, active := newTestServerWithOptions(t, kmsv2.Options{Observer: observer, Registry: lookup})
		response, err := server.Decrypt(context.Background(), &kmsapi.DecryptRequest{
			Ciphertext: []byte(testCiphertext), KeyId: active.KubernetesKeyID, Annotations: annotations,
		})
		assertCode(t, err, codes.InvalidArgument)
		if response != nil || lookup.calls != 0 || transit.DecryptCalls() != 0 {
			t.Fatal("invalid annotation encoding passed the request preflight")
		}
		if len(observer.requests) != 1 || observer.requests[0].ErrorClass != "annotation_invalid" {
			t.Fatalf("unexpected annotation observation: %#v", observer.requests)
		}
		if len(observer.aadReasons) != 0 || len(observer.keyIDReasons) != 0 {
			t.Fatal("request-limit validation changed the AAD or key-ID validation counters")
		}
	}
}

type countingSnapshotLookup struct {
	calls int
}

func (l *countingSnapshotLookup) Lookup(string) (keyregistry.KeySnapshot, error) {
	l.calls++
	return keyregistry.KeySnapshot{}, keyregistry.ErrUnknownKeyID
}
