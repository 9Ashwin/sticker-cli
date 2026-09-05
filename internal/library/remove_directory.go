package library

import (
	"context"
	"path/filepath"
)

// RemoveRelativeDirectory removes a directory tree beneath the library root
// without following symbolic links. A missing target is already clean.
// Callers should use it for private temporary directories, never for the
// library root itself.
func (l *Library) RemoveRelativeDirectory(ctx context.Context, relative string) error {
	relativePath := filepath.FromSlash(relative)
	if relative == "" || relative == "." || filepath.IsAbs(relativePath) || filepath.Clean(relativePath) != relativePath || filepath.VolumeName(relativePath) != "" {
		return errorf("validation", "unsafe_path", "Use a directory inside the library root.", "path escapes library root")
	}
	if err := contextErr(ctx); err != nil {
		return err
	}
	if err := l.ensureRoot(false); err != nil {
		return err
	}
	return removeRelativeDirectoryPlatform(ctx, l.Root, relativePath)
}
