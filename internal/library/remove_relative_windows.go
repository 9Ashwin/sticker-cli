//go:build windows

package library

import (
	"context"
	"errors"
	"os"
)

// removeRelativePlatform performs the same checked operation on Windows. The
// reparse-point check keeps the path within the library before deletion.
func removeRelativePlatform(ctx context.Context, root, relative, target string) (bool, error) {
	if err := contextErr(ctx); err != nil {
		return false, err
	}
	if err := rejectSymlinkComponents(root, target); err != nil {
		return false, err
	}
	info, err := os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return false, errorf("validation", "unsafe_path", "Remove links from the library path.", "target is a symbolic link")
	}
	if err := os.Remove(target); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return true, nil
}
