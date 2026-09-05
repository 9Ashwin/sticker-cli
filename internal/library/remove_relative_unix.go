//go:build !windows

package library

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// removeRelativePlatform anchors every component to a directory descriptor.
// Unlinkat removes the final name atomically and never follows a symlink.
func removeRelativePlatform(ctx context.Context, root, relative, _ string) (bool, error) {
	if err := contextErr(ctx); err != nil {
		return false, err
	}
	if filepath.IsAbs(relative) || filepath.Clean(relative) != relative || filepath.VolumeName(relative) != "" {
		return false, errorf("validation", "unsafe_path", "Use a path inside the library root.", "path escapes library root")
	}
	parts := strings.Split(relative, string(filepath.Separator))
	if len(parts) == 0 || relative == "." {
		return false, errorf("validation", "unsafe_path", "Use a file inside the library root.", "relative file path is invalid")
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return false, errorf("validation", "unsafe_path", "Use a path inside the library root.", "path contains an unsafe component")
		}
	}

	rootFD, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if errors.Is(err, unix.ENOENT) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	currentFD := rootFD
	defer func() { _ = unix.Close(currentFD) }()
	for _, part := range parts[:len(parts)-1] {
		nextFD, openErr := unix.Openat(currentFD, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if errors.Is(openErr, unix.ENOENT) {
			return false, nil
		}
		if openErr != nil {
			if errors.Is(openErr, unix.ELOOP) {
				return false, errorf("validation", "unsafe_path", "Remove links from the library path.", "path component is a symbolic link")
			}
			return false, openErr
		}
		_ = unix.Close(currentFD)
		currentFD = nextFD
	}

	name := parts[len(parts)-1]
	var stat unix.Stat_t
	if err := unix.Fstatat(currentFD, name, &stat, unix.AT_SYMLINK_NOFOLLOW); errors.Is(err, unix.ENOENT) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	if stat.Mode&unix.S_IFMT == unix.S_IFLNK {
		return false, errorf("validation", "unsafe_path", "Remove links from the library path.", "target is a symbolic link")
	}
	if err := unix.Unlinkat(currentFD, name, 0); errors.Is(err, unix.ENOENT) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return true, nil
}
