package status

import (
	"context"
	"fmt"
	"time"
)

// SchedulerOptions controls background probe cadence.
type SchedulerOptions struct {
	Controller        *Controller
	ProbeInterval     time.Duration
	DeepProbeInterval time.Duration
}

// Scheduler runs metadata and deep probes until the caller's context is canceled.
type Scheduler struct {
	controller        *Controller
	probeInterval     time.Duration
	deepProbeInterval time.Duration
}

// NewScheduler creates a background probe scheduler.
func NewScheduler(opts SchedulerOptions) (*Scheduler, error) {
	switch {
	case opts.Controller == nil:
		return nil, fmt.Errorf("%w: status controller is required", ErrConfigInvalid)
	case opts.ProbeInterval <= 0:
		return nil, fmt.Errorf("%w: probe interval must be positive", ErrConfigInvalid)
	case opts.DeepProbeInterval <= 0:
		return nil, fmt.Errorf("%w: deep probe interval must be positive", ErrConfigInvalid)
	}
	return &Scheduler{
		controller:        opts.Controller,
		probeInterval:     opts.ProbeInterval,
		deepProbeInterval: opts.DeepProbeInterval,
	}, nil
}

// Run starts probes and keeps running until ctx is canceled.
func (s *Scheduler) Run(ctx context.Context) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	_ = s.controller.ProbeOnce(ctx)

	probeTicker := time.NewTicker(s.probeInterval)
	defer probeTicker.Stop()
	deepTicker := time.NewTicker(s.deepProbeInterval)
	defer deepTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-probeTicker.C:
			_ = s.controller.ProbeOnce(ctx)
		case <-deepTicker.C:
			_ = s.controller.DeepProbeOnce(ctx)
		}
	}
}
