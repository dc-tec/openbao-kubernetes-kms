package status

import "errors"

var (
	// ErrConfigInvalid identifies invalid status controller construction settings.
	ErrConfigInvalid = errors.New("status config invalid")
	// ErrProbeFailed identifies a failed background status probe.
	ErrProbeFailed = errors.New("status probe failed")
	// ErrStateUnavailable identifies missing local registry state.
	ErrStateUnavailable = errors.New("status state unavailable")
	// ErrTransitMetadataInvalid identifies malformed or incomplete Transit metadata.
	ErrTransitMetadataInvalid = errors.New("transit metadata invalid")
	// ErrTransitKeyUnusable identifies Transit key settings that cannot safely serve KMS traffic.
	ErrTransitKeyUnusable = errors.New("transit key unusable")
	// ErrVersionRollback identifies observed Transit metadata that moves behind the active snapshot.
	ErrVersionRollback = errors.New("transit version rollback rejected")
)
