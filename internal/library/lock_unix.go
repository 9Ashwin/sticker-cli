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
	sticker := filepath.Join(root, ".sticker")
	if err := ensureLockDirectory(sticker); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(sticker, "write.lock"), os.O_CREATE|os.O_RDWR|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, wrapError("io", "write_failed", "Choose a writable library directory.", err)
	}
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
		err = syscall.Flock(int(file.Fd()), operation|syscall.LOCK_NB)
		if err == nil {
			return &fileLock{file: file}, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			_ = file.Close()
			return nil, wrapError("io", "write_failed", "Check the library lock permissions.", err)
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

func ensureLockDirectory(path string) error {
	if err := os.Mkdir(path, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return wrapError("io", "write_failed", "Choose a writable library directory.", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return wrapError("io", "read_failed", "Check the library lock directory.", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errorf("validation", "unsafe_path", "Remove links from the library path.", "lock directory is not a directory")
	}
	return nil
}
