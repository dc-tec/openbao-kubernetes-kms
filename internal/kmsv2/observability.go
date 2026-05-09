package kmsv2

import (
	"context"
	"errors"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/aad"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

const (
	methodStatus  = "status"
	methodEncrypt = "encrypt"
	methodDecrypt = "decrypt"

	errorClassAADMissing          = "aad_missing"
	errorClassAADMismatched       = "aad_mismatch"
	errorClassAnnotationInvalid   = "annotation_invalid"
	errorClassKeyIDMalformed      = "key_id_malformed"
	errorClassKeyIDUnknown        = "key_id_unknown"
	errorClassOpenBaoUnavailable  = "openbao_unavailable"
	errorClassStatusStale         = "status_stale"
	errorClassTimeout             = "timeout"
	errorClassTransitKeyMissing   = "transit_key_missing"
	errorClassTransitPolicyDenied = "transit_policy_denied"
	errorClassAuthFailed          = "auth_failed"
	errorClassCanceled            = "canceled"
	errorClassUnknown             = "unknown"
)

var grpcStatusLabels = map[codes.Code]string{
	codes.OK:                 "ok",
	codes.Canceled:           "canceled",
	codes.Unknown:            "unknown",
	codes.InvalidArgument:    "invalid_argument",
	codes.DeadlineExceeded:   "deadline_exceeded",
	codes.NotFound:           "not_found",
	codes.AlreadyExists:      "already_exists",
	codes.PermissionDenied:   "permission_denied",
	codes.ResourceExhausted:  "resource_exhausted",
	codes.FailedPrecondition: "failed_precondition",
	codes.Aborted:            "aborted",
	codes.OutOfRange:         "out_of_range",
	codes.Unimplemented:      "unimplemented",
	codes.Internal:           "internal",
	codes.Unavailable:        "unavailable",
	codes.DataLoss:           "data_loss",
	codes.Unauthenticated:    "unauthenticated",
}

var grpcErrorClasses = map[codes.Code]string{
	codes.Canceled:         errorClassCanceled,
	codes.DeadlineExceeded: errorClassTimeout,
	codes.PermissionDenied: errorClassTransitPolicyDenied,
	codes.Unauthenticated:  errorClassAuthFailed,
	codes.NotFound:         errorClassTransitKeyMissing,
	codes.Unavailable:      errorClassOpenBaoUnavailable,
}

// RequestObservation is one redacted KMS v2 request observation.
type RequestObservation struct {
	Method            string
	Status            string
	Duration          time.Duration
	KeyIDHash         string
	TransitKeyVersion int
	RequestUIDHash    string
	ErrorClass        string
	Healthz           string
}

// Observer receives redacted KMS v2 request and validation observations.
type Observer interface {
	ObserveKMSRequest(context.Context, RequestObservation)
	ObserveAADValidationError(string)
	ObserveDecryptKeyIDError(string)
}

func (s *Server) observeRequest(
	ctx context.Context,
	observation RequestObservation,
	err error,
	duration time.Duration,
) {
	if s.observer == nil {
		return
	}
	observation.Status = statusLabel(err)
	observation.Duration = duration
	if observation.ErrorClass == "" {
		observation.ErrorClass = errorClass(err)
	}
	s.observer.ObserveKMSRequest(ctx, observation)
}

func (s *Server) observeValidationError(err error) {
	if s.observer == nil || err == nil {
		return
	}
	switch {
	case errors.Is(err, keyregistry.ErrMalformedKeyID):
		s.observer.ObserveDecryptKeyIDError(errorClassKeyIDMalformed)
	case errors.Is(err, keyregistry.ErrUnknownKeyID):
		s.observer.ObserveDecryptKeyIDError(errorClassKeyIDUnknown)
	case errors.Is(err, aad.ErrAADRequired):
		s.observer.ObserveAADValidationError(errorClassAADMissing)
	case errors.Is(err, aad.ErrAnnotationMismatch):
		s.observer.ObserveAADValidationError(errorClassAADMismatched)
	case errors.Is(err, aad.ErrInvalidAnnotations):
		s.observer.ObserveAADValidationError(errorClassAnnotationInvalid)
	}
}

func statusLabel(err error) string {
	if err == nil {
		return "ok"
	}
	if label, ok := grpcStatusLabels[grpcstatus.Code(err)]; ok {
		return label
	}
	return "unknown"
}

func errorClass(err error) string {
	if err == nil {
		return ""
	}
	if class := contextErrorClass(err); class != "" {
		return class
	}
	if class := validationErrorClass(err); class != "" {
		return class
	}
	if class, ok := grpcErrorClasses[grpcstatus.Code(err)]; ok {
		return class
	}
	return errorClassUnknown
}

func contextErrorClass(err error) string {
	if contextError(err) {
		if errors.Is(err, context.Canceled) {
			return errorClassCanceled
		}
		return errorClassTimeout
	}
	return ""
}

func validationErrorClass(err error) string {
	switch {
	case errors.Is(err, keyregistry.ErrMalformedKeyID):
		return errorClassKeyIDMalformed
	case errors.Is(err, keyregistry.ErrUnknownKeyID):
		return errorClassKeyIDUnknown
	case errors.Is(err, aad.ErrAADRequired):
		return errorClassAADMissing
	case errors.Is(err, aad.ErrInvalidAnnotations):
		return errorClassAnnotationInvalid
	case errors.Is(err, aad.ErrAnnotationMismatch):
		return errorClassAADMismatched
	case errors.Is(err, ErrStatusUnavailable),
		errors.Is(err, ErrStatusUnhealthy),
		errors.Is(err, ErrActiveKeyUnavailable),
		errors.Is(err, ErrStatusKeyIDMismatch):
		return errorClassStatusStale
	}
	return ""
}
