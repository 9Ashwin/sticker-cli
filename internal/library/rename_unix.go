//go:build !windows

package library

import (
	"os"
	"path/filepath"
)

func renameAtomic(from, to string) error { return os.Rename(from, to) }

func syncDirectory(path string) error {
	directory, err := os.Open(filepath.Clean(path))
	if err != nil {
		return err
	}
	defer func() { _ = directory.Close() }()
	return directory.Sync()
}
