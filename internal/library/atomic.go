package library

import (
	"context"
	"errors"
	"io"
	"os"
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

func finishAtomicRename(target, directory string, hooks Hooks) error {
	if hooks.AfterRename != nil {
		if err := hooks.AfterRename(target); err != nil {
			return &Error{Kind: "io", Subtype: "write_failed", Message: "manifest was committed but directory synchronization failed", Hint: "Read the manifest again before retrying.", Err: err, Committed: true}
		}
	}
	if err := syncDirectory(directory); err != nil {
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
