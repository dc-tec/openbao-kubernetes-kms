package status_test

import (
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/kmsv2"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/status"
	"github.com/dc-tec/openbao-kubernetes-kms/test/fakes"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	kmsapi "k8s.io/kms/apis/v2"
)

func TestDeferredObservationPreservesConfirmedKeysAndFreezesProgress(t *testing.T) {
	for _, count := range []int{1, 2} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			f := newPendingPersistenceFixture(t, count)
			before, _ := f.store.State()
			unblock := blockPersistenceWrite(t, f.disk.Path)
			for range 4 {
				f.clock.Advance(time.Minute)
				assertDeferredObservation(t, f)
				assertPublishedHash(t, f.store, before.CurrentHash)
				if err := f.disk.Confirm(before); err != nil {
					t.Fatalf("deferred save changed durable state: %v", err)
				}
			}
			assertPendingCount(t, before, count)
			crypto := fakes.NewKMSTransit()
			local := rotationServer(t, f.store, f.controller, crypto)
			peerStore := newTestStore(t, f.clock)
			peerState := rebuildState(t, newTestObserver(t, f.clock, 1, 0), f.transit.profile, f.clock.Now())
			if err := peerStore.PublishHealthy(peerState, f.clock.Read()); err != nil {
				t.Fatal(err)
			}
			peer := rotationServer(t, peerStore, nil, crypto)
			assertCrossNodeDecrypt(t, local, peer, crypto)
			assertCrossNodeDecrypt(t, peer, local, crypto)
			sealed, err := local.Encrypt(t.Context(), &kmsapi.EncryptRequest{Plaintext: []byte("DEK")})
			if err != nil || sealed.GetKeyId() != before.ActiveKeyID {
				t.Fatalf("deferred save changed Encrypt key: %v", err)
			}
			unblock()
			if err := f.controller.ProbeOnce(t.Context()); err != nil {
				t.Fatal(err)
			}
			recovered, _ := f.store.State()
			if count == 1 {
				assertActiveVersion(t, recovered, 1)
				assertPendingCount(t, recovered, 3)
			} else {
				assertActiveVersion(t, recovered, 2)
				assertReadinessReasons(t, f.store, status.ReasonDeepProbePending)
			}
			if f.store.DiagnosticsSnapshot().PersistenceDegraded {
				t.Fatal("recovery retained the persistence warning")
			}
			if err := f.disk.Confirm(recovered); err != nil {
				t.Fatal(err)
			}
			// A failed stable-at write retains its original timestamp. The
			// recovery probe can promote only after the exact save succeeds.
			f.clock.Advance(time.Minute)
			if err := f.controller.ProbeOnce(t.Context()); err != nil {
				t.Fatal(err)
			}
			promoted, _ := f.store.State()
			assertActiveVersion(t, promoted, 2)
			assertReadinessReasons(t, f.store, status.ReasonDeepProbePending)
			if err := f.controller.DeepProbeOnce(t.Context()); err != nil {
				t.Fatal(err)
			}
			assertStoreHealth(t, f.store, kmsv2.HealthOK)
		})
	}
}

func TestObservationAvailabilityRejectsChangedPersistence(t *testing.T) {
	for _, damage := range []string{"partial write", "missing checkpoint", "checkpoint ahead", "other state"} {
		t.Run(damage, func(t *testing.T) {
			f := newPendingPersistenceFixture(t, 1)
			before, _ := f.store.State()
			checkpointPath := keyregistry.StateCheckpointPath(f.disk.Path)
			path := f.disk.Path
			if damage == "partial write" {
				path = checkpointPath
			}
			blockPersistenceWrite(t, path)
			if err := f.controller.ProbeOnce(t.Context()); err == nil {
				t.Fatal("blocked save succeeded")
			}
			if damage != "partial write" {
				assertStoreHealth(t, f.store, kmsv2.HealthOK)
				alterPersistenceEvidence(t, f, damage)
				if err := f.controller.ProbeOnce(t.Context()); err == nil {
					t.Fatal("changed evidence accepted")
				}
			}
			assertStoreHealth(t, f.store, kmsv2.HealthUnhealthy)
			assertPublishedHash(t, f.store, before.CurrentHash)
			stateBytes := readRecoveryEvidence(t, f.disk.Path)
			checkpointBytes := readRecoveryEvidence(t, checkpointPath)
			if err := f.disk.Confirm(before); err == nil {
				t.Fatal("changed persistence confirmed")
			}
			if readRecoveryEvidence(t, f.disk.Path) != stateBytes ||
				readRecoveryEvidence(t, checkpointPath) != checkpointBytes {
				t.Fatal("confirmation repaired persistence evidence")
			}
		})
	}
}

func alterPersistenceEvidence(t *testing.T, f persistenceFixture, damage string) {
	t.Helper()
	checkpointPath := keyregistry.StateCheckpointPath(f.disk.Path)
	if damage == "missing checkpoint" {
		if err := os.Remove(checkpointPath); err != nil {
			t.Fatal(err)
		}
		return
	}
	before, _ := f.store.State()
	next, err := newTestObserver(t, f.clock, 3, time.Minute).Observe(before, f.transit.profile, f.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	if damage == "checkpoint ahead" {
		err = writeRecoveryCheckpoint(checkpointPath, next.State)
	} else {
		// Copy another valid generation directly; the blocked temporary
		// path prevents the normal writer from replacing this evidence.
		data, encodeErr := json.Marshal(next.State)
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		err = os.WriteFile(f.disk.Path, data, 0o600)
	}
	if err != nil {
		t.Fatal(err)
	}
}

func TestDeferredObservationDoesNotMaskBackendOrDeepFailures(t *testing.T) {
	for _, failure := range []string{
		"new key", "rollback", "encryption minimum", "decrypt minimum", "unsafe profile", "metadata read", "deep probe",
	} {
		t.Run(failure, func(t *testing.T) {
			f := newPendingPersistenceFixture(t, 1)
			before, _ := f.store.State()
			blockPersistenceWrite(t, f.disk.Path)
			assertDeferredObservation(t, f)
			changeDeferredBackend(t, f, failure)
			if err := f.controller.ProbeOnce(t.Context()); err == nil {
				t.Fatal("backend failure hidden")
			}
			assertStoreHealth(t, f.store, kmsv2.HealthUnhealthy)
			assertPublishedHash(t, f.store, before.CurrentHash)
			server := rotationServer(t, f.store, f.controller, fakes.NewKMSTransit())
			_, err := server.Encrypt(t.Context(), &kmsapi.EncryptRequest{Plaintext: []byte("DEK")})
			if grpcstatus.Code(err) != codes.FailedPrecondition {
				t.Fatalf("Encrypt bypassed unhealthy state: %v", err)
			}
			if failure == "deep probe" {
				f.transit.deepProbeErr = nil
				if err := f.controller.DeepProbeOnce(t.Context()); err != nil {
					t.Fatal(err)
				}
				assertDeferredObservation(t, f)
			}
		})
	}
}

func changeDeferredBackend(t *testing.T, f persistenceFixture, failure string) {
	t.Helper()
	switch failure {
	case "new key":
		f.transit.profile = profileForLatest(3, f.clock.Now())
	case "rollback":
		f.transit.profile = profileForLatest(1, f.clock.Now())
	case "encryption minimum":
		f.transit.profile.MinEncryptionVersion = 2
	case "decrypt minimum":
		f.transit.profile.MinDecryptionVersion = 2
	case "unsafe profile":
		f.transit.profile.Exportable = true
	case "metadata read":
		f.transit.readErr = errors.New("backend unavailable")
	case "deep probe":
		f.transit.deepProbeErr = errors.New("data path unavailable")
		if err := f.controller.DeepProbeOnce(t.Context()); err == nil {
			t.Fatal("deep probe failure hidden")
		}
	}
}

func TestDeferredObservationStillExpiresWithoutMetadataProbes(t *testing.T) {
	f := newPendingPersistenceFixture(t, 1)
	blockPersistenceWrite(t, f.disk.Path)
	assertDeferredObservation(t, f)
	f.clock.Advance(3 * time.Minute)
	assertStoreHealth(t, f.store, kmsv2.HealthUnhealthy)
	assertReadinessReasons(t, f.store, status.ReasonStatusStale)
}

func TestPreRenamePromotionFailureCannotUseObservationException(t *testing.T) {
	f := newPendingPersistenceFixture(t, 3)
	f.clock.Advance(time.Minute)
	blockPersistenceWrite(t, f.disk.Path)
	if err := f.controller.ProbeOnce(t.Context()); err == nil {
		t.Fatal("blocked promotion succeeded")
	}
	assertStoreHealth(t, f.store, kmsv2.HealthUnhealthy)
	if f.store.DiagnosticsSnapshot().PersistenceDegraded {
		t.Fatal("promotion treated as an observation-only failure")
	}
}

func newPendingPersistenceFixture(t *testing.T, count int) persistenceFixture {
	t.Helper()
	f := newPersistenceFixture(t)
	f.transit.profile = profileForLatest(2, f.clock.Now())
	for range count {
		if err := f.controller.ProbeOnce(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func assertDeferredObservation(t *testing.T, f persistenceFixture) {
	t.Helper()
	if err := f.controller.ProbeOnce(t.Context()); !errors.Is(err, status.ErrProbeFailed) {
		t.Fatalf("deferred save did not report failure: %v", err)
	}
	assertStoreHealth(t, f.store, kmsv2.HealthOK)
	diagnostics := f.store.DiagnosticsSnapshot()
	if !diagnostics.PersistenceDegraded || len(diagnostics.Reasons) != 0 {
		t.Fatalf("incorrect deferred diagnostics: %+v", diagnostics)
	}
}
