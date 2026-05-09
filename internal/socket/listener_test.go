package socket_test

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dc-tec/openbao-kubernetes-kms/internal/socket"
)

const (
	socketBaseName       = "kms.sock"
	parentRestrictedMode = os.FileMode(0o750)
	parentLooseMode      = os.FileMode(0o777)
	socketSafeMode       = os.FileMode(0o660)
)

func TestListenSucceedsAndAppliesMode(t *testing.T) {
	parent := newRestrictedDir(t)
	socketPath := filepath.Join(parent, socketBaseName)

	listener := mustListen(t, socket.Options{
		Path: socketPath,
		Mode: socketSafeMode,
		GID:  -1,
	})
	t.Cleanup(func() { _ = listener.Close() })

	if listener.Path() != socketPath {
		t.Fatalf("unexpected path: %q", listener.Path())
	}
	info, err := os.Stat(socketPath)
	if err != nil {
		t.Fatalf("stat socket: %v", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		t.Fatal("created file is not a Unix socket")
	}
	if perm := info.Mode().Perm(); perm != socketSafeMode {
		t.Fatalf("unexpected socket mode: want %#o got %#o", socketSafeMode, perm)
	}
}

func TestListenAppliesGroupOwnership(t *testing.T) {
	parent := newRestrictedDir(t)
	socketPath := filepath.Join(parent, socketBaseName)

	listener := mustListen(t, socket.Options{
		Path: socketPath,
		Mode: socketSafeMode,
		GID:  os.Getgid(),
	})
	t.Cleanup(func() { _ = listener.Close() })

	info, err := os.Stat(socketPath)
	if err != nil {
		t.Fatalf("stat socket: %v", err)
	}
	statT, ok := sysStat(info)
	if !ok {
		t.Skip("filesystem does not expose unix stat info")
	}
	wantGID := os.Getgid()
	if wantGID < 0 || statT.gid != uint32(wantGID) { //nolint:gosec // GID is non-negative on supported unix platforms
		t.Fatalf("unexpected socket gid: want %d got %d", wantGID, statT.gid)
	}
}

func TestListenRejectsRelativePath(t *testing.T) {
	_, err := socket.Listen(socket.Options{Path: "kms.sock", Mode: socketSafeMode, GID: -1})
	if !errors.Is(err, socket.ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig, got %v", err)
	}
}

func TestListenRejectsUnsafeMode(t *testing.T) {
	parent := newRestrictedDir(t)
	socketPath := filepath.Join(parent, socketBaseName)

	cases := []struct {
		name string
		mode os.FileMode
	}{
		{"world-readable", os.FileMode(0o664)},
		{"group-execute", os.FileMode(0o770)},
		{"non-permission-bits", os.ModeSetuid | 0o660},
		{"zero", os.FileMode(0)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := socket.Listen(socket.Options{Path: socketPath, Mode: tc.mode, GID: -1})
			if !errors.Is(err, socket.ErrInvalidConfig) {
				t.Fatalf("want ErrInvalidConfig, got %v", err)
			}
		})
	}
}

func TestListenRejectsMissingParent(t *testing.T) {
	socketPath := filepath.Join(shortTempDir(t), "missing-parent", socketBaseName)
	_, err := socket.Listen(socket.Options{Path: socketPath, Mode: socketSafeMode, GID: -1})
	if !errors.Is(err, socket.ErrUnsafeParent) {
		t.Fatalf("want ErrUnsafeParent, got %v", err)
	}
}

func TestListenRejectsWritableParent(t *testing.T) {
	cases := []struct {
		name string
		mode os.FileMode
	}{
		{name: "group-writable", mode: 0o770},
		{name: "world-writable", mode: parentLooseMode},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parent := shortTempDir(t)
			if err := os.Chmod(parent, tc.mode); err != nil {
				t.Fatalf("chmod parent: %v", err)
			}
			socketPath := filepath.Join(parent, socketBaseName)
			_, err := socket.Listen(socket.Options{Path: socketPath, Mode: socketSafeMode, GID: -1})
			if !errors.Is(err, socket.ErrUnsafeParent) {
				t.Fatalf("want ErrUnsafeParent, got %v", err)
			}
		})
	}
}

func TestListenRejectsSymlinkedParent(t *testing.T) {
	root := shortTempDir(t)
	realParent := filepath.Join(root, "real")
	if err := os.MkdirAll(realParent, parentRestrictedMode); err != nil {
		t.Fatalf("mkdir real parent: %v", err)
	}
	linkParent := filepath.Join(root, "link")
	if err := os.Symlink(realParent, linkParent); err != nil {
		t.Fatalf("symlink parent: %v", err)
	}
	socketPath := filepath.Join(linkParent, socketBaseName)
	_, err := socket.Listen(socket.Options{Path: socketPath, Mode: socketSafeMode, GID: -1})
	if !errors.Is(err, socket.ErrUnsafeParent) {
		t.Fatalf("want ErrUnsafeParent, got %v", err)
	}
}

func TestListenRejectsSymlinkTarget(t *testing.T) {
	parent := newRestrictedDir(t)
	socketPath := filepath.Join(parent, socketBaseName)
	if err := os.Symlink("/dev/null", socketPath); err != nil {
		t.Fatalf("symlink target: %v", err)
	}
	_, err := socket.Listen(socket.Options{Path: socketPath, Mode: socketSafeMode, GID: -1})
	if !errors.Is(err, socket.ErrInvalidSocketTarget) {
		t.Fatalf("want ErrInvalidSocketTarget, got %v", err)
	}
}

func TestListenRejectsRegularFileTarget(t *testing.T) {
	parent := newRestrictedDir(t)
	socketPath := filepath.Join(parent, socketBaseName)
	if err := os.WriteFile(socketPath, []byte("not-a-socket"), 0o600); err != nil {
		t.Fatalf("write file target: %v", err)
	}
	_, err := socket.Listen(socket.Options{Path: socketPath, Mode: socketSafeMode, GID: -1})
	if !errors.Is(err, socket.ErrInvalidSocketTarget) {
		t.Fatalf("want ErrInvalidSocketTarget, got %v", err)
	}
}

func TestListenRejectsDirectoryTarget(t *testing.T) {
	parent := newRestrictedDir(t)
	socketPath := filepath.Join(parent, socketBaseName)
	if err := os.Mkdir(socketPath, parentRestrictedMode); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	_, err := socket.Listen(socket.Options{Path: socketPath, Mode: socketSafeMode, GID: -1})
	if !errors.Is(err, socket.ErrInvalidSocketTarget) {
		t.Fatalf("want ErrInvalidSocketTarget, got %v", err)
	}
}

func TestListenRejectsLiveSocketCollision(t *testing.T) {
	parent := newRestrictedDir(t)
	socketPath := filepath.Join(parent, socketBaseName)

	first, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("first listen: %v", err)
	}
	t.Cleanup(func() { _ = first.Close() })

	_, err = socket.Listen(socket.Options{
		Path:        socketPath,
		Mode:        socketSafeMode,
		GID:         -1,
		DialTimeout: 200 * time.Millisecond,
	})
	if !errors.Is(err, socket.ErrLiveSocketCollision) {
		t.Fatalf("want ErrLiveSocketCollision, got %v", err)
	}
}

func TestListenReclaimsStaleDeadSocket(t *testing.T) {
	parent := newRestrictedDir(t)
	socketPath := filepath.Join(parent, socketBaseName)

	stale, err := net.ListenUnix("unix", &net.UnixAddr{Name: socketPath, Net: "unix"})
	if err != nil {
		t.Fatalf("stale listen: %v", err)
	}
	stale.SetUnlinkOnClose(false)
	if err := stale.Close(); err != nil {
		t.Fatalf("close stale listener: %v", err)
	}
	if _, err := os.Stat(socketPath); err != nil {
		t.Fatalf("expected stale socket file to remain: %v", err)
	}

	var staleHits atomic.Int64
	listener := mustListen(t, socket.Options{
		Path:                 socketPath,
		Mode:                 socketSafeMode,
		GID:                  -1,
		DialTimeout:          200 * time.Millisecond,
		OnStaleSocketRemoved: func() { staleHits.Add(1) },
	})
	t.Cleanup(func() { _ = listener.Close() })

	if got := staleHits.Load(); got != 1 {
		t.Fatalf("expected 1 stale-socket callback, got %d", got)
	}
	info, err := os.Stat(socketPath)
	if err != nil {
		t.Fatalf("stat reclaimed socket: %v", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		t.Fatal("reclaimed path is not a socket")
	}
}

func TestListenerCloseUnlinksSocketFile(t *testing.T) {
	parent := newRestrictedDir(t)
	socketPath := filepath.Join(parent, socketBaseName)

	listener := mustListen(t, socket.Options{Path: socketPath, Mode: socketSafeMode, GID: -1})
	if err := listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}
	if _, err := os.Stat(socketPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected socket file removed after Close, stat err=%v", err)
	}
}

// shortTempDir returns a per-test directory under a short root.
//
// Unix domain socket paths are bounded by the kernel: macOS allows ~104 bytes
// and Linux allows ~108. testing.T.TempDir on macOS produces paths inside
// /var/folders that already exceed 104 bytes once a socket basename is appended,
// so tests build paths under /tmp instead and clean them up explicitly.
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "kmsskt-")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func newRestrictedDir(t *testing.T) string {
	t.Helper()
	dir := shortTempDir(t)
	if err := os.Chmod(dir, parentRestrictedMode); err != nil {
		t.Fatalf("chmod parent: %v", err)
	}
	return dir
}

func mustListen(t *testing.T, opts socket.Options) *socket.Listener {
	t.Helper()
	listener, err := socket.Listen(opts)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	return listener
}
