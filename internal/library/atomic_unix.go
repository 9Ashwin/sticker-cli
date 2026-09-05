//go:build !windows

package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

func atomicReplacePlatform(ctx context.Context, target string, data []byte, hooks Hooks) error {
	directory := filepath.Dir(target)
	targetName := filepath.Base(target)
	directoryFD, err := unix.Open(directory, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return wrapError("validation", "unsafe_path", "Use a real library directory.", err)
	}
	defer func() { _ = unix.Close(directoryFD) }()
	var targetStat unix.Stat_t
	if err := unix.Fstatat(directoryFD, targetName, &targetStat, unix.AT_SYMLINK_NOFOLLOW); err == nil {
		if targetStat.Mode&unix.S_IFMT == unix.S_IFLNK {
			return errorf("validation", "unsafe_path", "Remove the manifest symlink before writing.", "manifest target is a symbolic link")
		}
	} else if !errors.Is(err, unix.ENOENT) {
		return wrapError("io", "read_failed", "Check the manifest path.", err)
	}
	temporaryName := ""
	var temporary *os.File
	for attempt := 0; attempt < 100; attempt++ {
		candidate := ".manifest-" + time.Now().Format("20060102150405.000000000") + "-" + string(rune('a'+attempt%26)) + ".tmp"
		fd, openErr := unix.Openat(directoryFD, candidate, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
		if errors.Is(openErr, unix.EEXIST) {
			continue
		}
		if openErr != nil {
			return wrapError("io", "write_failed", "Choose a writable library directory.", fmt.Errorf("create temporary: %w", openErr))
		}
		temporaryName = candidate
		temporary = os.NewFile(uintptr(fd), filepath.Join(directory, candidate))
		break
	}
	if temporary == nil {
		return errorf("io", "write_failed", "Retry the operation.", "could not create a unique temporary manifest")
	}
	removeTemporary := true
	defer func() {
		_ = temporary.Close()
		if removeTemporary {
			_ = unix.Unlinkat(directoryFD, temporaryName, 0)
		}
	}()
	if _, err := copyContext(ctx, temporary, bytesReader(data)); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return errorf("cancelled", "interrupted", "Retry the operation when ready.", "manifest write cancelled")
		}
		return wrapError("io", "write_failed", "Retry the operation.", err)
	}
	if err := temporary.Sync(); err != nil {
		return wrapError("io", "write_failed", "Check available disk space.", fmt.Errorf("sync temporary: %w", err))
	}
	if err := temporary.Close(); err != nil {
		return wrapError("io", "write_failed", "Retry the operation.", fmt.Errorf("close temporary: %w", err))
	}
	if err := contextErr(ctx); err != nil {
		return err
	}
	if hooks.BeforeRename != nil {
		if err := hooks.BeforeRename(target); err != nil {
			return wrapError("io", "write_failed", "Retry the operation.", err)
		}
	}
	if err := unix.Renameat(directoryFD, temporaryName, directoryFD, targetName); err != nil {
		return wrapError("io", "write_failed", "Retry the operation.", fmt.Errorf("rename manifest: %w", err))
	}
	removeTemporary = false
	return finishAtomicRename(target, directory, hooks)
}
