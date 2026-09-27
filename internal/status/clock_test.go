package status_test

import (
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/kmsv2"
)

func (c *fakeClock) JumpWall(delta time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(delta)
}

func TestStaleCacheRequiresNewProbeAfterClockCorrection(t *testing.T) {
	for _, forward := range []bool{false, true} {
		t.Run(map[bool]string{false: "backward", true: "forward_then_corrected"}[forward], func(t *testing.T) {
			clock := newFakeClock()
			observer := newTestObserver(t, clock, 1, 0)
			state := rebuildState(t, observer, profileForLatest(1, clock.Now()), clock.Now())
			store := newTestStore(t, clock)
			if err := store.PublishHealthy(state, clock.Read()); err != nil {
				t.Fatal(err)
			}
			if forward {
				clock.JumpWall(time.Hour)
				current, err := store.Current(t.Context())
				if err != nil || current.Healthz != kmsv2.HealthUnhealthy {
					t.Fatal("forward correction did not expire cache")
				}
				clock.JumpWall(-time.Hour)
			} else {
				clock.JumpWall(-time.Hour)
				clock.Advance(3 * time.Minute)
			}
			current, err := store.Current(t.Context())
			if err != nil || current.Healthz != kmsv2.HealthUnhealthy {
				t.Fatal("clock correction revived stale cache")
			}
			diagnostics := store.DiagnosticsSnapshot()
			if !diagnostics.Stale || diagnostics.CacheAge < 2*time.Minute {
				t.Fatal("diagnostics disagree with freshness gate")
			}
			if err := store.PublishHealthy(state, clock.Read()); err != nil {
				t.Fatal(err)
			}
			current, err = store.Current(t.Context())
			if err != nil || current.Healthz != kmsv2.HealthOK {
				t.Fatal("fresh evidence did not restore health")
			}
		})
	}
}
