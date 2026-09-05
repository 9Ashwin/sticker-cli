//go:build !windows

package library

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

func createRelativeTempDirectoryPlatform(root, relative, pattern string) (string, error) {
	parts := strings.Split(relative, string(filepath.Separator))
	if len(parts) == 0 || relative == "" {
		return "", errorf("validation", "unsafe_path", "Use a directory inside the library root.", "path is empty")
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", errorf("validation", "unsafe_path", "Use a directory inside the library root.", "path contains an unsafe component")
		}
	}
	parentFD, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return "", wrapError("validation", "unsafe_path", "Use a real library directory.", err)
	}
	defer func() { _ = unix.Close(parentFD) }()
	for _, part := range parts {
		nextFD, openErr := unix.Openat(parentFD, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if openErr != nil {
			return "", wrapError("validation", "unsafe_path", "Remove links from the library path.", openErr)
		}
		_ = unix.Close(parentFD)
		parentFD = nextFD
	}
	prefix := strings.TrimSuffix(pattern, "*")
	for attempt := 0; attempt < 100; attempt++ {
		candidate := fmt.Sprintf("%s%d-%d", prefix, time.Now().UnixNano(), attempt)
		if err := unix.Mkdirat(parentFD, candidate, 0o700); err == nil {
			return filepath.Join(root, relative, candidate), nil
		} else if !errors.Is(err, unix.EEXIST) {
			return "", wrapError("io", "write_failed", "Choose a writable library directory.", err)
		}
	}
	return "", errorf("io", "write_failed", "Retry the operation.", "could not create a unique temporary directory")
}
