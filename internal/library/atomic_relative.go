package library

import (
	"context"
	"path/filepath"
)

func (l *Library) writeRelativeAtomic(ctx context.Context, relative string, target string, data []byte) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if l.Hooks.BeforeManifest != nil {
		if err := l.Hooks.BeforeManifest(); err != nil {
			return wrapError("io", "write_failed", "Retry the operation.", err)
		}
	}
	return atomicReplaceRelativePlatform(ctx, l.Root, filepath.FromSlash(relative), target, data, l.Hooks)
}
