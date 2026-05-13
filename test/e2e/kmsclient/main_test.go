package main

import (
	"context"
	"sync"
	"testing"

	"google.golang.org/grpc"
	kmsapi "k8s.io/kms/apis/v2"
)

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
