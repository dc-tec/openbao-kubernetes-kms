package status

import (
	"time"

	clocktime "github.com/dc-tec/openbao-kubernetes-kms/internal/clock"
)

const (
	defaultCircuitBreakerFailureThreshold = 3
	defaultCircuitBreakerOpenDuration     = 30 * time.Second
)

// CircuitBreakerState is the redacted lifecycle state exposed to diagnostics.
type CircuitBreakerState string

const (
	// CircuitBreakerClosed means probes are allowed.
	CircuitBreakerClosed CircuitBreakerState = "closed"
	// CircuitBreakerOpen means probes are temporarily skipped.
	CircuitBreakerOpen CircuitBreakerState = "open"
)

// CircuitBreakerOptions controls repeated dependency-failure backoff.
type CircuitBreakerOptions struct {
	FailureThreshold int
	OpenDuration     time.Duration
}

// CircuitBreakerSnapshot is safe to expose through diagnostics and metrics.
type CircuitBreakerSnapshot struct {
	State               CircuitBreakerState
	ConsecutiveFailures int
	OpenedAt            time.Time
	OpenUntil           time.Time
	LastFailureAt       time.Time
}

type circuitBreaker struct {
	failureThreshold    int
	openDuration        time.Duration
	state               CircuitBreakerState
	consecutiveFailures int
	openedAt            time.Time
	openUntil           clocktime.Deadline
	lastFailureAt       time.Time
}

func newCircuitBreaker(opts CircuitBreakerOptions) circuitBreaker {
	threshold := opts.FailureThreshold
	if threshold <= 0 {
		threshold = defaultCircuitBreakerFailureThreshold
	}
	duration := opts.OpenDuration
	if duration <= 0 {
		duration = defaultCircuitBreakerOpenDuration
	}
	return circuitBreaker{
		failureThreshold: threshold,
		openDuration:     duration,
		state:            CircuitBreakerClosed,
	}
}

func (b *circuitBreaker) allow(now clocktime.Reading) bool {
	if b.state != CircuitBreakerOpen {
		return true
	}
	if !b.openUntil.Pending(now) {
		b.state = CircuitBreakerClosed
		return true
	}
	return false
}

func (b *circuitBreaker) recordSuccess() {
	b.state = CircuitBreakerClosed
	b.consecutiveFailures = 0
	b.openedAt = time.Time{}
	b.openUntil = clocktime.Deadline{}
	b.lastFailureAt = time.Time{}
}

func (b *circuitBreaker) recordFailure(now clocktime.Reading) {
	b.consecutiveFailures++
	b.lastFailureAt = now.Wall
	if b.consecutiveFailures >= b.failureThreshold {
		b.state = CircuitBreakerOpen
		b.openedAt = now.Wall
		b.openUntil = clocktime.After(now, b.openDuration)
	}
}

func (b circuitBreaker) snapshot(now clocktime.Reading) CircuitBreakerSnapshot {
	state := b.state
	if state == "" {
		state = CircuitBreakerClosed
	}
	return CircuitBreakerSnapshot{
		State:               state,
		ConsecutiveFailures: b.consecutiveFailures,
		OpenedAt:            b.openedAt,
		OpenUntil:           b.openUntil.WallTime(now),
		LastFailureAt:       b.lastFailureAt,
	}
}

func aggregateCircuitBreakerSnapshots(
	metadata CircuitBreakerSnapshot,
	deep CircuitBreakerSnapshot,
) CircuitBreakerSnapshot {
	return CircuitBreakerSnapshot{
		State:               aggregateCircuitBreakerState(metadata.State, deep.State),
		ConsecutiveFailures: max(metadata.ConsecutiveFailures, deep.ConsecutiveFailures),
		OpenedAt:            earliestTime(metadata.OpenedAt, deep.OpenedAt),
		OpenUntil:           latestTime(metadata.OpenUntil, deep.OpenUntil),
		LastFailureAt:       latestTime(metadata.LastFailureAt, deep.LastFailureAt),
	}
}

func aggregateCircuitBreakerState(states ...CircuitBreakerState) CircuitBreakerState {
	for _, state := range states {
		if state == CircuitBreakerOpen {
			return CircuitBreakerOpen
		}
	}
	return CircuitBreakerClosed
}

func earliestTime(first time.Time, second time.Time) time.Time {
	if first.IsZero() {
		return second
	}
	if second.IsZero() || first.Before(second) {
		return first
	}
	return second
}

func latestTime(first time.Time, second time.Time) time.Time {
	if first.After(second) {
		return first
	}
	return second
}
