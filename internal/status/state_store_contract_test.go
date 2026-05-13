package status_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/kmsv2"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/openbao"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/status"
)

func TestFileStateStoreReplayCheckpointContract(t *testing.T) {
	clock := newFakeClock()
	initial, promoted := replayContractStates(t, clock)

	tests := []struct {
		name      string
		mutate    func(t testing.TB, path string)
		wantError error
	}{
		{
			name: "older state with surviving checkpoint",
			mutate: func(t testing.TB, path string) {
				t.Helper()
				if err := keyregistry.SaveStateFile(path, initial); err != nil {
					t.Fatalf("replay older state: %v", err)
				}
			},
			wantError: keyregistry.ErrStateRollback,
		},
		{
			name: "truncated state with surviving checkpoint",
			mutate: func(t testing.TB, path string) {
				t.Helper()
				if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
					t.Fatalf("truncate state file: %v", err)
				}
			},
			wantError: keyregistry.ErrStateCorrupt,
		},
		{
			name: "missing state with surviving checkpoint",
			mutate: func(t testing.TB, path string) {
				t.Helper()
				if err := os.Remove(path); err != nil {
					t.Fatalf("remove state file: %v", err)
				}
			},
			wantError: keyregistry.ErrStateRollback,
		},
		{
			name: "same generation body mismatch with surviving checkpoint",
			mutate: func(t testing.TB, path string) {
				t.Helper()
				alternate := sameGenerationAlternateState(t, initial, promoted)
				if err := keyregistry.SaveStateFile(path, alternate); err != nil {
					t.Fatalf("save same-generation alternate state: %v", err)
				}
			},
			wantError: keyregistry.ErrStateRollback,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "key-registry.json")
			store := status.FileStateStore{Path: path}
			if err := store.Save(promoted); err != nil {
				t.Fatalf("save promoted state: %v", err)
			}
			if _, err := store.Load(); err != nil {
				t.Fatalf("reload promoted state before replay: %v", err)
			}

			tt.mutate(t, path)

			_, err := store.Load()
			if !errors.Is(err, tt.wantError) {
				t.Fatalf("expected %v, got %v", tt.wantError, err)
			}
		})
	}
}

func TestFileStateStoreRecreatesMissingCheckpointForValidState(t *testing.T) {
	clock := newFakeClock()
	_, promoted := replayContractStates(t, clock)
	path := filepath.Join(t.TempDir(), "key-registry.json")
	store := status.FileStateStore{Path: path}

	if err := keyregistry.SaveStateFile(path, promoted); err != nil {
		t.Fatalf("save unanchored promoted state: %v", err)
	}
	if _, err := os.Stat(keyregistry.StateCheckpointPath(path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected checkpoint to be absent before load, got %v", err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("load valid unanchored state: %v", err)
	}
	if loaded.CurrentHash != promoted.CurrentHash {
		t.Fatalf("loaded unexpected state hash: want %s got %s", promoted.CurrentHash, loaded.CurrentHash)
	}
	checkpoint, err := keyregistry.LoadStateCheckpoint(keyregistry.StateCheckpointPath(path))
	if err != nil {
		t.Fatalf("load recreated checkpoint: %v", err)
	}
	if checkpoint.Generation != promoted.Generation || checkpoint.CurrentHash != promoted.CurrentHash {
		t.Fatalf("checkpoint does not match promoted state: %#v", checkpoint)
	}
}

func TestControllerAutoBootstrapBoundaryWithMissingStateFiles(t *testing.T) {
	tests := []struct {
		name              string
		profile           func(*fakeClock) openbao.KeyProfile
		wantErr           error
		wantReason        string
		wantSaveCalls     int
		wantHealthyStatus bool
	}{
		{
			name: "initial Transit metadata allowed",
			profile: func(clock *fakeClock) openbao.KeyProfile {
				return profileForLatest(1, clock.Now())
			},
			wantSaveCalls:     1,
			wantHealthyStatus: true,
		},
		{
			name: "rotated Transit metadata denied",
			profile: func(clock *fakeClock) openbao.KeyProfile {
				return profileForLatest(2, clock.Now())
			},
			wantErr:       status.ErrStateUnavailable,
			wantReason:    "latest_version=2",
			wantSaveCalls: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock := newFakeClock()
			store := newTestStore(t, clock)
			observer := newTestObserver(t, clock, 3, 2*time.Minute)
			auth := &fakeAuth{}
			transit := &fakeTransit{profile: tt.profile(clock)}
			stateStore := &fakeStateStore{loadErr: keyregistry.ErrStateNotFound}
			controller := newTestController(t, clock, store, observer, auth, transit, stateStore)

			err := controller.ProbeOnce(context.Background())
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("unexpected probe error: want %v got %v", tt.wantErr, err)
			}
			if tt.wantReason != "" && !strings.Contains(err.Error(), tt.wantReason) {
				t.Fatalf("expected error reason %q, got %v", tt.wantReason, err)
			}
			if stateStore.saveCalls != tt.wantSaveCalls {
				t.Fatalf("unexpected save calls: want %d got %d", tt.wantSaveCalls, stateStore.saveCalls)
			}
			current, currentErr := store.Current(context.Background())
			if currentErr != nil {
				t.Fatalf("current status: %v", currentErr)
			}
			if tt.wantHealthyStatus {
				if current.Healthz != kmsv2.HealthOK || current.KeyID == "" {
					t.Fatalf("expected healthy bootstrapped status, got %+v", current)
				}
				return
			}
			if current.Healthz != kmsv2.HealthUnhealthy || current.KeyID != "" {
				t.Fatalf("expected unhealthy status without key_id, got %+v", current)
			}
		})
	}
}

func replayContractStates(t *testing.T, clock *fakeClock) (keyregistry.StateFile, keyregistry.StateFile) {
	t.Helper()

	observer := newTestObserver(t, clock, 1, 0)
	initial := rebuildState(t, observer, profileForLatest(1, clock.Now()), clock.Now())
	promoted, err := observer.Observe(initial, profileForLatest(2, clock.Now()), clock.Now())
	if err != nil {
		t.Fatalf("promote v2: %v", err)
	}
	return initial, promoted.State
}

func sameGenerationAlternateState(
	t testing.TB,
	initial keyregistry.StateFile,
	promoted keyregistry.StateFile,
) keyregistry.StateFile {
	t.Helper()

	active, err := promoted.ActiveSnapshot()
	if err != nil {
		t.Fatalf("promoted active snapshot: %v", err)
	}
	active.TransitVersion++
	active.TransitVersionCreatedAt = active.TransitVersionCreatedAt.Add(time.Hour)
	active.KubernetesKeyID = ""
	active.State = keyregistry.StateActive

	alternate, err := keyregistry.NewStateFile(active, nil, promoted.Generation, initial.CurrentHash)
	if err != nil {
		t.Fatalf("new same-generation alternate state: %v", err)
	}
	return alternate
}
