//go:build certauth_pkcs11

package auth

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReadPKCS11PINFileRejectsUnsafeMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pin")
	if err := os.WriteFile(path, []byte("1234\n"), 0o644); err != nil {
		t.Fatalf("write pin fixture: %v", err)
	}

	_, err := readPKCS11PINFile(path)
	if !errors.Is(err, ErrPKCS11PINRead) {
		t.Fatalf("expected unsafe PIN file mode, got %v", err)
	}
}

func TestReadPKCS11PINFileRejectsSymlink(t *testing.T) {
	tempDir := t.TempDir()
	target := filepath.Join(tempDir, "pin-target")
	if err := os.WriteFile(target, []byte("1234\n"), 0o600); err != nil {
		t.Fatalf("write pin fixture: %v", err)
	}
	link := filepath.Join(tempDir, "pin")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink pin fixture: %v", err)
	}

	_, err := readPKCS11PINFile(link)
	if !errors.Is(err, ErrPKCS11PINRead) {
		t.Fatalf("expected symlink PIN file failure, got %v", err)
	}
}
