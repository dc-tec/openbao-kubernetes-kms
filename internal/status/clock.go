// Package status maintains the cheap KMS Status view and rotation observation state.
package status

import "time"

// Clock is the time source used by cache staleness and rotation tests.
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time {
	return time.Now().UTC()
}

func clockOrReal(clock Clock) Clock {
	if clock == nil {
		return realClock{}
	}
	return clock
}
