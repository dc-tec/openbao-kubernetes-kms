package status

import (
	"testing"
	"time"

	clocktime "github.com/dc-tec/openbao-kubernetes-kms/internal/clock"
)

func TestCircuitBreakerCooldownIgnoresWallClockJumps(t *testing.T) {
	now := clocktime.Reading{Wall: time.Unix(1700000000, 0)}
	breaker := newCircuitBreaker(CircuitBreakerOptions{FailureThreshold: 1, OpenDuration: time.Minute})
	breaker.recordFailure(now)
	now.Wall = now.Wall.Add(time.Hour)
	if breaker.allow(now) {
		t.Fatal("forward jump bypassed cooldown")
	}
	now.Wall = now.Wall.Add(-2 * time.Hour)
	now.Elapsed = time.Minute
	if !breaker.allow(now) {
		t.Fatal("backward jump prolonged cooldown")
	}
}
