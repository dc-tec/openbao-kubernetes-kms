package kmsv2

import (
	"context"
	"errors"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/aad"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/openbao"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

// errorPolicy contains only approved public text and bounded observation classes.
type errorPolicy struct {
	code    codes.Code
	message string
	class   string
}

type sentinelErrorPolicy struct {
	sentinel error
	errorPolicy
}

// Match causes with errors.Is, including wrapped refresh failures. Context
// cancellation and deadlines take precedence over the operation that failed.
var sentinelErrorPolicies = []sentinelErrorPolicy{
	{context.Canceled, errorPolicy{codes.Canceled, "request canceled", errorClassCanceled}},
	{context.DeadlineExceeded, errorPolicy{codes.DeadlineExceeded, "request timed out", errorClassTimeout}},
	{ErrKeyMetadataRefresh, errorPolicy{codes.Unavailable, ErrKeyMetadataRefresh.Error(), errorClassKeyMetadataRefresh}},
	{ErrPlaintextRequired, errorPolicy{codes.InvalidArgument, "plaintext is required", errorClassUnknown}},
	{ErrCiphertextRequired, errorPolicy{codes.InvalidArgument, "ciphertext is required", errorClassUnknown}},
	{
		ErrRequestLimitExceeded,
		errorPolicy{codes.InvalidArgument, ErrRequestLimitExceeded.Error(), errorClassProtocolLimit},
	},
	{keyregistry.ErrMalformedKeyID, errorPolicy{codes.InvalidArgument, "key_id malformed", errorClassKeyIDMalformed}},
	{aad.ErrInvalidAnnotations, errorPolicy{codes.InvalidArgument, "annotations invalid", errorClassAnnotationInvalid}},
	{
		aad.ErrAnnotationMismatch,
		errorPolicy{codes.InvalidArgument, "annotations do not match key snapshot", errorClassAADMismatched},
	},
	{ErrResponseLimitExceeded, errorPolicy{codes.Internal, ErrResponseLimitExceeded.Error(), errorClassProtocolLimit}},
	{
		ErrConcurrencyLimitExceeded,
		errorPolicy{codes.ResourceExhausted, ErrConcurrencyLimitExceeded.Error(), errorClassConcurrencyLimit},
	},
	{keyregistry.ErrUnknownKeyID, errorPolicy{codes.NotFound, "key_id unknown", errorClassKeyIDUnknown}},
	{ErrStatusUnavailable, errorPolicy{codes.FailedPrecondition, ErrStatusUnavailable.Error(), errorClassStatusStale}},
	{ErrStatusUnhealthy, errorPolicy{codes.FailedPrecondition, ErrStatusUnhealthy.Error(), errorClassStatusStale}},
	{
		ErrActiveKeyUnavailable,
		errorPolicy{codes.FailedPrecondition, ErrActiveKeyUnavailable.Error(), errorClassStatusStale},
	},
	{ErrStatusKeyIDMismatch, errorPolicy{codes.FailedPrecondition, ErrStatusKeyIDMismatch.Error(), errorClassStatusStale}},
	{aad.ErrAADRequired, errorPolicy{codes.FailedPrecondition, "AAD required", errorClassAADMissing}},
	{ErrPanicRecovered, errorPolicy{codes.Internal, "kms request failed", errorClassPanic}},
}

var unknownLocalErrorPolicy = errorPolicy{codes.Internal, "kms request failed", errorClassUnknown}

// A gRPC code does not identify every backend cause. Typed OpenBao errors below
// retain sealed, rate-limit, and transport classes even when codes are shared.
var transitCodePolicies = map[codes.Code]errorPolicy{
	codes.Canceled:          {codes.Canceled, "request canceled", errorClassCanceled},
	codes.DeadlineExceeded:  {codes.DeadlineExceeded, "request timed out", errorClassTimeout},
	codes.PermissionDenied:  {codes.PermissionDenied, "transit permission denied", errorClassTransitPolicyDenied},
	codes.Unauthenticated:   {codes.Unauthenticated, "transit authentication failed", errorClassAuthFailed},
	codes.NotFound:          {codes.NotFound, "transit key not found", errorClassTransitKeyMissing},
	codes.InvalidArgument:   {codes.InvalidArgument, "transit decrypt failed", errorClassUnknown},
	codes.ResourceExhausted: {codes.ResourceExhausted, "transit rate limited", errorClassUnknown},
	codes.Unavailable:       {codes.Unavailable, "transit unavailable", errorClassOpenBaoUnavailable},
}

type backendErrorPolicy struct {
	code  codes.Code
	class string
}

var openBaoErrorPolicies = map[openbao.ErrorClass]backendErrorPolicy{
	openbao.ErrorClassUnauthenticated:  {codes.Unauthenticated, errorClassAuthFailed},
	openbao.ErrorClassPermissionDenied: {codes.PermissionDenied, errorClassTransitPolicyDenied},
	openbao.ErrorClassNotFound:         {codes.NotFound, errorClassTransitKeyMissing},
	openbao.ErrorClassInvalidRequest:   {codes.InvalidArgument, errorClassUnknown},
	openbao.ErrorClassDecryptFailed:    {codes.InvalidArgument, errorClassAADMismatched},
	openbao.ErrorClassRateLimited:      {codes.ResourceExhausted, errorClassOpenBaoRateLimited},
	openbao.ErrorClassSealed:           {codes.Unavailable, errorClassOpenBaoSealed},
	openbao.ErrorClassUnavailable:      {codes.Unavailable, errorClassOpenBaoUnavailable},
	openbao.ErrorClassTLSFailed:        {codes.Unavailable, errorClassOpenBaoTLSFailed},
	openbao.ErrorClassDNSFailed:        {codes.Unavailable, errorClassOpenBaoDNSFailed},
	openbao.ErrorClassConnectionFailed: {codes.Unavailable, errorClassOpenBaoConnectionFailed},
}

func (p errorPolicy) rpcError() error {
	return grpcstatus.Error(p.code, p.message)
}

func lookupLocalErrorPolicy(err error) (errorPolicy, bool) {
	for _, policy := range sentinelErrorPolicies {
		if errors.Is(err, policy.sentinel) {
			return policy.errorPolicy, true
		}
	}
	return unknownLocalErrorPolicy, false
}

func rpcError(err error) error {
	if err == nil {
		return nil
	}
	policy, _ := lookupLocalErrorPolicy(err)
	return policy.rpcError()
}

func errorClass(err error) string {
	if err == nil {
		return ""
	}
	if policy, ok := lookupLocalErrorPolicy(err); ok {
		return policy.class
	}
	return transitCodePolicy(grpcstatus.Code(err), "").class
}

func transitRPCError(err error, method string) error {
	if err == nil {
		return nil
	}
	return transitErrorPolicy(err, method).rpcError()
}

func transitErrorClass(err error) string {
	if err == nil {
		return ""
	}
	return transitErrorPolicy(err, "").class
}

func transitErrorPolicy(err error, method string) errorPolicy {
	if contextError(err) {
		policy, _ := lookupLocalErrorPolicy(err)
		return policy
	}
	if authenticationError(err) {
		return transitCodePolicy(codes.Unauthenticated, method)
	}

	var backendErr *openbao.Error
	hasBackendError := errors.As(err, &backendErr)
	backendPolicy := backendErrorPolicy{codes.Unavailable, errorClassUnknown}
	if hasBackendError {
		if policy, ok := openBaoErrorPolicies[backendErr.Class]; ok {
			backendPolicy = policy
		}
	}

	code := grpcstatus.Code(err)
	if code != codes.Unknown {
		policy := transitCodePolicy(code, method)
		if hasBackendError {
			policy.class = backendPolicy.class
		}
		return policy
	}
	if hasBackendError {
		policy := transitCodePolicy(backendPolicy.code, method)
		policy.class = backendPolicy.class
		return policy
	}
	return errorPolicy{codes.Unavailable, "transit operation failed", errorClassOpenBaoUnavailable}
}

func transitCodePolicy(code codes.Code, method string) errorPolicy {
	policy, ok := transitCodePolicies[code]
	if !ok {
		return errorPolicy{code, "transit operation failed", errorClassUnknown}
	}
	if code == codes.InvalidArgument && method == methodEncrypt {
		policy.message = "transit encrypt failed"
	}
	return policy
}

func authenticationError(err error) bool {
	if !errors.Is(err, openbao.ErrAuthentication) {
		return false
	}
	var apiErr *openbao.Error
	if errors.As(err, &apiErr) {
		switch apiErr.Class {
		case openbao.ErrorClassUnavailable, openbao.ErrorClassSealed, openbao.ErrorClassRateLimited,
			openbao.ErrorClassTLSFailed, openbao.ErrorClassDNSFailed, openbao.ErrorClassConnectionFailed:
			return false
		}
	}
	return true
}

func contextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func contextErrorClass(err error) string {
	if contextError(err) {
		policy, _ := lookupLocalErrorPolicy(err)
		return policy.class
	}
	return ""
}
