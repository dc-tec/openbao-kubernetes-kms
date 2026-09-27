package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestVerifySecretAbsent(t *testing.T) {
	for _, tc := range []struct {
		name, script string
		wantError    bool
	}{
		{"absent", "exit 0", false},
		{"present", "echo secret/post-restore", true},
		{"forbidden", "echo Forbidden >&2; exit 1", true},
		{"unavailable", "echo connection refused >&2; exit 1", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			script := "#!/bin/sh\n" + tc.script + "\n"
			// #nosec G306 -- executable kubectl stub in an isolated test directory.
			if err := os.WriteFile(filepath.Join(dir, "kubectl"), []byte(script), 0o700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			err := verifySecretAbsent(context.Background(), &labConfig{root: dir}, "test-config", "post-restore")
			if (err != nil) != tc.wantError {
				t.Fatalf("verifySecretAbsent error = %v, wantError %v", err, tc.wantError)
			}
		})
	}
}
