package kmsv2

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/aad"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/openbao"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func TestLocalErrorPolicyContract(t *testing.T) {
	for _, tc := range []struct {
		name    string
		err     error
		code    codes.Code
		message string
		class   string
	}{
		{"canceled", context.Canceled, codes.Canceled, "request canceled", "canceled"},
		{"timeout", context.DeadlineExceeded, codes.DeadlineExceeded, "request timed out", "timeout"},
		{"refresh", ErrKeyMetadataRefresh, codes.Unavailable, "key metadata refresh failed", "key_metadata_refresh_failed"},
		{"plaintext", ErrPlaintextRequired, codes.InvalidArgument, "plaintext is required", "unknown"},
		{"ciphertext", ErrCiphertextRequired, codes.InvalidArgument, "ciphertext is required", "unknown"},
		{
			"request limit",
			ErrRequestLimitExceeded,
			codes.InvalidArgument,
			"kms request exceeds protocol limits",
			"protocol_limit",
		},
		{
			"response limit",
			ErrResponseLimitExceeded,
			codes.Internal,
			"kms response exceeds protocol limits",
			"protocol_limit",
		},
		{
			"concurrency",
			ErrConcurrencyLimitExceeded,
			codes.ResourceExhausted,
			"kms request concurrency limit reached",
			"concurrency_limit",
		},
		{"malformed key", keyregistry.ErrMalformedKeyID, codes.InvalidArgument, "key_id malformed", "key_id_malformed"},
		{"unknown key", keyregistry.ErrUnknownKeyID, codes.NotFound, "key_id unknown", "key_id_unknown"},
		{
			"invalid annotations",
			aad.ErrInvalidAnnotations,
			codes.InvalidArgument,
			"annotations invalid",
			"annotation_invalid",
		},
		{
			"mismatched annotations",
			aad.ErrAnnotationMismatch,
			codes.InvalidArgument,
			"annotations do not match key snapshot",
			"aad_mismatch",
		},
		{"missing AAD", aad.ErrAADRequired, codes.FailedPrecondition, "AAD required", "aad_missing"},
		{"status unavailable", ErrStatusUnavailable, codes.FailedPrecondition, "status unavailable", "status_stale"},
		{"status unhealthy", ErrStatusUnhealthy, codes.FailedPrecondition, "status unhealthy", "status_stale"},
		{"active key", ErrActiveKeyUnavailable, codes.FailedPrecondition, "active key unavailable", "status_stale"},
		{"status key mismatch", ErrStatusKeyIDMismatch, codes.FailedPrecondition, "status key_id mismatch", "status_stale"},
		{"panic", ErrPanicRecovered, codes.Internal, "kms request failed", "panic"},
		{"invalid Transit response", ErrTransitInvalidResponse, codes.Internal, "kms request failed", "unknown"},
		{"config", ErrConfigInvalid, codes.Internal, "kms request failed", "unknown"},
		{"unknown", errors.New("sensitive detail"), codes.Internal, "kms request failed", "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, err := range []error{tc.err, fmt.Errorf("sensitive detail: %w", tc.err)} {
				assertErrorPolicy(t, rpcError(err), errorClass(err), tc.code, tc.message, tc.class)
			}
		})
	}
	if rpcError(nil) != nil || errorClass(nil) != "" {
		t.Fatal("successful calls must not have an error or error class")
	}
}

func TestContextCausePrecedesRefreshAndAuthentication(t *testing.T) {
	for _, sentinel := range []error{ErrKeyMetadataRefresh, openbao.ErrAuthentication} {
		for _, tc := range []struct {
			cause   error
			code    codes.Code
			message string
			class   string
		}{
			{context.Canceled, codes.Canceled, "request canceled", "canceled"},
			{context.DeadlineExceeded, codes.DeadlineExceeded, "request timed out", "timeout"},
		} {
			for _, err := range []error{errors.Join(sentinel, tc.cause), errors.Join(tc.cause, sentinel)} {
				assertErrorPolicy(t, rpcError(err), errorClass(err), tc.code, tc.message, tc.class)
				assertErrorPolicy(t, transitRPCError(err, methodEncrypt), transitErrorClass(err), tc.code, tc.message, tc.class)
			}
		}
	}
}

func TestTransitErrorPolicyContract(t *testing.T) {
	for _, tc := range []struct {
		backend openbao.ErrorClass
		code    codes.Code
		message string
		class   string
	}{
		{openbao.ErrorClassUnauthenticated, codes.Unauthenticated, "transit authentication failed", "auth_failed"},
		{openbao.ErrorClassPermissionDenied, codes.PermissionDenied, "transit permission denied", "transit_policy_denied"},
		{openbao.ErrorClassNotFound, codes.NotFound, "transit key not found", "transit_key_missing"},
		{openbao.ErrorClassInvalidRequest, codes.InvalidArgument, "", "unknown"},
		{openbao.ErrorClassDecryptFailed, codes.InvalidArgument, "", "aad_mismatch"},
		{openbao.ErrorClassRateLimited, codes.ResourceExhausted, "transit rate limited", "openbao_rate_limited"},
		{openbao.ErrorClassSealed, codes.Unavailable, "transit unavailable", "openbao_sealed"},
		{openbao.ErrorClassUnavailable, codes.Unavailable, "transit unavailable", "openbao_unavailable"},
		{openbao.ErrorClassTLSFailed, codes.Unavailable, "transit unavailable", "openbao_tls_failed"},
		{openbao.ErrorClassDNSFailed, codes.Unavailable, "transit unavailable", "openbao_dns_failed"},
		{openbao.ErrorClassConnectionFailed, codes.Unavailable, "transit unavailable", "openbao_connection_failed"},
		{openbao.ErrorClassUnknown, codes.Unavailable, "transit unavailable", "unknown"},
	} {
		t.Run(string(tc.backend), func(t *testing.T) {
			err := fmt.Errorf("sensitive detail: %w", &openbao.Error{Class: tc.backend, Operation: "sensitive detail"})
			for _, method := range []string{methodEncrypt, methodDecrypt} {
				message := tc.message
				if tc.code == codes.InvalidArgument {
					message = "transit " + method + " failed"
				}
				assertErrorPolicy(t, transitRPCError(err, method), transitErrorClass(err), tc.code, message, tc.class)
			}
		})
	}

	err := errors.New("sensitive detail")
	assertErrorPolicy(t, transitRPCError(err, methodEncrypt), transitErrorClass(err),
		codes.Unavailable, "transit operation failed", "openbao_unavailable")
	if transitRPCError(nil, methodEncrypt) != nil || transitErrorClass(nil) != "" {
		t.Fatal("successful Transit calls must not have an error or error class")
	}
}

func TestTransitGRPCErrorPolicyContract(t *testing.T) {
	for _, tc := range []struct {
		code    codes.Code
		message string
		class   string
	}{
		{codes.Canceled, "request canceled", "canceled"},
		{codes.DeadlineExceeded, "request timed out", "timeout"},
		{codes.PermissionDenied, "transit permission denied", "transit_policy_denied"},
		{codes.Unauthenticated, "transit authentication failed", "auth_failed"},
		{codes.NotFound, "transit key not found", "transit_key_missing"},
		{codes.ResourceExhausted, "transit rate limited", "unknown"},
		{codes.Unavailable, "transit unavailable", "openbao_unavailable"},
		{codes.Internal, "transit operation failed", "unknown"},
		{codes.DataLoss, "transit operation failed", "unknown"},
	} {
		t.Run(tc.code.String(), func(t *testing.T) {
			err := grpcstatus.Error(tc.code, "sensitive detail")
			assertErrorPolicy(t, transitRPCError(err, methodDecrypt), transitErrorClass(err), tc.code, tc.message, tc.class)
		})
	}
}

func assertErrorPolicy(t *testing.T, err error, class string, code codes.Code, message, wantClass string) {
	t.Helper()
	status := grpcstatus.Convert(err)
	if status.Code() != code || status.Message() != message || class != wantClass {
		t.Fatalf("got (%s, %q, %q), want (%s, %q, %q)",
			status.Code(), status.Message(), class, code, message, wantClass)
	}
	if strings.Contains(status.Message(), "sensitive") {
		t.Fatal("public error contains raw cause details")
	}
}
