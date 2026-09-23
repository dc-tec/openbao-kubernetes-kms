package keyregistry_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/keyregistry"
)

func TestStateLockExcludesWritersAndSurvivesRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.json")
	first, err := keyregistry.LockState(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Close() })
	if second, err := keyregistry.LockState(path); !errors.Is(err, keyregistry.ErrStateLocked) {
		if second != nil {
			_ = second.Close()
		}
		t.Fatalf("second writer acquired lock: %v", err)
	}
	before, err := os.Stat(path + ".lock")
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := keyregistry.LockState(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = second.Close() }()
	after, err := os.Stat(path + ".lock")
	if err != nil || !os.SameFile(before, after) {
		t.Fatalf("lock file must keep its inode: %v", err)
	}
}

func TestStateLockRejectsUnsafePaths(t *testing.T) {
	for _, scenario := range []string{
		"symlink-lock", "symlink-directory", "writable-directory", "public-lock", "hardlink-lock",
	} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "registry.json")
			lock, err := keyregistry.LockState(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := lock.Close(); err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "symlink-lock":
				path = filepath.Join(dir, "other.json")
				err = os.Symlink(filepath.Join(dir, "registry.json.lock"), path+".lock")
			case "symlink-directory":
				link := filepath.Join(t.TempDir(), "link")
				err = os.Symlink(dir, link)
				path = filepath.Join(link, "registry.json")
			case "writable-directory":
				// #nosec G302 -- deliberately unsafe permissions exercise rejection.
				err = os.Chmod(dir, 0o770)
			case "public-lock":
				// #nosec G302 -- deliberately unsafe permissions exercise rejection.
				err = os.Chmod(path+".lock", 0o644)
			case "hardlink-lock":
				err = os.Link(path+".lock", filepath.Join(dir, "alias"))
			}
			if err != nil {
				t.Fatal(err)
			}
			if got, err := keyregistry.LockState(path); err == nil {
				_ = got.Close()
				t.Fatal("unsafe lock path accepted")
			}
		})
	}
}
