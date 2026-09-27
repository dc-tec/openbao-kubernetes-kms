//go:build certauth_pkcs11

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestChownRecursiveDoesNotFollowSymlinks(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "external")
	// Following this link would fail: its target is absent outside the fixture.
	if err := os.Symlink(filepath.Join(t.TempDir(), "absent"), link); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{dir, link} {
		if err := chownRecursive(path, os.Geteuid(), os.Getegid()); err != nil {
			t.Fatalf("change fixture ownership without following links: %v", err)
		}
	}
	info, err := os.Lstat(link)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("fixture link was replaced: %v", err)
	}
}
