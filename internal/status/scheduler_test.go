package status_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/openbao"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/status"
)

func TestSchedulerDeepProbesNewActiveKeyWithoutWaitingForDeepInterval(t *testing.T) {
	clock := newFakeClock()
	store := newTestStore(t, clock)
	observer := newTestObserver(t, clock, 1, 0)
	transit := &fakeTransit{profile: profileForLatest(1, clock.Now())}
	stateStore := &fakeStateStore{loadErr: keyregistry.ErrStateNotFound}
	controller := newTestController(t, clock, store, observer, transit, stateStore)

	if err := controller.ProbeOnce(context.Background()); err != nil {
		t.Fatalf("initial metadata probe: %v", err)
	}
	if err := controller.DeepProbeOnce(context.Background()); err != nil {
		t.Fatalf("initial deep probe: %v", err)
	}

	transit.profile = profileForLatest(2, clock.Now())
	transit.deepProbeSignal = make(chan openbao.ProbeRequest, 1)
	scheduler, err := status.NewScheduler(status.SchedulerOptions{
		Controller:        controller,
		ProbeInterval:     time.Hour,
		DeepProbeInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("new scheduler: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- scheduler.Run(ctx)
	}()

	select {
	case request := <-transit.deepProbeSignal:
		if request.KeyVersion != 2 {
			t.Fatalf("unexpected deep-probe key version: %d", request.KeyVersion)
		}
	case <-time.After(time.Second):
		t.Fatal("scheduler did not deep probe the promoted key")
	}

	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected scheduler result: %v", err)
	}
}
