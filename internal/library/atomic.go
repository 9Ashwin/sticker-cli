package library

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
)

func (l *Library) atomicReplace(ctx context.Context, target string, data []byte) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if l.Hooks.BeforeManifest != nil {
		if err := l.Hooks.BeforeManifest(); err != nil {
			return wrapError("io", "write_failed", "Retry the operation.", err)
		}
	}
	if err := rejectExistingSymlink(target); err != nil {
		return err
	}
	directory := filepath.Dir(target)
	temporary, err := os.CreateTemp(directory, ".manifest-*.tmp")
	if err != nil {
		return wrapError("io", "write_failed", "Choose a writable library directory.", err)
	}
	temporaryName := temporary.Name()
	removeTemporary := true
	defer func() {
		_ = temporary.Close()
		if removeTemporary {
			_ = os.Remove(temporaryName)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return wrapError("io", "write_failed", "Choose a writable library directory.", err)
	}
	if _, err := copyContext(ctx, temporary, bytesReader(data)); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return errorf("cancelled", "interrupted", "Retry the operation when ready.", "manifest write cancelled")
		}
		return wrapError("io", "write_failed", "Retry the operation.", err)
	}
	if err := temporary.Sync(); err != nil {
		return wrapError("io", "write_failed", "Check available disk space.", err)
	}
	if err := temporary.Close(); err != nil {
		return wrapError("io", "write_failed", "Retry the operation.", err)
	}
	if err := contextErr(ctx); err != nil {
		return err
	}
	if l.Hooks.BeforeRename != nil {
		if err := l.Hooks.BeforeRename(target); err != nil {
			return wrapError("io", "write_failed", "Retry the operation.", err)
		}
	}
	if err := renameAtomic(temporaryName, target); err != nil {
		return wrapError("io", "write_failed", "Retry the operation.", err)
	}
	removeTemporary = false
	if l.Hooks.AfterRename != nil {
		if err := l.Hooks.AfterRename(target); err != nil {
			return &Error{Kind: "io", Subtype: "write_failed", Message: "manifest was committed but directory synchronization failed", Hint: "Read the manifest again before retrying.", Err: err, Committed: true}
		}
	}
	if err := syncDirectory(directory); err != nil {
		return &Error{Kind: "io", Subtype: "write_failed", Message: "manifest was committed but directory synchronization failed", Hint: "Read the manifest again before retrying.", Err: err, Committed: true}
	}
	if l.Hooks.SyncDirectory != nil {
		if err := l.Hooks.SyncDirectory(directory); err != nil {
			return &Error{Kind: "io", Subtype: "write_failed", Message: "manifest was committed but directory synchronization failed", Hint: "Read the manifest again before retrying.", Err: err, Committed: true}
		}
	}
	return nil
}

func rejectExistingSymlink(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return wrapError("io", "read_failed", "Check the manifest path.", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errorf("validation", "unsafe_path", "Remove the manifest symlink before writing.", "manifest target is a symbolic link")
	}
	return nil
}

type byteReader struct {
	data []byte
	off  int
}

func bytesReader(data []byte) io.Reader { return &byteReader{data: data} }

func (r *byteReader) Read(p []byte) (int, error) {
	if r.off >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.off:])
	r.off += n
	return n, nil
}
