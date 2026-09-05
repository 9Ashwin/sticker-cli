package library

import (
	"context"
	"path/filepath"
)

// RemoveRelative removes one file beneath the library root without following
// symbolic links in path components. The operation is idempotent: a missing
// root, parent, or target returns (false, nil). A present target is removed in
// one filesystem operation and returns (true, nil).
func (l *Library) RemoveRelative(ctx context.Context, relative string) (bool, error) {
	if err := contextErr(ctx); err != nil {
		return false, err
	}
	target, err := l.rootPath(filepath.FromSlash(relative))
	if err != nil {
		return false, err
	}
	return removeRelativePlatform(ctx, l.Root, filepath.FromSlash(relative), target)
}
