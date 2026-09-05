//go:build windows

package library

import (
	"context"
	"os"
	"path/filepath"
)

func removeRelativeDirectoryPlatform(_ context.Context, root, relative string) error {
	path := filepath.Join(root, relative)
	if err := os.RemoveAll(path); err != nil {
		return wrapError("io", "write_failed", "Remove the temporary export path.", err)
	}
	return nil
}
