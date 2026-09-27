package oauth2

import (
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ReadClientSecret reads one bounded, single-line secret from a private regular file.
// A trailing newline is accepted. Other whitespace is preserved as credential data.
func ReadClientSecret(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", ErrCredential
	}
	before, err := os.Lstat(path)
	if err != nil || !safeCredentialFile(before) {
		return "", ErrCredential
	}
	// #nosec G304 -- the credential path is administrator-supplied and checked before and after opening.
	file, err := os.Open(path)
	if err != nil {
		return "", ErrCredential
	}
	defer func() { _ = file.Close() }()
	opened, err := file.Stat()
	if err != nil || !safeCredentialFile(opened) || !os.SameFile(before, opened) {
		return "", ErrCredential
	}
	current, err := os.Lstat(path)
	if err != nil || !safeCredentialFile(current) || !os.SameFile(opened, current) {
		return "", ErrCredential
	}
	content, err := io.ReadAll(io.LimitReader(file, maxSecretBytes+1))
	if err != nil || len(content) > maxSecretBytes {
		return "", ErrCredential
	}
	secret := strings.TrimSuffix(strings.TrimSuffix(string(content), "\n"), "\r")
	if secret == "" || strings.ContainsAny(secret, "\x00\r\n") {
		return "", ErrCredential
	}
	return secret, nil
}

func safeCredentialFile(info os.FileInfo) bool {
	return info.Mode().IsRegular() && info.Mode().Perm()&0o137 == 0
}
