package keyregistry

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
)

// ValidateIdentityFingerprint checks the opaque configuration identity binding.
// Its contents are produced by config.IdentityFingerprint; raw backend paths
// are never stored in the registry or added to the KMS wire contract.
func ValidateIdentityFingerprint(value string) error {
	const prefix = "cfg1."
	if !strings.HasPrefix(value, prefix) || len(value) != len(prefix)+sha256RawURLLength {
		return fmt.Errorf("%w: configuration identity fingerprint required", ErrStateCorrupt)
	}
	encoded := strings.TrimPrefix(value, prefix)
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil || len(decoded) != sha256.Size {
		return fmt.Errorf("%w: invalid configuration identity fingerprint", ErrStateCorrupt)
	}
	return nil
}
