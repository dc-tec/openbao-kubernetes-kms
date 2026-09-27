// Package status maintains the cheap KMS Status view and rotation observation state.
package status

import clocktime "github.com/dc-tec/openbao-kubernetes-kms/internal/clock"

// Clock supplies wall time and process-local elapsed time.
type Clock = clocktime.Clock

func clockOrReal(clock Clock) Clock {
	if clock == nil {
		return clocktime.Real{}
	}
	return clock
}
