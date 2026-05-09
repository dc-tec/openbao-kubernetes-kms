// Package socket creates safe Unix domain sockets for the KMS provider.
//
// The package validates the parent directory, rejects unsafe socket targets
// (symlink, regular file, directory), refuses to take over a live peer, and
// reclaims a verified-dead stale socket exactly once before binding.
package socket

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// disallowedMode mirrors the configuration policy: no world bits and no execute bits.
//
// 0o117 covers owner execute, group execute, and any world access.
const disallowedMode = os.FileMode(0o117)

// defaultDialTimeout bounds the live-peer probe before binding.
const defaultDialTimeout = 250 * time.Millisecond

var (
	// ErrInvalidConfig identifies invalid Listen options.
	ErrInvalidConfig = errors.New("socket config invalid")
	// ErrUnsafeParent identifies an unsafe socket parent directory.
	ErrUnsafeParent = errors.New("socket parent directory unsafe")
	// ErrInvalidSocketTarget identifies a socket path with the wrong file type.
	ErrInvalidSocketTarget = errors.New("socket path target invalid")
	// ErrLiveSocketCollision identifies a path that already has a live peer.
	ErrLiveSocketCollision = errors.New("socket path is in use by a live peer")
	// ErrIndeterminateSocketState identifies a socket whose state cannot be
	// classified as definitively dead (for example because the dial probe
	// returned permission-denied or timed out). The package fails closed in
	// these cases and refuses to remove the file.
	ErrIndeterminateSocketState = errors.New("socket path liveness indeterminate")
)

// Options controls Listen behaviour.
type Options struct {
	// Path is the absolute Unix socket path to bind.
	Path string
	// Mode is the permission mode applied to the bound socket.
	Mode os.FileMode
	// GID is the group ID applied to the bound socket. A negative value skips chown.
	GID int
	// DialTimeout bounds the live-peer probe; zero applies a sensible default.
	DialTimeout time.Duration
	// OnStaleSocketRemoved fires once after a verified-dead socket is reclaimed.
	// WS09 wires the openbao_kms_socket_restarts_total counter through this hook.
	OnStaleSocketRemoved func()
}

// Listener is a Unix domain listener bound through Listen.
//
// Close stops accepting and unlinks the socket file (the standard
// net.UnixListener behaviour).
type Listener struct {
	net.Listener
	path string
}

// Path returns the socket path the listener is bound to.
func (l *Listener) Path() string {
	return l.path
}

// Listen binds a Unix domain listener after validating the parent directory and
// the existing socket target. Stale dead sockets are reclaimed; live peers are
// rejected. The bound socket is chmodded to opts.Mode and chowned to opts.GID
// when opts.GID is non-negative.
func Listen(opts Options) (*Listener, error) {
	if err := validateOptions(opts); err != nil {
		return nil, err
	}
	if err := validateParent(opts.Path); err != nil {
		return nil, err
	}
	if err := prepareTarget(opts); err != nil {
		return nil, err
	}

	listener, err := net.Listen("unix", opts.Path)
	if err != nil {
		return nil, fmt.Errorf("listen unix socket: %w", err)
	}
	if err := os.Chmod(opts.Path, opts.Mode); err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("chmod unix socket: %w", err)
	}
	if opts.GID >= 0 {
		if err := os.Chown(opts.Path, -1, opts.GID); err != nil {
			_ = listener.Close()
			return nil, fmt.Errorf("chown unix socket: %w", err)
		}
	}
	return &Listener{Listener: listener, path: opts.Path}, nil
}

func validateOptions(opts Options) error {
	if !filepath.IsAbs(opts.Path) {
		return fmt.Errorf("%w: path must be absolute", ErrInvalidConfig)
	}
	if opts.Mode == 0 {
		return fmt.Errorf("%w: mode must be set", ErrInvalidConfig)
	}
	if opts.Mode&^os.ModePerm != 0 {
		return fmt.Errorf("%w: mode must be a permission mode", ErrInvalidConfig)
	}
	if opts.Mode&disallowedMode != 0 {
		return fmt.Errorf("%w: mode must not allow world or execute bits", ErrInvalidConfig)
	}
	return nil
}

func validateParent(socketPath string) error {
	parent := filepath.Dir(socketPath)
	info, err := os.Lstat(parent)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnsafeParent, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: parent is a symlink", ErrUnsafeParent)
	}
	if !info.IsDir() {
		return fmt.Errorf("%w: parent is not a directory", ErrUnsafeParent)
	}
	if info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("%w: parent must not be group-writable or world-writable", ErrUnsafeParent)
	}
	return nil
}

func prepareTarget(opts Options) error {
	info, err := os.Lstat(opts.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("%w: %v", ErrInvalidSocketTarget, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: existing path is a symlink", ErrInvalidSocketTarget)
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("%w: existing path is not a Unix socket", ErrInvalidSocketTarget)
	}

	switch probeSocket(opts.Path, opts.DialTimeout) {
	case probeLive:
		return fmt.Errorf("%w: %s", ErrLiveSocketCollision, opts.Path)
	case probeUncertain:
		return fmt.Errorf("%w: %s", ErrIndeterminateSocketState, opts.Path)
	}

	if err := os.Remove(opts.Path); err != nil {
		return fmt.Errorf("remove stale socket: %w", err)
	}
	if opts.OnStaleSocketRemoved != nil {
		opts.OnStaleSocketRemoved()
	}
	return nil
}

// probeResult classifies a dial probe of an existing socket path.
type probeResult int

const (
	// probeDead means the dial returned ECONNREFUSED: the socket file is
	// orphaned with no listener attached. Safe to remove.
	probeDead probeResult = iota
	// probeLive means the dial succeeded: a peer is listening. Fail closed.
	probeLive
	// probeUncertain means the dial failed with anything other than
	// ECONNREFUSED (permission denied, timeout, transient errors). The probe
	// cannot prove the socket is dead, so the package fails closed.
	probeUncertain
)

func probeSocket(path string, timeout time.Duration) probeResult {
	if timeout <= 0 {
		timeout = defaultDialTimeout
	}
	conn, err := net.DialTimeout("unix", path, timeout)
	if err == nil {
		_ = conn.Close()
		return probeLive
	}
	return classifyDialErr(err)
}

func classifyDialErr(err error) probeResult {
	if errors.Is(err, syscall.ECONNREFUSED) {
		return probeDead
	}
	return probeUncertain
}
