package kmsv2_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/aad"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/kmsv2"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	kmsapi "k8s.io/kms/apis/v2"
)

func TestRequestFinalizationRecoversAndReleasesResources(t *testing.T) {
	for _, tc := range []struct {
		method string
		cache  bool
		invoke func(*testing.T, *kmsv2.Server, keyregistry.KeySnapshot) error
	}{
		{"status", true, invokePanickingStatus},
		{"encrypt", false, invokePanickingEncrypt},
		{"decrypt", false, invokePanickingDecrypt},
	} {
		t.Run(tc.method, func(t *testing.T) {
			observer := &fakeObserver{}
			var requestContext context.Context
			options := kmsv2.Options{Observer: observer, RequestTimeout: time.Minute}
			if tc.cache {
				options.StatusCache = panickingStatusCache{requestContext: &requestContext}
			} else {
				options.Transit = panickingTransit{requestContext: &requestContext}
			}
			server, _, _, active := newTestServerWithOptions(t, options)
			err := tc.invoke(t, server, active)
			if status := grpcstatus.Convert(err); status.Code() != codes.Internal || status.Message() != "kms request failed" {
				t.Fatalf("unexpected panic response: %v", status)
			}
			assertPanicObservation(t, observer, tc.method)
			if requestContext == nil || requestContext.Err() != context.Canceled {
				t.Fatal("panic did not cancel the request context")
			}
			status, encrypt, decrypt := server.InFlightKMSRequests()
			if status != 0 || encrypt != 0 || decrypt != 0 {
				t.Fatalf("panic leaked a limiter slot: %d, %d, %d", status, encrypt, decrypt)
			}
		})
	}
}

func invokePanickingStatus(t *testing.T, server *kmsv2.Server, _ keyregistry.KeySnapshot) error {
	t.Helper()
	response, err := server.Status(context.Background(), &kmsapi.StatusRequest{})
	if response != nil {
		t.Fatal("panic returned a Status response")
	}
	return err
}

func invokePanickingEncrypt(t *testing.T, server *kmsv2.Server, _ keyregistry.KeySnapshot) error {
	t.Helper()
	response, err := server.Encrypt(context.Background(), &kmsapi.EncryptRequest{Plaintext: []byte(testPlaintext)})
	if response != nil {
		t.Fatal("panic returned an Encrypt response")
	}
	return err
}

func invokePanickingDecrypt(t *testing.T, server *kmsv2.Server, active keyregistry.KeySnapshot) error {
	t.Helper()
	annotations, err := aad.BuildAnnotations(active, pluginVersion)
	if err != nil {
		t.Fatal(err)
	}
	response, err := server.Decrypt(context.Background(), &kmsapi.DecryptRequest{
		Ciphertext: []byte(testCiphertext), KeyId: active.KubernetesKeyID,
		Annotations: protoAnnotations(annotations),
	})
	if response != nil {
		t.Fatal("panic returned a Decrypt response")
	}
	return err
}

func assertPanicObservation(t *testing.T, observer *fakeObserver, method string) {
	t.Helper()
	if len(observer.requests) != 1 {
		t.Fatalf("got %d observations, want one", len(observer.requests))
	}
	observation := observer.requests[0]
	if observation.Method != method || observation.Status != "internal" || observation.ErrorClass != "panic" ||
		!observation.PanicRecovered || observation.PanicType != "string" {
		t.Fatalf("unexpected panic observation: %#v", observation)
	}
	if strings.Contains(fmt.Sprintf("%#v", observation), "sensitive") {
		t.Fatal("observation contains the panic value")
	}
}

func TestRequestFinalizationObservesUnhealthyStatusWithoutRPCFailure(t *testing.T) {
	observer := &fakeObserver{}
	server, cache, _, _ := newTestServerWithOptions(t, kmsv2.Options{Observer: observer})
	cache.SetError(fmt.Errorf("sensitive cache failure"))
	response, err := server.Status(context.Background(), &kmsapi.StatusRequest{})
	if err != nil || response.GetHealthz() != kmsv2.HealthUnhealthy || response.GetKeyId() != "" {
		t.Fatalf("unexpected unhealthy Status: %v, %v", response, err)
	}
	if len(observer.requests) != 1 {
		t.Fatalf("got %d observations, want one", len(observer.requests))
	}
	observation := observer.requests[0]
	if observation.Status != "ok" || observation.ErrorClass != "" || observation.Healthz != "unhealthy" ||
		observation.KeyIDHash != "" {
		t.Fatalf("unexpected unhealthy Status observation: %#v", observation)
	}
}

func TestRequestFinalizationPreservesUnrecognizedLookupClassification(t *testing.T) {
	observer := &fakeObserver{}
	lookup := failingSnapshotLookup{err: grpcstatus.Error(codes.PermissionDenied, "sensitive registry failure")}
	server, _, transit, active := newTestServerWithOptions(t, kmsv2.Options{Observer: observer, Registry: lookup})
	annotations, err := aad.BuildAnnotations(active, pluginVersion)
	if err != nil {
		t.Fatal(err)
	}
	response, err := server.Decrypt(context.Background(), &kmsapi.DecryptRequest{
		Ciphertext: []byte(testCiphertext), KeyId: active.KubernetesKeyID,
		Annotations: protoAnnotations(annotations),
	})
	status := grpcstatus.Convert(err)
	if response != nil || status.Code() != codes.Internal || status.Message() != "kms request failed" {
		t.Fatalf("unrecognized lookup failure was not redacted: %v, %v", response, status)
	}
	if len(observer.requests) != 1 || observer.requests[0].ErrorClass != "transit_policy_denied" {
		t.Fatalf("unrecognized lookup classification changed: %#v", observer.requests)
	}
	if transit.DecryptCalls() != 0 {
		t.Fatal("failed registry lookup reached Transit")
	}
}

type failingSnapshotLookup struct {
	err error
}

func (lookup failingSnapshotLookup) Lookup(string) (keyregistry.KeySnapshot, error) {
	return keyregistry.KeySnapshot{}, lookup.err
}

func TestRequestFinalizationObservesEmptyRequests(t *testing.T) {
	for _, tc := range []struct {
		method string
		invoke func(*kmsv2.Server) error
	}{
		{"encrypt", func(server *kmsv2.Server) error {
			_, err := server.Encrypt(context.Background(), nil)
			return err
		}},
		{"decrypt", func(server *kmsv2.Server) error {
			_, err := server.Decrypt(context.Background(), nil)
			return err
		}},
	} {
		t.Run(tc.method, func(t *testing.T) {
			observer := &fakeObserver{}
			server, _, transit, _ := newTestServerWithOptions(t, kmsv2.Options{Observer: observer})
			assertCode(t, tc.invoke(server), codes.InvalidArgument)
			if len(observer.requests) != 1 {
				t.Fatalf("got %d observations, want one", len(observer.requests))
			}
			observation := observer.requests[0]
			if observation.Method != tc.method || observation.ErrorClass != "unknown" ||
				observation.Status != "invalid_argument" {
				t.Fatalf("unexpected empty-request observation: %#v", observation)
			}
			if transit.EncryptCalls() != 0 || transit.DecryptCalls() != 0 {
				t.Fatal("empty request reached Transit")
			}
		})
	}
}

type panickingStatusCache struct {
	requestContext *context.Context
}

func (p panickingStatusCache) Current(ctx context.Context) (kmsv2.CachedStatus, error) {
	*p.requestContext = ctx
	panic("sensitive cached status")
}

type panickingTransit struct {
	requestContext *context.Context
}

func (p panickingTransit) Encrypt(
	ctx context.Context,
	_ kmsv2.TransitEncryptRequest,
) (kmsv2.TransitEncryptResponse, error) {
	*p.requestContext = ctx
	panic("sensitive encryption material")
}

func (p panickingTransit) Decrypt(
	ctx context.Context,
	_ kmsv2.TransitDecryptRequest,
) (kmsv2.TransitDecryptResponse, error) {
	*p.requestContext = ctx
	panic("sensitive decryption material")
}
