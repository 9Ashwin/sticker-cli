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
	return atomicReplacePlatform(ctx, target, data, l.Hooks)
}

// WriteRelativeAtomic publishes data at a path beneath the library root.
// Parent directories must already exist. The platform implementation anchors
// the write to the library root so a concurrent symlink swap cannot redirect
// the temporary file or rename outside the library.
func (l *Library) WriteRelativeAtomic(ctx context.Context, relative string, data []byte) error {
	path, err := l.rootPath(filepath.FromSlash(relative))
	if err != nil {
		return err
	}
	return l.writeRelativeAtomic(ctx, relative, path, data)
}

func finishAtomicRename(target, directory string, hooks Hooks) error {
	return finishAtomicRenameWith(target, directory, hooks, func() error {
		return syncDirectory(directory)
	})
}

func finishAtomicRenameWith(target, directory string, hooks Hooks, sync func() error) error {
	if hooks.AfterRename != nil {
		if err := hooks.AfterRename(target); err != nil {
			return &Error{Kind: "io", Subtype: "write_failed", Message: "manifest was committed but directory synchronization failed", Hint: "Read the manifest again before retrying.", Err: err, Committed: true}
		}
	}
	if err := sync(); err != nil {
		return &Error{Kind: "io", Subtype: "write_failed", Message: "manifest was committed but directory synchronization failed", Hint: "Read the manifest again before retrying.", Err: err, Committed: true}
	}
	if hooks.SyncDirectory != nil {
		if err := hooks.SyncDirectory(directory); err != nil {
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
