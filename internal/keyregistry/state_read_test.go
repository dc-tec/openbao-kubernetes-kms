package keyregistry

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestStateReadersRejectUnsafeFiles(t *testing.T) {
	readers := map[string]func(string) error{
		"state": func(path string) error {
			_, _, err := LoadStateFile(path, StateLoadOptions{})
			return err
		},
		"checkpoint": func(path string) error {
			_, err := LoadStateCheckpoint(path)
			return err
		},
	}
	for name, read := range readers {
		t.Run(name, func(t *testing.T) {
			for _, kind := range []string{"symlink", "fifo", "directory", "permissions", "parent-symlink"} {
				t.Run(kind, func(t *testing.T) {
					path := unsafeStateReadFixture(t, kind)
					if err := read(path); !errors.Is(err, ErrStatePermission) {
						t.Fatalf("expected unsafe file rejection, got %v", err)
					}
				})
			}
		})
	}
}

func unsafeStateReadFixture(t *testing.T, kind string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	var err error
	switch kind {
	case "symlink":
		err = os.Symlink("/dev/null", path)
	case "fifo":
		err = unix.Mkfifo(path, 0o600)
	case "directory":
		err = os.Mkdir(path, 0o700)
	case "permissions":
		err = os.WriteFile(path, []byte("{}"), 0o600)
		if err == nil {
			// #nosec G302 -- exercise rejection of group-writable state.
			err = os.Chmod(path, 0o660)
		}
	case "parent-symlink":
		link := filepath.Join(t.TempDir(), "linked-parent")
		err = os.Symlink(dir, link)
		path = filepath.Join(link, "state.json")
	}
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestStateReadPinsParentAndValidatesOpenedFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "parent")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "state.json")
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	parent, err := unix.Open(dir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = unix.Close(parent) }()
	if err := os.Rename(dir, dir+"-moved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := openStateFileAt(parent, "state.json")
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(file)
	_ = file.Close()
	if err != nil || string(data) != "original" {
		t.Fatalf("read followed replacement parent: %q, %v", data, err)
	}
	// A replacement inside the pinned directory must still pass descriptor validation.
	original := filepath.Join(dir+"-moved", "state.json")
	if err := os.Remove(original); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path, original); err != nil {
		t.Fatal(err)
	}
	if file, err := openStateFileAt(parent, "state.json"); !errors.Is(err, ErrStatePermission) {
		if file != nil {
			_ = file.Close()
		}
		t.Fatalf("expected substituted symlink rejection, got %v", err)
	}
}
