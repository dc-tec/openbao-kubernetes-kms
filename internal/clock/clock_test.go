package clock_test

import (
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/clock"
)

func TestDeadlineIgnoresWallClockCorrections(t *testing.T) {
	start := clock.Reading{Wall: time.Unix(1700000000, 0)}
	deadline := clock.After(start, time.Minute)
	for _, jump := range []time.Duration{-time.Hour, time.Hour} {
		now := clock.Reading{Wall: start.Wall.Add(jump), Elapsed: time.Minute - time.Nanosecond}
		if !deadline.Pending(now) {
			t.Fatal("wall correction shortened elapsed deadline")
		}
		if !deadline.WallTime(now).Equal(now.Wall.Add(time.Nanosecond)) {
			t.Fatal("diagnostic deadline is not a projection")
		}
		now.Elapsed++
		if deadline.Pending(now) {
			t.Fatal("deadline did not finish at its elapsed boundary")
		}
	}
}

func TestLifetimeRetainsGreatestObservedAge(t *testing.T) {
	start := clock.Reading{Wall: time.Unix(1700000000, 0)}
	for _, tc := range []struct {
		name          string
		wall, elapsed time.Duration
	}{
		{"backward correction", -time.Hour, time.Minute},
		{"forward correction or suspension", time.Minute, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lifetime := clock.NewLifetime(start, time.Minute)
			now := clock.Reading{Wall: start.Wall.Add(tc.wall), Elapsed: tc.elapsed}
			if lifetime.Remaining(now) != 0 {
				t.Fatal("expiry bound did not take effect")
			}
			now.Wall = start.Wall
			if lifetime.Remaining(now) > 0 {
				t.Fatal("wall correction revived expired evidence")
			}
		})
	}
}
