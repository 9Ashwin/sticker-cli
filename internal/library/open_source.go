package library

import (
	"errors"
	"os"
	"path/filepath"
)

// openSourceNoFollow resolves only the operating system's stable path aliases,
// rejects user-controlled symlink components, and then opens the canonical
// path one component at a time. The canonical path is used after validation so
// a concurrent replacement of the original path cannot redirect the read.
func openSourceNoFollow(path string) (*os.File, error) {
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	canonical = filepath.Clean(canonical)
	if err := validateSourceSymlinks(path, canonical); err != nil {
		return nil, err
	}
	return openAbsoluteNoFollow(canonical)
}

func sourcePathError(err error) error {
	var coded *Error
	if errors.As(err, &coded) {
		return err
	}
	return wrapError("io", "read_failed", "Check the source image path.", err)
}
