//go:build !windows

package library

import (
	"errors"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

func ensureRelativeDirectoryPlatform(root, relative string) error {
	parts := strings.Split(relative, string(filepath.Separator))
	if len(parts) == 0 || relative == "" {
		return errorf("validation", "unsafe_path", "Use a directory inside the library root.", "path is empty")
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return errorf("validation", "unsafe_path", "Use a directory inside the library root.", "path contains an unsafe component")
		}
	}
	rootFD, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return wrapError("validation", "unsafe_path", "Use a real library directory.", err)
	}
	currentFD := rootFD
	defer func() { _ = unix.Close(currentFD) }()
	for _, part := range parts {
		nextFD, openErr := unix.Openat(currentFD, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if errors.Is(openErr, unix.ENOENT) {
			if mkdirErr := unix.Mkdirat(currentFD, part, 0o700); mkdirErr != nil && !errors.Is(mkdirErr, unix.EEXIST) {
				return wrapError("io", "write_failed", "Choose a writable library directory.", mkdirErr)
			}
			nextFD, openErr = unix.Openat(currentFD, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		}
		if openErr != nil {
			return wrapError("validation", "unsafe_path", "Remove links from the library path.", openErr)
		}
		_ = unix.Close(currentFD)
		currentFD = nextFD
	}
	return nil
}
