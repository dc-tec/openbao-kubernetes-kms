// Package fakes contains reusable test doubles for cross-package validation.
package fakes

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/kmsv2"
)

const (
	kmsTransitCiphertextFormat = "vault:v%d:test-ciphertext-%d"
	kmsTransitPanicMessage     = "fake transit encrypt panic"
)

// KMSTransit is an in-memory Transit fake for KMS v2 tests.
type KMSTransit struct {
	mu           sync.Mutex
	records      map[string]KMSTransitRecord
	encryptCount int
	decryptCount int
	lastEnc      kmsv2.TransitEncryptRequest
	blockEncrypt bool
	blockDecrypt bool
	panicEncrypt bool
}

// KMSTransitRecord stores one fake ciphertext's decrypt material.
type KMSTransitRecord struct {
	Plaintext      []byte
	AssociatedData []byte
	KeyVersion     int
}

// NewKMSTransit builds an empty in-memory KMS Transit fake.
func NewKMSTransit() *KMSTransit {
	return &KMSTransit{records: make(map[string]KMSTransitRecord)}
}

// Encrypt records plaintext and AAD under a synthetic Transit ciphertext.
func (f *KMSTransit) Encrypt(
	ctx context.Context,
	request kmsv2.TransitEncryptRequest,
) (kmsv2.TransitEncryptResponse, error) {
	f.mu.Lock()
	if f.panicEncrypt {
		f.mu.Unlock()
		panic(kmsTransitPanicMessage)
	}
	f.encryptCount++
	f.lastEnc = cloneTransitEncryptRequest(request)
	block := f.blockEncrypt
	index := f.encryptCount
	f.mu.Unlock()

	if block {
		<-ctx.Done()
		return kmsv2.TransitEncryptResponse{}, ctx.Err()
	}

	ciphertext := []byte(fmt.Sprintf(kmsTransitCiphertextFormat, request.KeyVersion, index))
	f.mu.Lock()
	f.records[string(ciphertext)] = KMSTransitRecord{
		Plaintext:      bytes.Clone(request.Plaintext),
		AssociatedData: bytes.Clone(request.AssociatedData),
		KeyVersion:     request.KeyVersion,
	}
	f.mu.Unlock()

	return kmsv2.TransitEncryptResponse{
		Ciphertext: ciphertext,
		KeyVersion: request.KeyVersion,
	}, nil
}

// Decrypt returns the plaintext for a known fake ciphertext when AAD matches.
func (f *KMSTransit) Decrypt(
	ctx context.Context,
	request kmsv2.TransitDecryptRequest,
) (kmsv2.TransitDecryptResponse, error) {
	if err := ctx.Err(); err != nil {
		return kmsv2.TransitDecryptResponse{}, err
	}

	f.mu.Lock()
	f.decryptCount++
	block := f.blockDecrypt
	record, ok := f.records[string(request.Ciphertext)]
	f.mu.Unlock()

	if block {
		<-ctx.Done()
		return kmsv2.TransitDecryptResponse{}, ctx.Err()
	}
	if !ok {
		return kmsv2.TransitDecryptResponse{}, errors.New("unknown ciphertext")
	}
	if !bytes.Equal(record.AssociatedData, request.AssociatedData) {
		return kmsv2.TransitDecryptResponse{}, errors.New("message authentication failed")
	}
	return kmsv2.TransitDecryptResponse{Plaintext: bytes.Clone(record.Plaintext)}, nil
}

// EncryptCalls returns the number of fake encrypt calls.
func (f *KMSTransit) EncryptCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.encryptCount
}

// DecryptCalls returns the number of fake decrypt calls.
func (f *KMSTransit) DecryptCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.decryptCount
}

// LastEncrypt returns the last fake encrypt request.
func (f *KMSTransit) LastEncrypt() kmsv2.TransitEncryptRequest {
	f.mu.Lock()
	defer f.mu.Unlock()

	return cloneTransitEncryptRequest(f.lastEnc)
}

// SetBlockEncrypt controls whether Encrypt blocks until context cancellation.
func (f *KMSTransit) SetBlockEncrypt(block bool) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.blockEncrypt = block
}

// SetBlockDecrypt controls whether Decrypt blocks until context cancellation.
func (f *KMSTransit) SetBlockDecrypt(block bool) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.blockDecrypt = block
}

// SetPanicEncrypt controls whether Encrypt panics.
func (f *KMSTransit) SetPanicEncrypt(panicEnabled bool) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.panicEncrypt = panicEnabled
}

func cloneTransitEncryptRequest(request kmsv2.TransitEncryptRequest) kmsv2.TransitEncryptRequest {
	return kmsv2.TransitEncryptRequest{
		Plaintext:      bytes.Clone(request.Plaintext),
		AssociatedData: bytes.Clone(request.AssociatedData),
		KeyVersion:     request.KeyVersion,
	}
}

// StatusCache is a mutable fake KMS v2 status cache.
type StatusCache struct {
	mu      sync.Mutex
	current kmsv2.CachedStatus
	err     error
	count   int
}

// NewStatusCache builds a fake status cache with the supplied current value.
func NewStatusCache(current kmsv2.CachedStatus) *StatusCache {
	return &StatusCache{current: current}
}

// Current returns the configured fake status or error.
func (f *StatusCache) Current(context.Context) (kmsv2.CachedStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.count++
	return f.current, f.err
}

// Set replaces the fake status and clears any configured error.
func (f *StatusCache) Set(status kmsv2.CachedStatus) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.current = status
	f.err = nil
}

// SetError configures Current to return an error.
func (f *StatusCache) SetError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.err = err
}

// Calls returns the number of Current calls.
func (f *StatusCache) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.count
}
