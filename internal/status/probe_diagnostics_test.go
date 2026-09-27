package status_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/aad"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/openbao"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/status"
)

func TestProbeDiagnosticsReportFailureAndClearAfterRecovery(t *testing.T) {
	for _, tc := range []struct {
		name   string
		fail   func(*fakeTransit, *fakeStateStore)
		reason status.HealthReason
		class  string
		deep   bool
	}{
		{"TLS upsert read", func(tr *fakeTransit, _ *fakeStateStore) {
			tr.disableUpsertErr = &openbao.Error{Class: openbao.ErrorClassTLSFailed, Operation: "sensitive-host"}
		}, status.ReasonUpsertCheckFailed, "tls_failed", false},
		{"DNS metadata read", func(tr *fakeTransit, _ *fakeStateStore) {
			tr.readErr = &openbao.Error{Class: openbao.ErrorClassDNSFailed}
		}, status.ReasonMetadataReadFailed, "dns_failed", false},
		{"rejected login", func(tr *fakeTransit, _ *fakeStateStore) {
			tr.readErr = errors.Join(openbao.ErrAuthentication, &openbao.Error{Class: openbao.ErrorClassPermissionDenied})
		}, status.ReasonMetadataReadFailed, "auth_failed", false},
		{"auth connection", func(tr *fakeTransit, _ *fakeStateStore) {
			tr.readErr = errors.Join(openbao.ErrAuthentication, &openbao.Error{Class: openbao.ErrorClassConnectionFailed})
		}, status.ReasonMetadataReadFailed, "connection_failed", false},
		{"unsafe upsert", func(tr *fakeTransit, _ *fakeStateStore) {
			tr.implicitKeyCreationEnabled = true
		}, status.ReasonUpsertAllowed, "upsert_allowed", false},
		{"invalid profile", func(tr *fakeTransit, _ *fakeStateStore) {
			tr.profile.DeletionAllowed = true
		}, status.ReasonProfileInvalid, "profile_invalid", false},
		{"save failure", func(tr *fakeTransit, state *fakeStateStore) {
			tr.profile.LatestVersion = 2
			state.saveErr = errors.New("sensitive state path")
		}, status.ReasonStateSaveFailed, "state_save_failed", false},
		{"deep policy failure", func(tr *fakeTransit, _ *fakeStateStore) {
			tr.deepProbeErr = &openbao.Error{Class: openbao.ErrorClassPermissionDenied}
		}, status.ReasonDeepProbeFailed, "permission_denied", true},
		{"invalid deep result", func(tr *fakeTransit, _ *fakeStateStore) {
			tr.deepProbeResult.KeyVersion = 99
		}, status.ReasonDeepProbeInvalid, "deep_probe_invalid", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newDiagnosticFixture(t, 1, 0)
			fixture.probeHealthy(t)
			tc.fail(fixture.transit, fixture.state)
			if tc.name == "save failure" {
				fixture.transit.profile = profileForLatest(2, fixture.clock.Now())
			}
			probe := fixture.controller.ProbeOnce
			kind := status.ProbeKindMetadata
			if tc.deep {
				probe = fixture.controller.DeepProbeOnce
				kind = status.ProbeKindDeep
			}
			if err := probe(context.Background()); err == nil {
				t.Fatal("probe succeeded with injected failure")
			}
			diagnostics := fixture.store.DiagnosticsSnapshot()
			if !slices.Contains(diagnostics.Reasons, tc.reason) {
				t.Fatalf("reasons = %v", diagnostics.Reasons)
			}
			class := diagnostics.MetadataErrorClass
			if tc.deep {
				class = diagnostics.DeepErrorClass
			}
			if class != tc.class {
				t.Fatalf("class = %q, want %q", class, tc.class)
			}
			last := fixture.observations.probes[len(fixture.observations.probes)-1]
			if last.Reason != tc.reason || last.ErrorClass != tc.class || last.Kind != kind {
				t.Fatalf("observation = %+v", last)
			}
			*fixture.transit = fakeTransit{profile: profileForLatest(1, fixture.clock.Now())}
			fixture.state.saveErr = nil
			fixture.probeHealthy(t)
			diagnostics = fixture.store.DiagnosticsSnapshot()
			if len(diagnostics.Reasons) != 0 || diagnostics.MetadataErrorClass != "" || diagnostics.DeepErrorClass != "" {
				t.Fatalf("recovered diagnostics = %+v", diagnostics)
			}
		})
	}
}

func TestReadinessReasonsTrackBootstrapStalenessAndPromotion(t *testing.T) {
	f := newDiagnosticFixture(t, 1, 0)
	if !slices.Contains(f.store.DiagnosticsSnapshot().Reasons, status.ReasonStateUnavailable) {
		t.Fatal("missing-state reason absent")
	}
	if err := f.controller.ProbeOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertReadinessReasons(t, f.store, status.ReasonDeepProbePending)
	f.probeHealthy(t)
	profileBase := f.clock.Now()
	f.clock.Advance(3 * time.Minute)
	assertReadinessReasons(t, f.store, status.ReasonStatusStale)
	f.transit.profile = profileForLatest(2, profileBase)
	if err := f.controller.ProbeOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertReadinessReasons(t, f.store, status.ReasonDeepProbePending)
	f.probeHealthy(t)
}

func TestPromotionEventRequiresPersistedPublication(t *testing.T) {
	f := newDiagnosticFixture(t, 2, time.Minute)
	f.probeHealthy(t)
	previous, _ := f.store.Active()
	if len(f.observations.promotions) != 0 {
		t.Fatal("bootstrap emitted promotion")
	}
	f.transit.profile = profileForLatest(2, f.clock.Now())
	if err := f.controller.ProbeOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.observations.promotions) != 0 {
		t.Fatal("pending rotation emitted promotion")
	}
	if err := f.controller.ProbeOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.clock.Advance(time.Minute)
	f.state.saveErr = errors.New("sensitive path")
	if err := f.controller.ProbeOnce(context.Background()); err == nil {
		t.Fatal("save failure was ignored")
	}
	if len(f.observations.promotions) != 0 {
		t.Fatal("failed save emitted promotion")
	}
	f.state.saveErr = nil
	if err := f.controller.ProbeOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	active, _ := f.store.Active()
	want := status.PromotionObservation{
		PreviousKeyIDHash: aad.HashValue(previous.KubernetesKeyID), KeyIDHash: aad.HashValue(active.KubernetesKeyID),
		PreviousTransitVersion: 1, TransitVersion: 2,
	}
	if !reflect.DeepEqual(f.observations.promotions, []status.PromotionObservation{want}) {
		t.Fatalf("promotions = %+v", f.observations.promotions)
	}
	if f.observations.publishedStateID != active.KubernetesKeyID || f.state.saved.ActiveKeyID != active.KubernetesKeyID {
		t.Fatal("promotion emitted before state publication and persistence")
	}
	f.probeHealthy(t)
	if len(f.observations.promotions) != 1 {
		t.Fatal("unchanged key emitted duplicate promotion")
	}
}

type diagnosticFixture struct {
	clock        *fakeClock
	store        *status.Store
	transit      *fakeTransit
	state        *fakeStateStore
	controller   *status.Controller
	observations *probeRecorder
}

func newDiagnosticFixture(t *testing.T, count int, delay time.Duration) diagnosticFixture {
	t.Helper()
	clock := newFakeClock()
	store := newTestStore(t, clock)
	tr := &fakeTransit{profile: profileForLatest(1, clock.Now())}
	state := &fakeStateStore{loadErr: keyregistry.ErrStateNotFound}
	recorder := &probeRecorder{store: store}
	controller := newTestControllerWithOptions(t, status.ControllerOptions{
		Clock: clock, Store: store, Observer: newTestObserver(t, clock, count, delay),
		Transit: tr, StateStore: state, MountPath: "transit", KeyName: "sensitive-key", ProbeObserver: recorder,
	})
	return diagnosticFixture{clock, store, tr, state, controller, recorder}
}

func (f diagnosticFixture) probeHealthy(t *testing.T) {
	t.Helper()
	if err := f.controller.ProbeOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := f.controller.DeepProbeOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
}

type probeRecorder struct {
	store            *status.Store
	probes           []status.ProbeObservation
	promotions       []status.PromotionObservation
	publishedStateID string
}

func (r *probeRecorder) ObserveStatusProbe(_ context.Context, obs status.ProbeObservation) {
	r.probes = append(r.probes, obs)
}

func (r *probeRecorder) ObserveKeyPromotion(_ context.Context, obs status.PromotionObservation) {
	r.promotions = append(r.promotions, obs)
	active, _ := r.store.Active()
	r.publishedStateID = active.KubernetesKeyID
}

func assertReadinessReasons(t *testing.T, store *status.Store, want ...status.HealthReason) {
	t.Helper()
	if got := store.DiagnosticsSnapshot().Reasons; !slices.Equal(got, want) {
		t.Fatalf("reasons = %v, want %v", got, want)
	}
}
