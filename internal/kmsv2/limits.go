package kmsv2

import (
	"fmt"
	"unicode/utf8"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/aad"
	kmsapi "k8s.io/kms/apis/v2"
)

const (
	// MaxKMSCiphertextBytes is the exclusive Kubernetes KMS v2 ciphertext limit.
	MaxKMSCiphertextBytes = 1024
	// MaxKMSKeyIDBytes is the exclusive Kubernetes KMS v2 key_id limit.
	MaxKMSKeyIDBytes = 1024
	// MaxKMSAnnotationBytes is the exclusive Kubernetes KMS v2 annotation map limit.
	MaxKMSAnnotationBytes = 32 * 1024
	// MaxGRPCMessageBytes keeps the protobuf envelope bounded while allowing the KMS field limits.
	MaxGRPCMessageBytes = 64 * 1024
)

func validateDecryptRequestLimits(request *kmsapi.DecryptRequest) error {
	if len(request.GetCiphertext()) >= MaxKMSCiphertextBytes {
		return fmt.Errorf("%w: ciphertext exceeds %d bytes", ErrRequestLimitExceeded, MaxKMSCiphertextBytes-1)
	}
	if len(request.GetKeyId()) >= MaxKMSKeyIDBytes {
		return fmt.Errorf("%w: key_id exceeds %d bytes", ErrRequestLimitExceeded, MaxKMSKeyIDBytes-1)
	}
	if err := validateAnnotationsProtoLimits(request.GetAnnotations(), ErrRequestLimitExceeded); err != nil {
		return err
	}
	return nil
}

func validateEncryptResponseLimits(response *kmsapi.EncryptResponse) error {
	if response == nil {
		return fmt.Errorf("%w: nil encrypt response", ErrResponseLimitExceeded)
	}
	if len(response.GetCiphertext()) >= MaxKMSCiphertextBytes {
		return fmt.Errorf("%w: ciphertext exceeds %d bytes", ErrResponseLimitExceeded, MaxKMSCiphertextBytes-1)
	}
	if response.GetKeyId() == "" || len(response.GetKeyId()) >= MaxKMSKeyIDBytes {
		return fmt.Errorf("%w: key_id exceeds %d bytes", ErrResponseLimitExceeded, MaxKMSKeyIDBytes-1)
	}
	if err := validateAnnotationsProtoLimits(response.GetAnnotations(), ErrResponseLimitExceeded); err != nil {
		return err
	}
	return nil
}

func validateAnnotationsProtoLimits(annotations map[string][]byte, limitErr error) error {
	total := 0
	for key, value := range annotations {
		if !utf8.ValidString(key) || !utf8.Valid(value) {
			return fmt.Errorf("%w: %s", aad.ErrInvalidAnnotations, messageAnnotationEncodingInvalid)
		}
		total += len(key) + len(value)
		if total >= MaxKMSAnnotationBytes {
			return fmt.Errorf("%w: annotations exceed %d bytes", limitErr, MaxKMSAnnotationBytes-1)
		}
	}
	return nil
}
