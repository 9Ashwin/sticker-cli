//go:build windows

package library

import (
	"os"
	"path/filepath"
)

func ensureRelativeDirectoryPlatform(root, relative string) error {
	path := filepath.Join(root, relative)
	if err := os.MkdirAll(path, 0o700); err != nil {
		return wrapError("io", "write_failed", "Choose a writable library directory.", err)
	}
	if err := rejectSymlinkComponents(root, path); err != nil {
		return err
	}
	return nil
}
