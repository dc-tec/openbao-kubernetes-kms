// Package kmsv2 implements the Kubernetes KMS v2 gRPC protocol boundary.
package kmsv2

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/aad"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"
	"github.com/dc-tec/openbao-kubernetes-kms/internal/openbao"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	kmsapi "k8s.io/kms/apis/v2"
)

const (
	// APIVersion is the Kubernetes KMS plugin API version served by this package.
	APIVersion = "v2"
	// HealthOK is the only healthy value accepted by kube-apiserver.
	HealthOK = "ok"
	// HealthUnhealthy is the generic redacted unhealthy status.
	HealthUnhealthy = "unhealthy"
)

const (
	messageAADRequired                 = "AAD required"
	messageActiveKeyUnavailable        = "active key unavailable"
	messageAnnotationEncodingInvalid   = "annotation encoding is invalid"
	messageAnnotationsInvalid          = "annotations invalid"
	messageAnnotationsMismatch         = "annotations do not match key snapshot"
	messageCiphertextRequired          = "ciphertext is required"
	messageKMSRequestFailed            = "kms request failed"
	messageKeyIDMalformed              = "key_id malformed"
	messageKeyIDUnknown                = "key_id unknown"
	messagePlaintextRequired           = "plaintext is required"
	messageRequestLimitExceeded        = "kms request exceeds protocol limits"
	messageRequestCanceled             = "request canceled"
	messageRequestTimedOut             = "request timed out"
	messageResponseLimitExceeded       = "kms response exceeds protocol limits"
	messageStatusKeyIDMismatch         = "status key_id mismatch"
	messageStatusUnavailable           = "status unavailable"
	messageStatusUnhealthy             = "status unhealthy"
	messageTransitAuthenticationFailed = "transit authentication failed"
	messageTransitDecryptFailed        = "transit decrypt failed"
	messageTransitKeyNotFound          = "transit key not found"
	messageTransitOperationFailed      = "transit operation failed"
	messageTransitPermissionDenied     = "transit permission denied"
	messageTransitRateLimited          = "transit rate limited"
	messageTransitUnavailable          = "transit unavailable"

	messageConfigPluginVersionRequired     = "plugin version is required"
	messageConfigRegistryRequired          = "key registry is required"
	messageConfigRequestTimeoutNonNegative = "request timeout must not be negative"
	messageConfigStatusCacheRequired       = "status cache is required"
	messageConfigTransitRequired           = "transit adapter is required"
)

var (
	// ErrConfigInvalid identifies invalid KMS v2 server construction settings.
	ErrConfigInvalid = errors.New("kmsv2 config invalid")
	// ErrStatusUnavailable identifies unavailable cached Status state.
	ErrStatusUnavailable = errors.New("status unavailable")
	// ErrStatusUnhealthy identifies cached Status state that is not healthy.
	ErrStatusUnhealthy = errors.New("status unhealthy")
	// ErrActiveKeyUnavailable identifies missing or invalid active key state.
	ErrActiveKeyUnavailable = errors.New("active key unavailable")
	// ErrStatusKeyIDMismatch identifies a cached Status key_id that does not match the active snapshot.
	ErrStatusKeyIDMismatch = errors.New("status key_id mismatch")
	// ErrPlaintextRequired identifies an empty KMS Encrypt plaintext.
	ErrPlaintextRequired = errors.New("plaintext required")
	// ErrCiphertextRequired identifies an empty KMS Decrypt ciphertext.
	ErrCiphertextRequired = errors.New("ciphertext required")
	// ErrTransitInvalidResponse identifies a malformed Transit response.
	ErrTransitInvalidResponse = errors.New("transit response invalid")
	// ErrRequestLimitExceeded identifies KMS request fields outside Kubernetes KMS v2 bounds.
	ErrRequestLimitExceeded = errors.New("kms request exceeds protocol limits")
	// ErrResponseLimitExceeded identifies KMS response fields outside Kubernetes KMS v2 bounds.
	ErrResponseLimitExceeded = errors.New("kms response exceeds protocol limits")
	// ErrPanicRecovered identifies a recovered panic inside a KMS v2 request handler.
	ErrPanicRecovered = errors.New("kms panic recovered")
)

// StatusCache exposes the cached Status view maintained by the status workstream.
type StatusCache interface {
	Current(context.Context) (CachedStatus, error)
}

// CachedStatus is the Status data consumed by KMS v2 request handlers.
type CachedStatus struct {
	Healthz string
	KeyID   string
	Active  keyregistry.KeySnapshot
}

// Transit is the narrow cryptographic operation surface needed by KMS v2.
type Transit interface {
	Encrypt(context.Context, TransitEncryptRequest) (TransitEncryptResponse, error)
	Decrypt(context.Context, TransitDecryptRequest) (TransitDecryptResponse, error)
}

// TransitEncryptRequest is one encrypt operation using an explicit Transit key version.
type TransitEncryptRequest struct {
	Plaintext      []byte
	AssociatedData []byte
	KeyVersion     int
}

// TransitEncryptResponse is the ciphertext returned by the Transit adapter.
type TransitEncryptResponse struct {
	Ciphertext []byte
	KeyVersion int
}

// TransitDecryptRequest is one decrypt operation with validated associated data.
type TransitDecryptRequest struct {
	Ciphertext     []byte
	AssociatedData []byte
}

// TransitDecryptResponse is the plaintext returned by the Transit adapter.
type TransitDecryptResponse struct {
	Plaintext []byte
}

// Options contains KMS v2 server dependencies.
type Options struct {
	StatusCache    StatusCache
	Registry       aad.SnapshotLookup
	Transit        Transit
	PluginVersion  string
	RequestTimeout time.Duration
	Observer       Observer
}

// Server implements the Kubernetes KMS v2 service.
type Server struct {
	kmsapi.UnimplementedKeyManagementServiceServer

	statusCache    StatusCache
	registry       aad.SnapshotLookup
	transit        Transit
	pluginVersion  string
	requestTimeout time.Duration
	observer       Observer
}

// NewServer builds a KMS v2 protocol server.
func NewServer(opts Options) (*Server, error) {
	if opts.StatusCache == nil {
		return nil, fmt.Errorf("%w: %s", ErrConfigInvalid, messageConfigStatusCacheRequired)
	}
	if opts.Registry == nil {
		return nil, fmt.Errorf("%w: %s", ErrConfigInvalid, messageConfigRegistryRequired)
	}
	if opts.Transit == nil {
		return nil, fmt.Errorf("%w: %s", ErrConfigInvalid, messageConfigTransitRequired)
	}
	if opts.PluginVersion == "" {
		return nil, fmt.Errorf("%w: %s", ErrConfigInvalid, messageConfigPluginVersionRequired)
	}
	if opts.RequestTimeout < 0 {
		return nil, fmt.Errorf("%w: %s", ErrConfigInvalid, messageConfigRequestTimeoutNonNegative)
	}

	return &Server{
		statusCache:    opts.StatusCache,
		registry:       opts.Registry,
		transit:        opts.Transit,
		pluginVersion:  opts.PluginVersion,
		requestTimeout: opts.RequestTimeout,
		observer:       opts.Observer,
	}, nil
}

// Register adds the KMS v2 service to a gRPC registrar.
func Register(registrar grpc.ServiceRegistrar, server *Server) {
	kmsapi.RegisterKeyManagementServiceServer(registrar, server)
}

// Status returns cached plugin health and active key_id without calling Transit.
func (s *Server) Status(ctx context.Context, _ *kmsapi.StatusRequest) (response *kmsapi.StatusResponse, err error) {
	start := time.Now()
	observation := RequestObservation{Method: methodStatus}
	defer func() {
		if response != nil {
			observation.Healthz = response.GetHealthz()
			if response.GetKeyId() != "" {
				observation.KeyIDHash = aad.HashValue(response.GetKeyId())
			}
		}
		s.observeRequest(ctx, observation, err, time.Since(start))
	}()
	defer recoverRPC(&err, &observation)

	requestCtx, cancel := s.requestContext(ctx)
	defer cancel()

	cached, err := s.statusCache.Current(requestCtx)
	if err != nil {
		if contextError(err) {
			return nil, rpcError(err)
		}
		return &kmsapi.StatusResponse{
			Version: APIVersion,
			Healthz: HealthUnhealthy,
		}, nil
	}

	return statusResponse(cached), nil
}

// Encrypt encrypts plaintext using the active cached key snapshot.
func (s *Server) Encrypt(
	ctx context.Context,
	request *kmsapi.EncryptRequest,
) (response *kmsapi.EncryptResponse, err error) {
	start := time.Now()
	observation := RequestObservation{Method: methodEncrypt}
	if request != nil && request.GetUid() != "" {
		observation.RequestUIDHash = aad.HashValue(request.GetUid())
	}
	defer func() {
		s.observeRequest(ctx, observation, err, time.Since(start))
	}()
	defer recoverRPC(&err, &observation)

	if request == nil || len(request.GetPlaintext()) == 0 {
		return nil, rpcError(ErrPlaintextRequired)
	}

	requestCtx, cancel := s.requestContext(ctx)
	defer cancel()

	active, keyID, err := s.activeStatus(requestCtx)
	if err != nil {
		observation.ErrorClass = errorClass(err)
		return nil, rpcError(err)
	}
	observation.KeyIDHash = aad.HashValue(keyID)
	observation.TransitKeyVersion = active.TransitVersion

	annotations, err := aad.BuildAnnotations(active, s.pluginVersion)
	if err != nil {
		observation.ErrorClass = errorClass(err)
		return nil, rpcError(err)
	}
	canonicalAAD, err := aad.BuildCanonical(active, annotations)
	if err != nil {
		observation.ErrorClass = errorClass(err)
		return nil, rpcError(err)
	}

	encrypted, err := s.transit.Encrypt(requestCtx, TransitEncryptRequest{
		Plaintext:      slices.Clone(request.GetPlaintext()),
		AssociatedData: canonicalAAD,
		KeyVersion:     active.TransitVersion,
	})
	if err != nil {
		observation.ErrorClass = transitErrorClass(err)
		return nil, transitRPCError(err)
	}
	if len(encrypted.Ciphertext) == 0 {
		observation.ErrorClass = errorClassUnknown
		return nil, rpcError(ErrTransitInvalidResponse)
	}
	if encrypted.KeyVersion != 0 && encrypted.KeyVersion != active.TransitVersion {
		observation.ErrorClass = errorClassUnknown
		return nil, rpcError(ErrTransitInvalidResponse)
	}

	response = &kmsapi.EncryptResponse{
		Ciphertext:  slices.Clone(encrypted.Ciphertext),
		KeyId:       keyID,
		Annotations: annotationsToProto(annotations),
	}
	if err := validateEncryptResponseLimits(response); err != nil {
		observation.ErrorClass = errorClass(err)
		return nil, rpcError(err)
	}
	return response, nil
}

// Decrypt validates key_id and annotations before calling Transit decrypt.
func (s *Server) Decrypt(
	ctx context.Context,
	request *kmsapi.DecryptRequest,
) (response *kmsapi.DecryptResponse, err error) {
	start := time.Now()
	observation := RequestObservation{Method: methodDecrypt}
	if request != nil {
		if request.GetUid() != "" {
			observation.RequestUIDHash = aad.HashValue(request.GetUid())
		}
		if request.GetKeyId() != "" {
			observation.KeyIDHash = aad.HashValue(request.GetKeyId())
		}
	}
	defer func() {
		s.observeRequest(ctx, observation, err, time.Since(start))
	}()
	defer recoverRPC(&err, &observation)

	if request == nil || len(request.GetCiphertext()) == 0 {
		return nil, rpcError(ErrCiphertextRequired)
	}
	if err := validateDecryptRequestLimits(request); err != nil {
		observation.ErrorClass = errorClass(err)
		return nil, rpcError(err)
	}

	requestCtx, cancel := s.requestContext(ctx)
	defer cancel()

	annotations, err := annotationsFromProto(request.GetAnnotations())
	if err != nil {
		s.observeValidationError(err)
		observation.ErrorClass = errorClass(err)
		return nil, rpcError(err)
	}
	prepared, err := aad.PrepareDecrypt(s.registry, request.GetKeyId(), annotations)
	if err != nil {
		s.observeValidationError(err)
		observation.ErrorClass = errorClass(err)
		return nil, rpcError(err)
	}
	observation.KeyIDHash = aad.HashValue(prepared.Snapshot.KubernetesKeyID)
	observation.TransitKeyVersion = prepared.Snapshot.TransitVersion

	decrypted, err := s.transit.Decrypt(requestCtx, TransitDecryptRequest{
		Ciphertext:     slices.Clone(request.GetCiphertext()),
		AssociatedData: prepared.Canonical,
	})
	if err != nil {
		observation.ErrorClass = transitErrorClass(err)
		return nil, transitRPCError(err)
	}

	return &kmsapi.DecryptResponse{Plaintext: slices.Clone(decrypted.Plaintext)}, nil
}

func (s *Server) requestContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if s.requestTimeout == 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, s.requestTimeout)
}

func (s *Server) activeStatus(ctx context.Context) (keyregistry.KeySnapshot, string, error) {
	cached, err := s.statusCache.Current(ctx)
	if err != nil {
		if contextError(err) {
			return keyregistry.KeySnapshot{}, "", err
		}
		return keyregistry.KeySnapshot{}, "", ErrStatusUnavailable
	}
	if cached.Healthz != HealthOK {
		return keyregistry.KeySnapshot{}, "", ErrStatusUnhealthy
	}
	active, err := cached.Active.Normalize()
	if err != nil {
		return keyregistry.KeySnapshot{}, "", fmt.Errorf("%w: %v", ErrActiveKeyUnavailable, err)
	}
	if active.State != keyregistry.StateActive {
		return keyregistry.KeySnapshot{}, "", ErrActiveKeyUnavailable
	}
	if cached.KeyID == "" {
		return keyregistry.KeySnapshot{}, "", ErrActiveKeyUnavailable
	}
	if cached.KeyID != active.KubernetesKeyID {
		return keyregistry.KeySnapshot{}, "", ErrStatusKeyIDMismatch
	}
	return active, cached.KeyID, nil
}

func statusResponse(cached CachedStatus) *kmsapi.StatusResponse {
	healthz := cached.Healthz
	keyID := cached.KeyID
	if healthz == "" {
		healthz = HealthUnhealthy
	}
	if healthz != HealthOK {
		keyID = ""
	}
	if healthz == HealthOK {
		active, err := cached.Active.Normalize()
		if err != nil || active.State != keyregistry.StateActive || active.KubernetesKeyID != keyID {
			healthz = HealthUnhealthy
			keyID = ""
		}
	}

	return &kmsapi.StatusResponse{
		Version: APIVersion,
		Healthz: healthz,
		KeyId:   keyID,
	}
}

func annotationsToProto(annotations map[string]string) map[string][]byte {
	encoded := make(map[string][]byte, len(annotations))
	for key, value := range annotations {
		encoded[key] = []byte(value)
	}
	return encoded
}

func annotationsFromProto(annotations map[string][]byte) (map[string]string, error) {
	if err := validateAnnotationsProtoLimits(annotations, ErrRequestLimitExceeded); err != nil {
		return nil, err
	}
	decoded := make(map[string]string, len(annotations))
	for key, value := range annotations {
		decoded[key] = string(value)
	}
	return decoded, nil
}

func recoverRPC(err *error, observation *RequestObservation) {
	if recovered := recover(); recovered != nil {
		if observation != nil {
			observation.ErrorClass = errorClass(ErrPanicRecovered)
			observation.PanicRecovered = true
			observation.PanicType = fmt.Sprintf("%T", recovered)
		}
		*err = grpcstatus.Error(codes.Internal, messageKMSRequestFailed)
	}
}

func rpcError(err error) error {
	if err == nil {
		return nil
	}
	if contextError(err) {
		return contextRPCError(err)
	}
	switch {
	case errors.Is(err, ErrPlaintextRequired),
		errors.Is(err, ErrCiphertextRequired),
		errors.Is(err, ErrRequestLimitExceeded),
		errors.Is(err, keyregistry.ErrMalformedKeyID),
		errors.Is(err, aad.ErrInvalidAnnotations),
		errors.Is(err, aad.ErrAnnotationMismatch):
		return grpcstatus.Error(codes.InvalidArgument, safeMessage(err))
	case errors.Is(err, ErrResponseLimitExceeded):
		return grpcstatus.Error(codes.Internal, safeMessage(err))
	case errors.Is(err, keyregistry.ErrUnknownKeyID):
		return grpcstatus.Error(codes.NotFound, safeMessage(err))
	case errors.Is(err, ErrStatusUnavailable),
		errors.Is(err, ErrStatusUnhealthy),
		errors.Is(err, ErrActiveKeyUnavailable),
		errors.Is(err, ErrStatusKeyIDMismatch),
		errors.Is(err, aad.ErrAADRequired):
		return grpcstatus.Error(codes.FailedPrecondition, safeMessage(err))
	default:
		return grpcstatus.Error(codes.Internal, messageKMSRequestFailed)
	}
}

func transitRPCError(err error) error {
	if err == nil {
		return nil
	}
	if contextError(err) {
		return contextRPCError(err)
	}
	code := grpcstatus.Code(err)
	if code != codes.Unknown {
		return grpcstatus.Error(code, safeCodeMessage(code))
	}
	var openBaoErr *openbao.Error
	if errors.As(err, &openBaoErr) {
		code = openBaoRPCCode(openBaoErr.Class)
		return grpcstatus.Error(code, safeCodeMessage(code))
	}
	return grpcstatus.Error(codes.Unavailable, messageTransitOperationFailed)
}

func transitErrorClass(err error) string {
	if err == nil {
		return ""
	}
	if class := contextErrorClass(err); class != "" {
		return class
	}
	var openBaoErr *openbao.Error
	if errors.As(err, &openBaoErr) {
		return openBaoKMSClass(openBaoErr.Class)
	}
	code := grpcstatus.Code(err)
	if code == codes.Unknown {
		return errorClassOpenBaoUnavailable
	}
	if class, ok := grpcErrorClasses[code]; ok {
		return class
	}
	return errorClassUnknown
}

func openBaoRPCCode(class openbao.ErrorClass) codes.Code {
	switch class {
	case openbao.ErrorClassUnauthenticated:
		return codes.Unauthenticated
	case openbao.ErrorClassPermissionDenied:
		return codes.PermissionDenied
	case openbao.ErrorClassNotFound:
		return codes.NotFound
	case openbao.ErrorClassDecryptFailed,
		openbao.ErrorClassInvalidRequest:
		return codes.InvalidArgument
	case openbao.ErrorClassRateLimited:
		return codes.ResourceExhausted
	case openbao.ErrorClassSealed,
		openbao.ErrorClassUnavailable:
		return codes.Unavailable
	default:
		return codes.Unavailable
	}
}

func openBaoKMSClass(class openbao.ErrorClass) string {
	switch class {
	case openbao.ErrorClassUnauthenticated:
		return errorClassAuthFailed
	case openbao.ErrorClassPermissionDenied:
		return errorClassTransitPolicyDenied
	case openbao.ErrorClassNotFound:
		return errorClassTransitKeyMissing
	case openbao.ErrorClassDecryptFailed:
		return errorClassAADMismatched
	case openbao.ErrorClassRateLimited:
		return errorClassOpenBaoRateLimited
	case openbao.ErrorClassSealed:
		return errorClassOpenBaoSealed
	case openbao.ErrorClassUnavailable:
		return errorClassOpenBaoUnavailable
	default:
		return errorClassUnknown
	}
}

func contextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func contextRPCError(err error) error {
	if errors.Is(err, context.Canceled) {
		return grpcstatus.Error(codes.Canceled, messageRequestCanceled)
	}
	return grpcstatus.Error(codes.DeadlineExceeded, messageRequestTimedOut)
}

func safeMessage(err error) string {
	switch {
	case errors.Is(err, ErrPlaintextRequired):
		return messagePlaintextRequired
	case errors.Is(err, ErrCiphertextRequired):
		return messageCiphertextRequired
	case errors.Is(err, ErrRequestLimitExceeded):
		return messageRequestLimitExceeded
	case errors.Is(err, ErrResponseLimitExceeded):
		return messageResponseLimitExceeded
	case errors.Is(err, keyregistry.ErrMalformedKeyID):
		return messageKeyIDMalformed
	case errors.Is(err, keyregistry.ErrUnknownKeyID):
		return messageKeyIDUnknown
	case errors.Is(err, aad.ErrInvalidAnnotations):
		return messageAnnotationsInvalid
	case errors.Is(err, aad.ErrAnnotationMismatch):
		return messageAnnotationsMismatch
	case errors.Is(err, aad.ErrAADRequired):
		return messageAADRequired
	case errors.Is(err, ErrPanicRecovered):
		return messageKMSRequestFailed
	case errors.Is(err, ErrStatusUnavailable):
		return messageStatusUnavailable
	case errors.Is(err, ErrStatusUnhealthy):
		return messageStatusUnhealthy
	case errors.Is(err, ErrActiveKeyUnavailable):
		return messageActiveKeyUnavailable
	case errors.Is(err, ErrStatusKeyIDMismatch):
		return messageStatusKeyIDMismatch
	default:
		return messageKMSRequestFailed
	}
}

func safeCodeMessage(code codes.Code) string {
	switch code {
	case codes.Canceled:
		return messageRequestCanceled
	case codes.DeadlineExceeded:
		return messageRequestTimedOut
	case codes.PermissionDenied:
		return messageTransitPermissionDenied
	case codes.Unauthenticated:
		return messageTransitAuthenticationFailed
	case codes.NotFound:
		return messageTransitKeyNotFound
	case codes.InvalidArgument:
		return messageTransitDecryptFailed
	case codes.ResourceExhausted:
		return messageTransitRateLimited
	case codes.Unavailable:
		return messageTransitUnavailable
	default:
		return messageTransitOperationFailed
	}
}
