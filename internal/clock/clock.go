// Package clock separates process-local elapsed time from UTC wall time.
package clock

import "time"

var origin = time.Now()

// Reading samples wall time and elapsed time from the same instant. Elapsed
// values belong to one process and must never be persisted or compared across clocks.
type Reading struct {
	Wall    time.Time
	Elapsed time.Duration
}

// Clock supplies wall time for external timestamps and paired readings for deadlines.
type Clock interface {
	Now() time.Time
	Read() Reading
}

// Real uses the Go runtime's monotonic clock for elapsed time.
type Real struct{}

// Read returns one paired clock reading.
func (Real) Read() Reading {
	now := time.Now()
	return Reading{Wall: now.UTC(), Elapsed: now.Sub(origin)}
}

// Now returns UTC wall time for credential claims and operator output.
func (r Real) Now() time.Time { return r.Read().Wall }

// Deadline is an optional process-local elapsed-time deadline.
type Deadline struct {
	at  time.Duration
	set bool
}

// After creates a deadline relative to a clock reading.
func After(now Reading, delay time.Duration) Deadline {
	return Deadline{at: now.Elapsed + delay, set: true}
}

// Pending reports whether a set deadline has not yet elapsed.
func (d Deadline) Pending(now Reading) bool { return d.set && now.Elapsed < d.at }

// WallTime projects a deadline onto the current UTC wall clock for diagnostics.
func (d Deadline) WallTime(now Reading) time.Time {
	if !d.set {
		return time.Time{}
	}
	return now.Wall.Add(d.at - now.Elapsed)
}

// Lifetime tracks conservative age for cached evidence and backend leases.
// Callers must serialize access. Wall-clock advances and elapsed time can age
// evidence, but later corrections cannot make observed evidence younger.
type Lifetime struct {
	start Reading
	age   time.Duration
	limit time.Duration
	set   bool
}

// NewLifetime starts a lifetime at the supplied reading.
func NewLifetime(start Reading, limit time.Duration) Lifetime {
	return Lifetime{start: start, limit: limit, set: true}
}

// Age returns the greatest nonnegative age observed on either clock.
func (l *Lifetime) Age(now Reading) time.Duration {
	if !l.set {
		return 0
	}
	l.age = max(l.age, now.Elapsed-l.start.Elapsed, now.Wall.Sub(l.start.Wall))
	return l.age
}

// Remaining returns the conservative remaining duration, or zero if unset.
func (l *Lifetime) Remaining(now Reading) time.Duration {
	if !l.set {
		return 0
	}
	return l.limit - l.Age(now)
}

// IsSet reports whether a lifetime was initialized.
func (l Lifetime) IsSet() bool { return l.set }
