//go:build windows

package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
)

func atomicReplacePlatform(ctx context.Context, target string, data []byte, hooks Hooks) error {
	directory := filepath.Dir(target)
	root, err := openDirectoryNoReparse(directory)
	if err != nil {
		return wrapError("validation", "unsafe_path", "Use a real library directory.", err)
	}
	defer func() { _ = root.Close() }()
	rootFinal, err := finalPath(root)
	if err != nil {
		return wrapError("io", "read_failed", "Check the library directory.", err)
	}
	if err := rejectExistingSymlink(target); err != nil {
		return err
	}
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
	temporaryHandle, err := os.Open(temporaryName)
	if err != nil {
		return wrapError("io", "read_failed", "Check the temporary manifest.", err)
	}
	temporaryFinal, err := finalPath(temporaryHandle)
	_ = temporaryHandle.Close()
	if err != nil {
		return wrapError("io", "read_failed", "Check the temporary manifest.", err)
	}
	if !windowsPathWithin(normalizeWindowsPath(rootFinal), normalizeWindowsPath(temporaryFinal)) {
		return errorf("validation", "unsafe_path", "Remove links from the library path.", "temporary file escapes library root")
	}
	if err := contextErr(ctx); err != nil {
		return err
	}
	if hooks.BeforeRename != nil {
		if err := hooks.BeforeRename(target); err != nil {
			return wrapError("io", "write_failed", "Retry the operation.", err)
		}
	}
	if err := renameAtomic(temporaryName, target); err != nil {
		return wrapError("io", "write_failed", "Retry the operation.", err)
	}
	removeTemporary = false
	targetHandle, err := openNoFollow(target)
	if err != nil {
		return &Error{Kind: "validation", Subtype: "unsafe_path", Message: "opened path escapes library root", Hint: "Read the manifest again before retrying.", Err: err, Committed: true}
	}
	targetFinal, err := finalPath(targetHandle)
	_ = targetHandle.Close()
	if err != nil || !windowsPathWithin(normalizeWindowsPath(rootFinal), normalizeWindowsPath(targetFinal)) {
		return &Error{Kind: "validation", Subtype: "unsafe_path", Message: "opened path escapes library root", Hint: "Read the manifest again before retrying.", Err: err, Committed: true}
	}
	return finishAtomicRename(target, directory, hooks)
}
