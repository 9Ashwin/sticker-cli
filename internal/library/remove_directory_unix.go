//go:build !windows

package library

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

func removeRelativeDirectoryPlatform(ctx context.Context, root, relative string) error {
	parts := strings.Split(relative, string(filepath.Separator))
	if len(parts) == 0 {
		return errorf("validation", "unsafe_path", "Use a directory inside the library root.", "path is empty")
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return errorf("validation", "unsafe_path", "Use a directory inside the library root.", "path contains an unsafe component")
		}
	}
	rootFD, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return wrapError("validation", "unsafe_path", "Use a real library directory.", err)
	}
	currentFD := rootFD
	defer func() { _ = unix.Close(currentFD) }()
	for _, part := range parts[:len(parts)-1] {
		nextFD, openErr := unix.Openat(currentFD, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if errors.Is(openErr, unix.ENOENT) {
			return nil
		}
		if openErr != nil {
			return wrapError("validation", "unsafe_path", "Remove links from the temporary path.", openErr)
		}
		_ = unix.Close(currentFD)
		currentFD = nextFD
	}
	return removeRelativeEntry(ctx, currentFD, parts[len(parts)-1])
}

func removeRelativeEntry(ctx context.Context, parentFD int, name string) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	var info unix.Stat_t
	if err := unix.Fstatat(parentFD, name, &info, unix.AT_SYMLINK_NOFOLLOW); errors.Is(err, unix.ENOENT) {
		return nil
	} else if err != nil {
		return wrapError("io", "read_failed", "Check the temporary export path.", err)
	}
	if info.Mode&unix.S_IFMT != unix.S_IFDIR {
		if err := unix.Unlinkat(parentFD, name, 0); errors.Is(err, unix.ENOENT) {
			return nil
		} else if err != nil {
			return wrapError("io", "write_failed", "Remove the temporary export path.", err)
		}
		return nil
	}
	childFD, err := unix.Openat(parentFD, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) {
		if unlinkErr := unix.Unlinkat(parentFD, name, 0); errors.Is(unlinkErr, unix.ENOENT) {
			return nil
		} else if unlinkErr != nil {
			return wrapError("io", "write_failed", "Remove the temporary export path.", unlinkErr)
		}
		return nil
	}
	if err != nil {
		return wrapError("io", "read_failed", "Check the temporary export path.", err)
	}
	child := os.NewFile(uintptr(childFD), name)
	if child == nil {
		_ = unix.Close(childFD)
		return errorf("internal", "unexpected", "Retry the export cleanup.", "could not open temporary directory")
	}
	entries, readErr := child.Readdirnames(-1)
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		_ = child.Close()
		return wrapError("io", "read_failed", "Check the temporary export path.", readErr)
	}
	for _, entry := range entries {
		if err := removeRelativeEntry(ctx, childFD, entry); err != nil {
			_ = child.Close()
			return err
		}
	}
	_ = child.Close()
	if err := unix.Unlinkat(parentFD, name, unix.AT_REMOVEDIR); errors.Is(err, unix.ENOENT) {
		return nil
	} else if err != nil {
		return wrapError("io", "write_failed", "Remove the temporary export path.", err)
	}
	return nil
}
