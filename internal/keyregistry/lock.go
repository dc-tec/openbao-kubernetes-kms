package keyregistry

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// ErrStateLocked identifies a state directory already in use by a writer.
var ErrStateLocked = errors.New("key registry state is locked by another process")

// LockState excludes concurrent serve and retirement writers. Close the returned
// file to release the lock. The lock file must remain in place after release.
func LockState(path string) (*os.File, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("%w: state path must be absolute", ErrStatePermission)
	}
	parent, err := unix.Open(filepath.Dir(path), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open state directory for locking: %w", err)
	}
	defer func() { _ = unix.Close(parent) }()
	var dirStat unix.Stat_t
	if err := unix.Fstat(parent, &dirStat); err != nil {
		return nil, fmt.Errorf("inspect state directory: %w", err)
	}
	if int64(dirStat.Uid) != int64(os.Geteuid()) || dirStat.Mode&0o022 != 0 {
		return nil, fmt.Errorf("%w: state directory must be owned by the current user and not group/world writable",
			ErrStatePermission)
	}
	lockName := filepath.Base(path) + ".lock"
	fd, err := unix.Openat(parent, lockName, unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open state lock: %w", err)
	}
	// #nosec G115 -- successful Openat returns a non-negative file descriptor.
	file := os.NewFile(uintptr(fd), filepath.Join(filepath.Dir(path), lockName))
	if err := validateAndLockStateFile(fd); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func validateAndLockStateFile(fd int) error {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return fmt.Errorf("inspect state lock: %w", err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Mode&0o077 != 0 ||
		int64(stat.Uid) != int64(os.Geteuid()) || stat.Nlink != 1 {
		return fmt.Errorf("%w: lock must be a private regular file owned by the current user with one link",
			ErrStatePermission)
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return ErrStateLocked
		}
		return fmt.Errorf("lock registry state: %w", err)
	}
	return nil
}
