//go:build !windows

package library

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// openRelativeNoFollow opens every path component relative to a directory
// descriptor. Each component is opened with O_NOFOLLOW, so a directory cannot
// be swapped for a symlink between validation and the final file open.
func openRelativeNoFollow(root, relative string) (*os.File, error) {
	if filepath.IsAbs(relative) || filepath.Clean(relative) != relative || filepath.VolumeName(relative) != "" {
		return nil, errorf("validation", "unsafe_path", "Use a path inside the library root.", "path escapes library root")
	}
	parts := strings.Split(relative, string(filepath.Separator))
	if len(parts) == 0 || relative == "." {
		return nil, errorf("validation", "unsafe_path", "Use a path inside the library root.", "empty relative path")
	}
	rootFD, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	currentFD := rootFD
	closeCurrent := true
	defer func() {
		if closeCurrent {
			_ = unix.Close(currentFD)
		}
	}()
	for index, part := range parts {
		if part == "" || part == "." || part == ".." {
			return nil, errorf("validation", "unsafe_path", "Use a path inside the library root.", "path contains an unsafe component")
		}
		flags := unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_CLOEXEC
		if index < len(parts)-1 {
			flags |= unix.O_DIRECTORY
		}
		nextFD, openErr := unix.Openat(currentFD, part, flags, 0)
		if openErr != nil {
			return nil, openErr
		}
		_ = unix.Close(currentFD)
		currentFD = nextFD
	}
	file := os.NewFile(uintptr(currentFD), filepath.Join(root, relative))
	if file == nil {
		_ = unix.Close(currentFD)
		return nil, errors.New("failed to create file from descriptor")
	}
	closeCurrent = false
	return file, nil
}
