//go:build windows

package library

import (
	"os"
	"path/filepath"
)

func createRelativeTempDirectoryPlatform(root, relative, pattern string) (string, error) {
	path, err := os.MkdirTemp(filepath.Join(root, relative), pattern)
	if err != nil {
		return "", wrapError("io", "write_failed", "Choose a writable library directory.", err)
	}
	if err := rejectSymlinkComponents(root, path); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}
