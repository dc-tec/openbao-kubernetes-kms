// Package openbao provides a narrow, typed OpenBao Transit client.
package openbao

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

const (
	sealedErrorFragment                 = "sealed"
	messageAuthenticationFailedFragment = "message authentication failed"
	requestStatusOK                     = "ok"
	requestStatusError                  = "error"
)

// ErrorClass is a stable OpenBao error category for callers and metrics.
type ErrorClass string

const (
	ErrorClassInvalidRequest   ErrorClass = "invalid_request"
	ErrorClassUnauthenticated  ErrorClass = "unauthenticated"
	ErrorClassPermissionDenied ErrorClass = "permission_denied"
	ErrorClassNotFound         ErrorClass = "not_found"
	ErrorClassDecryptFailed    ErrorClass = "decrypt_failed"
	ErrorClassRateLimited      ErrorClass = "rate_limited"
	ErrorClassUnavailable      ErrorClass = "unavailable"
	ErrorClassSealed           ErrorClass = "sealed"
	ErrorClassUnknown          ErrorClass = "unknown"
)

// Error is a redacted OpenBao API error.
type Error struct {
	Class      ErrorClass
	StatusCode int
	Operation  string
}

// Error returns a token/payload/path-safe message.
func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.StatusCode == 0 {
		return fmt.Sprintf("openbao %s failed: %s", e.Operation, e.Class)
	}
	return fmt.Sprintf("openbao %s failed: %s (status %d)", e.Operation, e.Class, e.StatusCode)
}

// Is reports equality by error class.
func (e *Error) Is(target error) bool {
	var targetError *Error
	if !errors.As(target, &targetError) {
		return false
	}
	return e.Class == targetError.Class
}

func classifyError(statusCode int, messages []string) ErrorClass {
	joined := strings.ToLower(strings.Join(messages, "\n"))
	if strings.Contains(joined, sealedErrorFragment) {
		return ErrorClassSealed
	}
	if strings.Contains(joined, messageAuthenticationFailedFragment) {
		return ErrorClassDecryptFailed
	}

	switch statusCode {
	case http.StatusBadRequest:
		return ErrorClassInvalidRequest
	case http.StatusUnauthorized:
		return ErrorClassUnauthenticated
	case http.StatusForbidden:
		return ErrorClassPermissionDenied
	case http.StatusNotFound:
		return ErrorClassNotFound
	case http.StatusTooManyRequests:
		return ErrorClassRateLimited
	default:
		if statusCode >= http.StatusInternalServerError {
			return ErrorClassUnavailable
		}
	}
	return ErrorClassUnknown
}

func newHTTPError(operation string, statusCode int, messages []string) *Error {
	return &Error{
		Class:      classifyError(statusCode, messages),
		StatusCode: statusCode,
		Operation:  operation,
	}
}

func requestStatus(err error) string {
	if err == nil {
		return requestStatusOK
	}
	var openBaoErr *Error
	if errors.As(err, &openBaoErr) {
		return string(openBaoErr.Class)
	}
	return requestStatusError
}

func requestErrorClass(err error) ErrorClass {
	if err == nil {
		return ""
	}
	var openBaoErr *Error
	if errors.As(err, &openBaoErr) {
		return openBaoErr.Class
	}
	return ErrorClassUnknown
}
