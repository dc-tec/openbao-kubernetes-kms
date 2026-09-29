package kmsv2

import (
	"context"
	"errors"
	"fmt"
	"time"

	grpcstatus "google.golang.org/grpc/status"
)

// transitFailure preserves the backend cause until the RPC boundary classifies
// it. Unknown backend failures and unknown local failures have different policies.
type transitFailure struct {
	cause error
}

func (e *transitFailure) Error() string { return "transit operation failed" }
func (e *transitFailure) Unwrap() error { return e.cause }

// finishRequest must be deferred directly by each public RPC handler so recover
// runs in the deferred function, after the inner handler releases its resources.
func (s *Server) finishRequest(
	ctx context.Context,
	observation *RequestObservation,
	start time.Time,
	err *error,
) {
	if recovered := recover(); recovered != nil {
		observation.PanicRecovered = true
		observation.PanicType = fmt.Sprintf("%T", recovered)
		*err = ErrPanicRecovered
	}
	if *err != nil {
		policy := requestErrorPolicy(*err, observation.Method)
		observation.ErrorClass = policy.class
		*err = policy.rpcError()
	}
	s.observeRequest(ctx, *observation, *err, time.Since(start))
}

func requestErrorPolicy(err error, method string) errorPolicy {
	var backend *transitFailure
	if errors.As(err, &backend) {
		return transitErrorPolicy(backend.cause, method)
	}
	policy, ok := lookupLocalErrorPolicy(err)
	if !ok {
		// A registry implementation can return a gRPC status. Preserve its
		// observation class while using the unknown-local response policy.
		policy.class = transitCodePolicy(grpcstatus.Code(err), method).class
	}
	return policy
}
