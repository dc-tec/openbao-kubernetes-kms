package kmsv2

type concurrencyLimiter struct {
	slots chan struct{}
}

func newConcurrencyLimiter(limit int) *concurrencyLimiter {
	return &concurrencyLimiter{slots: make(chan struct{}, limit)}
}

func (l *concurrencyLimiter) tryAcquire() bool {
	select {
	case l.slots <- struct{}{}:
		return true
	default:
		return false
	}
}

func (l *concurrencyLimiter) release() {
	<-l.slots
}

func (l *concurrencyLimiter) inFlight() int {
	return len(l.slots)
}

// InFlightKMSRequests returns the current Status, Encrypt, and Decrypt request counts.
func (s *Server) InFlightKMSRequests() (status int, encrypt int, decrypt int) {
	return s.statusLimiter.inFlight(), s.encryptLimiter.inFlight(), s.decryptLimiter.inFlight()
}
