package kmsv2

import (
	"context"
	"errors"
	"testing"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/openbao"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func TestAuthenticationErrorsRetainCauseClassification(t *testing.T) {
	for _, tc := range []struct {
		name  string
		cause error
		code  codes.Code
		class string
	}{
		{"local identity", errors.New("sensitive identity details"), codes.Unauthenticated, errorClassAuthFailed},
		{
			"login denied",
			&openbao.Error{Class: openbao.ErrorClassPermissionDenied},
			codes.Unauthenticated,
			errorClassAuthFailed,
		},
		{
			"login bad request",
			&openbao.Error{Class: openbao.ErrorClassInvalidRequest},
			codes.Unauthenticated,
			errorClassAuthFailed,
		},
		{
			"unavailable",
			&openbao.Error{Class: openbao.ErrorClassUnavailable},
			codes.Unavailable,
			errorClassOpenBaoUnavailable,
		},
		{"sealed", &openbao.Error{Class: openbao.ErrorClassSealed}, codes.Unavailable, errorClassOpenBaoSealed},
		{
			"rate limited",
			&openbao.Error{Class: openbao.ErrorClassRateLimited},
			codes.ResourceExhausted,
			errorClassOpenBaoRateLimited,
		},
		{"deadline", context.DeadlineExceeded, codes.DeadlineExceeded, contextErrorClass(context.DeadlineExceeded)},
		{"canceled", context.Canceled, codes.Canceled, contextErrorClass(context.Canceled)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := errors.Join(openbao.ErrAuthentication, tc.cause)
			if code := grpcstatus.Code(transitRPCError(err)); code != tc.code {
				t.Fatalf("code %v, want %v", code, tc.code)
			}
			if class := transitErrorClass(err); class != tc.class {
				t.Fatalf("class %q, want %q", class, tc.class)
			}
		})
	}
}
