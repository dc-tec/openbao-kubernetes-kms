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
	"google.golang.org/grpc"
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
	messageAnnotationEncodingInvalid = "annotation encoding is invalid"

	messageConfigPluginVersionRequired     = "plugin version is required"
	messageConfigMaxConcurrentDecrypt      = "max concurrent decrypt requests must be between 1 and 1024"
	messageConfigMaxConcurrentEncrypt      = "max concurrent encrypt requests must be between 1 and 1024"
	messageConfigMaxConcurrentStatus       = "max concurrent status requests must be between 1 and 1024"
	messageConfigRegistryRequired          = "key registry is required"
	messageConfigRequestTimeoutNonNegative = "request timeout must not be negative"
	messageConfigStatusCacheRequired       = "status cache is required"
	messageConfigTransitRequired           = "transit adapter is required"
)

const maxConcurrentKMSRequests = 1024

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
	// ErrConcurrencyLimitExceeded identifies a request rejected by a KMS concurrency limit.
	ErrConcurrencyLimitExceeded = errors.New("kms request concurrency limit reached")
	// ErrPanicRecovered identifies a recovered panic inside a KMS v2 request handler.
	ErrPanicRecovered = errors.New("kms panic recovered")
	// ErrKeyMetadataRefresh identifies a failed unknown-key discovery attempt.
	ErrKeyMetadataRefresh = errors.New("key metadata refresh failed")
)

// StatusCache exposes the cached Status view maintained by the status workstream.
type StatusCache interface {
	Current(context.Context) (CachedStatus, error)
}

// KeyRefresher discovers validated snapshots without activating them for Encrypt.
type KeyRefresher interface {
	RefreshForDecrypt(context.Context, string) error
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
	StatusCache          StatusCache
	Registry             aad.SnapshotLookup
	KeyRefresher         KeyRefresher
	Transit              Transit
	PluginVersion        string
	RequestTimeout       time.Duration
	MaxConcurrentStatus  int
	MaxConcurrentEncrypt int
	MaxConcurrentDecrypt int
	Observer             Observer
}

// Server implements the Kubernetes KMS v2 service.
type Server struct {
	kmsapi.UnimplementedKeyManagementServiceServer

	statusCache    StatusCache
	registry       aad.SnapshotLookup
	keyRefresher   KeyRefresher
	transit        Transit
	pluginVersion  string
	requestTimeout time.Duration
	statusLimiter  *concurrencyLimiter
	encryptLimiter *concurrencyLimiter
	decryptLimiter *concurrencyLimiter
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
	if opts.MaxConcurrentStatus <= 0 || opts.MaxConcurrentStatus > maxConcurrentKMSRequests {
		return nil, fmt.Errorf("%w: %s", ErrConfigInvalid, messageConfigMaxConcurrentStatus)
	}
	if opts.MaxConcurrentEncrypt <= 0 || opts.MaxConcurrentEncrypt > maxConcurrentKMSRequests {
		return nil, fmt.Errorf("%w: %s", ErrConfigInvalid, messageConfigMaxConcurrentEncrypt)
	}
	if opts.MaxConcurrentDecrypt <= 0 || opts.MaxConcurrentDecrypt > maxConcurrentKMSRequests {
		return nil, fmt.Errorf("%w: %s", ErrConfigInvalid, messageConfigMaxConcurrentDecrypt)
	}

	return &Server{
		statusCache:    opts.StatusCache,
		registry:       opts.Registry,
		keyRefresher:   opts.KeyRefresher,
		transit:        opts.Transit,
		pluginVersion:  opts.PluginVersion,
		requestTimeout: opts.RequestTimeout,
		statusLimiter:  newConcurrencyLimiter(opts.MaxConcurrentStatus),
		encryptLimiter: newConcurrencyLimiter(opts.MaxConcurrentEncrypt),
		decryptLimiter: newConcurrencyLimiter(opts.MaxConcurrentDecrypt),
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
	defer s.finishRequest(ctx, &observation, start, &err)
	response, err = s.status(ctx, &observation)
	if response != nil {
		observation.Healthz = response.GetHealthz()
		if response.GetKeyId() != "" {
			observation.KeyIDHash = aad.HashValue(response.GetKeyId())
		}
	}
	return response, err
}

func (s *Server) status(ctx context.Context, observation *RequestObservation) (*kmsapi.StatusResponse, error) {
	if !s.statusLimiter.tryAcquire() {
		observation.ConcurrencyRejected = true
		return nil, ErrConcurrencyLimitExceeded
	}
	defer s.statusLimiter.release()

	requestCtx, cancel := s.requestContext(ctx)
	defer cancel()

	cached, err := s.statusCache.Current(requestCtx)
	if err != nil {
		if contextError(err) {
			return nil, err
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
	defer s.finishRequest(ctx, &observation, start, &err)
	return s.encrypt(ctx, request, &observation)
}

func (s *Server) encrypt(
	ctx context.Context,
	request *kmsapi.EncryptRequest,
	observation *RequestObservation,
) (*kmsapi.EncryptResponse, error) {
	if !s.encryptLimiter.tryAcquire() {
		observation.ConcurrencyRejected = true
		return nil, ErrConcurrencyLimitExceeded
	}
	defer s.encryptLimiter.release()

	if request == nil || len(request.GetPlaintext()) == 0 {
		return nil, ErrPlaintextRequired
	}

	requestCtx, cancel := s.requestContext(ctx)
	defer cancel()

	active, keyID, err := s.activeStatus(requestCtx)
	if err != nil {
		return nil, err
	}
	observation.KeyIDHash = aad.HashValue(keyID)
	observation.TransitKeyVersion = active.TransitVersion

	annotations, err := aad.BuildAnnotations(active, s.pluginVersion)
	if err != nil {
		return nil, err
	}
	canonicalAAD, err := aad.BuildCanonical(active, annotations)
	if err != nil {
		return nil, err
	}

	encrypted, err := s.transit.Encrypt(requestCtx, TransitEncryptRequest{
		Plaintext:      slices.Clone(request.GetPlaintext()),
		AssociatedData: canonicalAAD,
		KeyVersion:     active.TransitVersion,
	})
	if err != nil {
		return nil, &transitFailure{cause: err}
	}
	if len(encrypted.Ciphertext) == 0 {
		return nil, ErrTransitInvalidResponse
	}
	if encrypted.KeyVersion != 0 && encrypted.KeyVersion != active.TransitVersion {
		return nil, ErrTransitInvalidResponse
	}

	response := &kmsapi.EncryptResponse{
		Ciphertext:  slices.Clone(encrypted.Ciphertext),
		KeyId:       keyID,
		Annotations: annotationsToProto(annotations),
	}
	if err := validateEncryptResponseLimits(response); err != nil {
		return nil, err
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
	defer s.finishRequest(ctx, &observation, start, &err)
	return s.decrypt(ctx, request, &observation)
}

func (s *Server) decrypt(
	ctx context.Context,
	request *kmsapi.DecryptRequest,
	observation *RequestObservation,
) (*kmsapi.DecryptResponse, error) {
	if !s.decryptLimiter.tryAcquire() {
		observation.ConcurrencyRejected = true
		return nil, ErrConcurrencyLimitExceeded
	}
	defer s.decryptLimiter.release()

	if request == nil || len(request.GetCiphertext()) == 0 {
		return nil, ErrCiphertextRequired
	}
	if err := validateDecryptRequestLimits(request); err != nil {
		return nil, err
	}

	requestCtx, cancel := s.requestContext(ctx)
	defer cancel()

	annotations := annotationsFromValidatedProto(request.GetAnnotations())
	prepared, err := aad.PrepareDecrypt(s.registry, request.GetKeyId(), annotations)
	if errors.Is(err, keyregistry.ErrUnknownKeyID) && s.keyRefresher != nil {
		if refreshErr := s.keyRefresher.RefreshForDecrypt(requestCtx, request.GetKeyId()); refreshErr != nil {
			err = fmt.Errorf("%w: %w", ErrKeyMetadataRefresh, refreshErr)
		} else {
			prepared, err = aad.PrepareDecrypt(s.registry, request.GetKeyId(), annotations)
		}
	}
	if err != nil {
		s.observeValidationError(err)
		return nil, err
	}
	observation.KeyIDHash = aad.HashValue(prepared.Snapshot.KubernetesKeyID)
	observation.TransitKeyVersion = prepared.Snapshot.TransitVersion

	decrypted, err := s.transit.Decrypt(requestCtx, TransitDecryptRequest{
		Ciphertext:     slices.Clone(request.GetCiphertext()),
		AssociatedData: prepared.Canonical,
	})
	if err != nil {
		return nil, &transitFailure{cause: err}
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
	active, err := validateCachedStatus(cached)
	if err != nil {
		return keyregistry.KeySnapshot{}, "", err
	}
	return active, cached.KeyID, nil
}

// validateCachedStatus checks the shared healthy-status and Encrypt preconditions
// against one cached value. The caller chooses an unhealthy response or RPC error.
func validateCachedStatus(cached CachedStatus) (keyregistry.KeySnapshot, error) {
	if cached.Healthz != HealthOK {
		return keyregistry.KeySnapshot{}, ErrStatusUnhealthy
	}
	active, err := cached.Active.Normalize()
	if err != nil {
		return keyregistry.KeySnapshot{}, fmt.Errorf("%w: %v", ErrActiveKeyUnavailable, err)
	}
	if active.State != keyregistry.StateActive || cached.KeyID == "" {
		return keyregistry.KeySnapshot{}, ErrActiveKeyUnavailable
	}
	if cached.KeyID != active.KubernetesKeyID {
		return keyregistry.KeySnapshot{}, ErrStatusKeyIDMismatch
	}
	return active, nil
}

func statusResponse(cached CachedStatus) *kmsapi.StatusResponse {
	healthz := cached.Healthz
	keyID := cached.KeyID
	if healthz == "" {
		healthz = HealthUnhealthy
	}
	// Preserve custom unhealthy values; only healthy responses need snapshot validation.
	if healthz != HealthOK {
		keyID = ""
	}
	if healthz == HealthOK {
		if _, err := validateCachedStatus(cached); err != nil {
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

// annotationsFromValidatedProto converts annotations after
// validateDecryptRequestLimits checks their encoding and total size.
func annotationsFromValidatedProto(annotations map[string][]byte) map[string]string {
	decoded := make(map[string]string, len(annotations))
	for key, value := range annotations {
		decoded[key] = string(value)
	}
	return decoded
}
