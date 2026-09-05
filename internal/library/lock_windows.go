//go:build windows

package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const (
	lockfileExclusive       = 0x00000002
	lockfileFailImmediately = 0x00000001
)

var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	lockFileExProc   = kernel32.NewProc("LockFileEx")
	unlockFileExProc = kernel32.NewProc("UnlockFileEx")
)

type fileLock struct {
	file    *os.File
	anchors []*os.File
	overlap syscall.Overlapped
	once    sync.Once
	err     error
}

func acquireLock(ctx context.Context, root string, exclusive bool, timeout time.Duration) (*fileLock, error) {
	if err := ctx.Err(); err != nil {
		return nil, errorf("cancelled", "interrupted", "Retry the operation when ready.", "library lock acquisition cancelled")
	}
	rootAnchor, err := openDirectoryNoReparse(root)
	if err != nil {
		return nil, wrapError("validation", "unsafe_path", "Use a real library directory.", err)
	}
	sticker := filepath.Join(root, ".sticker")
	if err := os.Mkdir(sticker, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		_ = rootAnchor.Close()
		return nil, wrapError("io", "write_failed", "Choose a writable library directory.", err)
	}
	stickerAnchor, err := openDirectoryNoReparse(sticker)
	if err != nil {
		_ = rootAnchor.Close()
		return nil, wrapError("validation", "unsafe_path", "Remove links from the library path.", err)
	}
	file, err := openLockNoReparse(filepath.Join(sticker, "write.lock"))
	if err != nil {
		_ = stickerAnchor.Close()
		_ = rootAnchor.Close()
		return nil, wrapError("io", "write_failed", "Choose a writable library directory.", err)
	}
	lock := &fileLock{file: file, anchors: []*os.File{stickerAnchor, rootAnchor}}
	flags := uint32(lockfileFailImmediately)
	if exclusive {
		flags |= lockfileExclusive
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			_ = lock.Close()
			return nil, errorf("cancelled", "interrupted", "Retry the operation when ready.", "library lock acquisition cancelled")
		}
		result, _, callErr := lockFileExProc.Call(file.Fd(), uintptr(flags), 0, 1, 0, uintptr(unsafe.Pointer(&lock.overlap)))
		if result != 0 {
			return lock, nil
		}
		if callErr != syscall.Errno(33) && !errors.Is(callErr, syscall.Errno(997)) {
			_ = lock.Close()
			return nil, wrapError("io", "write_failed", "Check the library lock permissions.", callErr)
		}
		select {
		case <-ctx.Done():
			_ = lock.Close()
			return nil, errorf("cancelled", "interrupted", "Retry the operation when ready.", "library lock acquisition cancelled")
		case <-deadline.C:
			_ = lock.Close()
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
		_, _, unlockErr := unlockFileExProc.Call(l.file.Fd(), 0, 1, 0, uintptr(unsafe.Pointer(&l.overlap)))
		if unlockErr != syscall.Errno(0) {
			l.err = unlockErr
		}
		if closeErr := l.file.Close(); l.err == nil {
			l.err = closeErr
		}
		for _, anchor := range l.anchors {
			if closeErr := anchor.Close(); l.err == nil {
				l.err = closeErr
			}
		}
	})
	return l.err
}
