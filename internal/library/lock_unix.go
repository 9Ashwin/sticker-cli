//go:build !windows

package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

type fileLock struct {
	file *os.File
	once sync.Once
	err  error
}

func acquireLock(ctx context.Context, root string, exclusive bool, timeout time.Duration) (*fileLock, error) {
	if err := ctx.Err(); err != nil {
		return nil, errorf("cancelled", "interrupted", "Retry the operation when ready.", "library lock acquisition cancelled")
	}
	rootFD, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, wrapError("validation", "unsafe_path", "Use a real library directory.", err)
	}
	defer func() { _ = unix.Close(rootFD) }()
	stickerFD, err := unix.Openat(rootFD, ".sticker", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if errors.Is(err, unix.ENOENT) {
		if mkdirErr := unix.Mkdirat(rootFD, ".sticker", 0o700); mkdirErr != nil && !errors.Is(mkdirErr, unix.EEXIST) {
			return nil, wrapError("io", "write_failed", "Choose a writable library directory.", mkdirErr)
		}
		stickerFD, err = unix.Openat(rootFD, ".sticker", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	}
	if err != nil {
		return nil, wrapError("validation", "unsafe_path", "Remove links from the library path.", err)
	}
	defer func() { _ = unix.Close(stickerFD) }()
	fileFD, err := unix.Openat(stickerFD, "write.lock", unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC, 0o600)
	if errors.Is(err, unix.ENOENT) {
		// Some older Darwin filesystems do not permit O_CREAT through an
		// openat directory descriptor; the anchored directory is still held
		// while this fallback creates the lock file by its checked path.
		file, pathErr := os.OpenFile(filepath.Join(root, ".sticker", "write.lock"), os.O_CREATE|os.O_RDWR|syscall.O_CLOEXEC, 0o600)
		if pathErr == nil {
			var expected, actual unix.Stat_t
			if statErr := unix.Fstatat(stickerFD, "write.lock", &expected, unix.AT_SYMLINK_NOFOLLOW); statErr != nil || unix.Fstat(int(file.Fd()), &actual) != nil || expected.Dev != actual.Dev || expected.Ino != actual.Ino {
				_ = file.Close()
				return nil, errorf("validation", "unsafe_path", "Remove links from the library path.", "lock file escaped its directory")
			}
			return acquireExistingLock(ctx, timeout, exclusive, file, rootFD, stickerFD)
		}
		err = pathErr
	}
	if err != nil {
		return nil, wrapError("io", "write_failed", "Choose a writable library directory.", err)
	}
	file := os.NewFile(uintptr(fileFD), filepath.Join(root, ".sticker", "write.lock"))
	return acquireExistingLock(ctx, timeout, exclusive, file, rootFD, stickerFD)
}

func acquireExistingLock(ctx context.Context, timeout time.Duration, exclusive bool, file *os.File, rootFD, stickerFD int) (*fileLock, error) {
	operation := syscall.LOCK_SH
	if exclusive {
		operation = syscall.LOCK_EX
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			_ = file.Close()
			return nil, errorf("cancelled", "interrupted", "Retry the operation when ready.", "library lock acquisition cancelled")
		}
		lockErr := syscall.Flock(int(file.Fd()), operation|syscall.LOCK_NB)
		if lockErr == nil {
			return &fileLock{file: file}, nil
		}
		if !errors.Is(lockErr, syscall.EWOULDBLOCK) && !errors.Is(lockErr, syscall.EAGAIN) {
			_ = file.Close()
			return nil, wrapError("io", "write_failed", "Check the library lock permissions.", lockErr)
		}
		select {
		case <-ctx.Done():
			_ = file.Close()
			return nil, errorf("cancelled", "interrupted", "Retry the operation when ready.", "library lock acquisition cancelled")
		case <-deadline.C:
			_ = file.Close()
			return nil, errorf("conflict", "library_busy", "Retry after the other operation finishes.", "library lock is busy")
		case <-ticker.C:
		}
	}
}

func acquireReadLockIfPresent(ctx context.Context, root string, timeout time.Duration) (*fileLock, error) {
	path := filepath.Join(root, ".sticker", "write.lock")
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, wrapError("io", "read_failed", "Check the library lock.", err)
	}
	return acquireLock(ctx, root, false, timeout)
}

func (l *fileLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	l.once.Do(func() {
		if err := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN); err != nil {
			l.err = err
		}
		if closeErr := l.file.Close(); l.err == nil {
			l.err = closeErr
		}
	})
	return l.err
}
