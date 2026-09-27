package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/kmsv2"
	"google.golang.org/grpc"
	kmsapi "k8s.io/kms/apis/v2"
)

func TestUnhealthyWaitObservesTransition(t *testing.T) {
	calls := 0
	client := fakeKMSClient{status: func(
		_ context.Context, _ *kmsapi.StatusRequest, _ ...grpc.CallOption,
	) (*kmsapi.StatusResponse, error) {
		calls++
		response := &kmsapi.StatusResponse{Version: kmsv2.APIVersion, Healthz: kmsv2.HealthUnhealthy}
		if calls == 1 {
			// An unhealthy response that retains a key ID is not the required contract.
			response.KeyId = "still-active"
		}
		return response, nil
	}}
	if err := waitForUnhealthyStatusWithin(context.Background(), client, time.Second); err != nil {
		t.Fatal(err)
	}
	if calls < 2 {
		t.Fatal("accepted unhealthy status with an active key ID")
	}
}

func TestUnhealthyWaitBoundsBlockedRPC(t *testing.T) {
	client := fakeKMSClient{status: func(
		ctx context.Context, _ *kmsapi.StatusRequest, _ ...grpc.CallOption,
	) (*kmsapi.StatusResponse, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	started := time.Now()
	err := waitForUnhealthyStatusWithin(context.Background(), client, 20*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline failure, got %v", err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("blocked Status RPC escaped the observation budget")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitForUnhealthyStatusWithin(ctx, client, time.Minute); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected caller cancellation, got %v", err)
	}
}

func TestDecryptSoakWorkerLetsInFlightRequestFinishAfterStop(t *testing.T) {
	stopCtx, stop := context.WithCancel(context.Background())
	requestParent := context.Background()
	started := make(chan struct{})
	release := make(chan struct{})
	var startedOnce sync.Once
	client := fakeKMSClient{
		decrypt: func(ctx context.Context, _ *kmsapi.DecryptRequest, _ ...grpc.CallOption) (*kmsapi.DecryptResponse, error) {
			startedOnce.Do(func() {
				close(started)
			})
			<-release
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return &kmsapi.DecryptResponse{Plaintext: []byte("plain")}, nil
		},
	}
	results := make(chan loadSoakSample, 1)
	done := make(chan struct{})

	go func() {
		defer close(done)
		runDecryptSoakWorker(
			stopCtx,
			requestParent,
			client,
			0,
			1,
			[]decryptSoakSample{{
				encrypted: encryptedSample{Ciphertext: []byte("ciphertext"), KeyID: "key-id"},
				plaintext: "plain",
			}},
			results,
		)
	}()

	<-started
	stop()
	close(release)
	<-done
	close(results)

	var samples []loadSoakSample
	for result := range results {
		samples = append(samples, result)
	}
	if len(samples) != 1 {
		t.Fatalf("expected one completed in-flight operation, got %d", len(samples))
	}
	if samples[0].err != nil {
		t.Fatalf("in-flight operation should not inherit stop context cancellation: %v", samples[0].err)
	}
}

type fakeKMSClient struct {
	status  func(context.Context, *kmsapi.StatusRequest, ...grpc.CallOption) (*kmsapi.StatusResponse, error)
	decrypt func(context.Context, *kmsapi.DecryptRequest, ...grpc.CallOption) (*kmsapi.DecryptResponse, error)
	encrypt func(context.Context, *kmsapi.EncryptRequest, ...grpc.CallOption) (*kmsapi.EncryptResponse, error)
}

func (f fakeKMSClient) Status(
	ctx context.Context,
	request *kmsapi.StatusRequest,
	opts ...grpc.CallOption,
) (*kmsapi.StatusResponse, error) {
	return f.status(ctx, request, opts...)
}

func (f fakeKMSClient) Decrypt(
	ctx context.Context,
	request *kmsapi.DecryptRequest,
	opts ...grpc.CallOption,
) (*kmsapi.DecryptResponse, error) {
	return f.decrypt(ctx, request, opts...)
}

func (f fakeKMSClient) Encrypt(
	ctx context.Context,
	request *kmsapi.EncryptRequest,
	opts ...grpc.CallOption,
) (*kmsapi.EncryptResponse, error) {
	return f.encrypt(ctx, request, opts...)
}
