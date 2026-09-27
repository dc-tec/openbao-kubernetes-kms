package status_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/kmsv2"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/openbao"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/status"
	"github.com/dc-tec/openbao-kubernetes-kms/test/fakes"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	kmsapi "k8s.io/kms/apis/v2"
)

func rotationServer(
	t *testing.T, store *status.Store, controller *status.Controller, transit kmsv2.Transit,
) *kmsv2.Server {
	t.Helper()
	server, err := kmsv2.NewServer(kmsv2.Options{
		StatusCache: store, Registry: store, KeyRefresher: controller, Transit: transit,
		PluginVersion: "test", RequestTimeout: time.Second,
		MaxConcurrentStatus: 16, MaxConcurrentEncrypt: 16, MaxConcurrentDecrypt: 64,
	})
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func TestNodesDecryptAcrossPromotionSkew(t *testing.T) {
	for _, test := range []struct {
		name  string
		count int
	}{{"unobserved", 0}, {"pending", 1}} {
		t.Run(test.name, func(t *testing.T) {
			clock := newFakeClock()
			ctx := context.Background()
			metadata := &fakeTransit{profile: profileForLatest(1, clock.Now())}
			crypto := fakes.NewKMSTransit()
			a, b := newTestStore(t, clock), newTestStore(t, clock)
			ca := newTestController(t, clock, a, newTestObserver(t, clock, 1, 0),
				metadata, &fakeStateStore{loadErr: keyregistry.ErrStateNotFound})
			cb := newTestController(t, clock, b, newTestObserver(t, clock, 3, time.Minute), metadata,
				&fakeStateStore{loadErr: keyregistry.ErrStateNotFound})
			for _, c := range []*status.Controller{ca, cb} {
				if err := c.ProbeOnce(ctx); err != nil {
					t.Fatal(err)
				}
				if err := c.DeepProbeOnce(ctx); err != nil {
					t.Fatal(err)
				}
			}
			sa, sb := rotationServer(t, a, ca, crypto), rotationServer(t, b, cb, crypto)
			metadata.profile = profileForLatest(2, clock.Now())
			if test.count == 1 {
				if err := cb.ProbeOnce(ctx); err != nil {
					t.Fatal(err)
				}
			}
			if err := ca.ProbeOnce(ctx); err != nil {
				t.Fatal(err)
			}
			if err := ca.DeepProbeOnce(ctx); err != nil {
				t.Fatal(err)
			}
			assertCrossNodeDecrypt(t, sa, sb, crypto)
			assertCrossNodeDecrypt(t, sb, sa, crypto)
			state, _ := b.State()
			assertActiveVersion(t, state, 1)
			assertPendingCount(t, state, test.count)
			assertStoreHealth(t, b, kmsv2.HealthOK)
			if metadata.readCalls != 4 {
				t.Fatalf("expected one discovery or background observation on B, reads=%d", metadata.readCalls)
			}
		})
	}
}

func assertCrossNodeDecrypt(t *testing.T, source, target *kmsv2.Server, crypto *fakes.KMSTransit) {
	t.Helper()
	ctx := context.Background()
	sealed, err := source.Encrypt(ctx, &kmsapi.EncryptRequest{Plaintext: []byte("shared DEK"), Uid: "rotation"})
	if err != nil {
		t.Fatal(err)
	}
	before := crypto.DecryptCalls()
	_, err = target.Decrypt(ctx, &kmsapi.DecryptRequest{Ciphertext: sealed.GetCiphertext(), KeyId: sealed.GetKeyId()})
	if grpcstatus.Code(err) != codes.InvalidArgument || crypto.DecryptCalls() != before {
		t.Fatalf("discovery bypassed annotation validation: %v", err)
	}
	opened, err := target.Decrypt(ctx, &kmsapi.DecryptRequest{
		Ciphertext: sealed.GetCiphertext(), KeyId: sealed.GetKeyId(), Annotations: sealed.GetAnnotations(),
	})
	if err != nil || string(opened.GetPlaintext()) != "shared DEK" {
		t.Fatalf("cross-node decrypt failed: %v", err)
	}
}

func TestControllerPersistsProgressWhileEncryptionMinimumBlocksActive(t *testing.T) {
	clock := newFakeClock()
	ctx := context.Background()
	store := newTestStore(t, clock)
	metadata := &fakeTransit{profile: profileForLatest(1, clock.Now())}
	saved := &fakeStateStore{loadErr: keyregistry.ErrStateNotFound}
	controller := newTestController(t, clock, store, newTestObserver(t, clock, 2, time.Minute), metadata, saved)
	if err := controller.ProbeOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if err := controller.DeepProbeOnce(ctx); err != nil {
		t.Fatal(err)
	}
	metadata.profile = profileForLatest(2, clock.Now())
	metadata.profile.MinEncryptionVersion = 2
	for count := 1; count <= 2; count++ {
		if err := controller.ProbeOnce(ctx); !errors.Is(err, status.ErrTransitKeyUnusable) {
			t.Fatalf("expected blocked encryption, got %v", err)
		}
		assertPendingCount(t, saved.saved, count)
		assertStoreHealth(t, store, kmsv2.HealthUnhealthy)
		if err := controller.DeepProbeOnce(ctx); !errors.Is(err, status.ErrTransitKeyUnusable) {
			t.Fatalf("deep probe did not skip blocked active version: %v", err)
		}
	}
	if metadata.deepProbeCalls != 1 {
		t.Fatal("blocked encryption version reached deep probe")
	}
	server := rotationServer(t, store, controller, fakes.NewKMSTransit())
	_, err := server.Encrypt(ctx, &kmsapi.EncryptRequest{Plaintext: []byte("DEK")})
	if grpcstatus.Code(err) != codes.FailedPrecondition {
		t.Fatalf("Encrypt accepted blocked active version: %v", err)
	}
	clock.Advance(time.Minute)
	if err := controller.ProbeOnce(ctx); err != nil {
		t.Fatal(err)
	}
	assertActiveVersion(t, saved.saved, 2)
	assertStoreHealth(t, store, kmsv2.HealthUnhealthy)
	if err := controller.DeepProbeOnce(ctx); err != nil {
		t.Fatal(err)
	}
	assertStoreHealth(t, store, kmsv2.HealthOK)
}

func TestDecryptDiscoveryCoalescesAndRateLimitsUnknownKeys(t *testing.T) {
	clock := newFakeClock()
	store := newTestStore(t, clock)
	observer := newTestObserver(t, clock, 1, 0)
	metadata := &fakeTransit{profile: profileForLatest(1, clock.Now())}
	controller := newTestController(t, clock, store, observer, metadata,
		&fakeStateStore{loadErr: keyregistry.ErrStateNotFound})
	ctx := context.Background()
	if err := controller.ProbeOnce(ctx); err != nil {
		t.Fatal(err)
	}
	metadata.profile = profileForLatest(2, clock.Now())
	v2 := rebuildState(t, observer, metadata.profile, clock.Now()).ActiveKeyID
	v3 := rebuildState(t, observer, profileForLatest(3, clock.Now()), clock.Now()).ActiveKeyID
	var group sync.WaitGroup
	for range 32 {
		group.Go(func() {
			if err := controller.RefreshForDecrypt(ctx, v2); err != nil {
				t.Errorf("refresh: %v", err)
			}
		})
	}
	group.Wait()
	if metadata.readCalls != 2 {
		t.Fatalf("concurrent refresh made %d metadata reads", metadata.readCalls)
	}
	state, _ := store.State()
	assertActiveVersion(t, state, 1)
	assertPendingCount(t, state, 0)
	for range 5 {
		if err := controller.RefreshForDecrypt(ctx, v3); err != nil {
			t.Fatal(err)
		}
	}
	if metadata.readCalls != 2 {
		t.Fatal("unknown keys bypassed refresh cooldown")
	}
	clock.Advance(30 * time.Second)
	metadata.profile = profileForLatest(3, clock.Now().Add(-30*time.Second))
	if err := controller.RefreshForDecrypt(ctx, v3); err != nil {
		t.Fatal(err)
	}
	state, _ = store.State()
	assertActiveVersion(t, state, 1)
	assertRetiredVersion(t, state, 2)
	assertPendingCount(t, state, 0)
	if _, err := store.Lookup(v2); err != nil {
		t.Fatalf("superseded pending key lost: %v", err)
	}
}

func TestDecryptDiscoveryRejectsChangedMetadataAndFailedPersistence(t *testing.T) {
	for _, failure := range []string{"timestamp", "missing", "save", "unavailable"} {
		t.Run(failure, func(t *testing.T) {
			clock := newFakeClock()
			store := newTestStore(t, clock)
			observer := newTestObserver(t, clock, 3, time.Minute)
			profile := profileForLatest(2, clock.Now())
			metadata := &fakeTransit{profile: profileForLatest(1, clock.Now())}
			saved := &fakeStateStore{loadErr: keyregistry.ErrStateNotFound}
			controller := newTestController(t, clock, store, observer, metadata, saved)
			if err := controller.ProbeOnce(context.Background()); err != nil {
				t.Fatal(err)
			}
			metadata.profile = profile
			keyID := rebuildState(t, observer, profile, clock.Now()).ActiveKeyID
			switch failure {
			case "timestamp":
				metadata.profile.VersionCreationTimes[0].CreatedAt = clock.Now()
			case "missing":
				metadata.profile.VersionCreationTimes = nil
			case "save":
				saved.saveErr = errors.New("disk unavailable")
			case "unavailable":
				metadata.readErr = errors.New("OpenBao unavailable")
			}
			if err := controller.RefreshForDecrypt(context.Background(), keyID); err == nil {
				t.Fatal("discovery accepted invalid metadata or failed persistence")
			}
			if _, err := store.Lookup(keyID); !errors.Is(err, keyregistry.ErrUnknownKeyID) {
				t.Fatalf("unvalidated key published: %v", err)
			}
		})
	}
}

func TestDecryptDiscoveryDoesNotResurrectRemovedKeys(t *testing.T) {
	clock := newFakeClock()
	observer := newTestObserver(t, clock, 1, 0)
	initial := rebuildState(t, observer, profileForLatest(1, clock.Now()), clock.Now())
	state := rebuildState(t, observer, profileForLatest(2, clock.Now()), clock.Now())
	removed, err := keyregistry.RetireVersions(state, 2)
	if err != nil {
		t.Fatal(err)
	}
	store := newTestStore(t, clock)
	metadata := &fakeTransit{profile: profileForLatest(2, clock.Now())}
	controller := newTestController(t, clock, store, observer, metadata, &fakeStateStore{state: removed})
	if err := controller.RefreshForDecrypt(context.Background(), initial.ActiveKeyID); err != nil {
		t.Fatal(err)
	}
	if metadata.readCalls != 0 || metadata.disableUpsertCalls != 0 {
		t.Fatal("removed key triggered metadata I/O")
	}
	if _, err := store.Lookup(initial.ActiveKeyID); !errors.Is(err, keyregistry.ErrUnknownKeyID) {
		t.Fatalf("discovery reintroduced removed identity: %v", err)
	}
}

func TestDiscoveryCancellationDoesNotCancelSubsequentRequests(t *testing.T) {
	clock := newFakeClock()
	store := newTestStore(t, clock)
	observer := newTestObserver(t, clock, 1, 0)
	metadata := &fakeTransit{profile: profileForLatest(1, clock.Now())}
	controller := newTestController(t, clock, store, observer, metadata,
		&fakeStateStore{loadErr: keyregistry.ErrStateNotFound})
	ctx := context.Background()
	if err := controller.ProbeOnce(ctx); err != nil {
		t.Fatal(err)
	}
	metadata.profile = profileForLatest(2, clock.Now())
	keyID := rebuildState(t, observer, metadata.profile, clock.Now()).ActiveKeyID
	metadata.readErr = context.Canceled
	if err := controller.RefreshForDecrypt(ctx, keyID); !errors.Is(err, context.Canceled) {
		t.Fatalf("discoverer lost cancellation: %v", err)
	}
	err := controller.RefreshForDecrypt(ctx, keyID)
	if !errors.Is(err, status.ErrProbeFailed) || errors.Is(err, context.Canceled) {
		t.Fatalf("independent caller inherited cancellation: %v", err)
	}
	if metadata.readCalls != 2 {
		t.Fatal("failed discovery bypassed cooldown")
	}
	metadata.readErr = nil
	clock.Advance(30 * time.Second)
	if err := controller.RefreshForDecrypt(ctx, keyID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Lookup(keyID); err != nil {
		t.Fatalf("discovery did not recover: %v", err)
	}
}

type gatedDeepTransit struct {
	*fakeTransit
	started chan struct{}
	release chan struct{}
}

func (f *gatedDeepTransit) ProbeEncryptDecrypt(
	ctx context.Context, req openbao.ProbeRequest,
) (openbao.ProbeResult, error) {
	close(f.started)
	select {
	case <-f.release:
		return openbao.ProbeResult{KeyVersion: req.KeyVersion}, nil
	case <-ctx.Done():
		return openbao.ProbeResult{}, ctx.Err()
	}
}

func TestProbeSerializationPreservesPromotionHealthAndRequestDeadline(t *testing.T) {
	clock := newFakeClock()
	store := newTestStore(t, clock)
	observer := newTestObserver(t, clock, 1, 0)
	initial := rebuildState(t, observer, profileForLatest(1, clock.Now()), clock.Now())
	if err := store.PublishHealthy(initial, clock.Read()); err != nil {
		t.Fatal(err)
	}
	metadata := &gatedDeepTransit{
		fakeTransit: &fakeTransit{profile: profileForLatest(2, clock.Now())},
		started:     make(chan struct{}), release: make(chan struct{}),
	}
	controller := newTestControllerWithOptions(t, status.ControllerOptions{
		Clock: clock, Store: store, Observer: observer, Transit: metadata, MountPath: "transit", KeyName: "key",
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	deepDone := make(chan error, 1)
	go func() { deepDone <- controller.DeepProbeOnce(ctx) }()
	select {
	case <-metadata.started:
	case <-ctx.Done():
		t.Fatal("deep probe did not start")
	}
	requestCtx, requestCancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer requestCancel()
	keyID := rebuildState(t, observer, metadata.profile, clock.Now()).ActiveKeyID
	if err := controller.RefreshForDecrypt(requestCtx, keyID); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("discovery did not respect deadline while waiting for probe: %v", err)
	}
	metadataDone := make(chan error, 1)
	go func() { metadataDone <- controller.ProbeOnce(ctx) }()
	close(metadata.release)
	if err := <-deepDone; err != nil {
		t.Fatal(err)
	}
	if err := <-metadataDone; err != nil {
		t.Fatal(err)
	}
	state, _ := store.State()
	assertActiveVersion(t, state, 2)
	assertStoreHealth(t, store, kmsv2.HealthUnhealthy)
}
