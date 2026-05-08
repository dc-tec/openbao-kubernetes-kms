package auth

import "time"

type fakeClock struct {
	now time.Time
}

func (f *fakeClock) Now() time.Time {
	return f.now.UTC()
}

func (f *fakeClock) advance(delta time.Duration) {
	f.now = f.now.Add(delta)
}
