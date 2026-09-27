package auth

import (
	"sync"
	"time"

	clocktime "github.com/dc-tec/openbao-kubernetes-kms/internal/clock"
)

type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	elapsed time.Duration
}

func (f *fakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now.UTC()
}

func (f *fakeClock) advance(delta time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(delta)
	f.elapsed += delta
}

func (f *fakeClock) Read() clocktime.Reading {
	f.mu.Lock()
	defer f.mu.Unlock()
	return clocktime.Reading{Wall: f.now.UTC(), Elapsed: f.elapsed}
}
