package keyregistry

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// Pin the immediate parent before opening either persistence file. Ancestor
// directories must remain under trusted host administration.
func openStateFile(path string) (*os.File, error) {
	parent, err := unix.Open(filepath.Dir(path), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrStateNotFound
		}
		return nil, fmt.Errorf("%w: open state parent: %w", ErrStatePermission, err)
	}
	defer func() { _ = unix.Close(parent) }()
	return openStateFileAt(parent, filepath.Base(path))
}

func openStateFileAt(parent int, name string) (*os.File, error) {
	var dirStat unix.Stat_t
	if err := unix.Fstat(parent, &dirStat); err != nil {
		return nil, fmt.Errorf("inspect state parent: %w", err)
	}
	if dirStat.Mode&unix.S_IFMT != unix.S_IFDIR || dirStat.Mode&0o022 != 0 {
		return nil, fmt.Errorf("%w: parent must be a directory without group/world write", ErrStatePermission)
	}
	// Nonblocking open prevents a substituted FIFO from hanging before validation.
	fd, err := unix.Openat(parent, name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrStateNotFound
		}
		return nil, fmt.Errorf("%w: open state file: %w", ErrStatePermission, err)
	}
	// #nosec G115 -- successful Openat returns a non-negative file descriptor.
	file := os.NewFile(uintptr(fd), name)
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("inspect opened state file: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&stateFileDisallowedMode != 0 {
		_ = file.Close()
		return nil, fmt.Errorf("%w: state file must be regular without mode bits %04o",
			ErrStatePermission, stateFileDisallowedMode)
	}
	return file, nil
}
