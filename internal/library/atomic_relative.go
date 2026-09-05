package library

import (
	"context"
	"io"
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

// WriteRelativeAtomicFrom streams bounded data into a root-anchored temporary
// file, validates it before rename, and atomically publishes it below the
// library root. The validator receives the number of bytes copied and runs
// before the destination becomes visible.
func (l *Library) WriteRelativeAtomicFrom(ctx context.Context, relative string, source io.Reader, limit int64, validate func(int64) error) error {
	if source == nil {
		return errorf("validation", "invalid_argument", "Provide a source stream.", "source stream is nil")
	}
	if limit <= 0 {
		return errorf("validation", "invalid_argument", "Provide a positive byte limit.", "stream limit must be positive")
	}
	path, err := l.rootPath(filepath.FromSlash(relative))
	if err != nil {
		return err
	}
	return l.writeRelativeAtomicFrom(ctx, filepath.FromSlash(relative), path, source, limit, validate)
}

func (l *Library) writeRelativeAtomicFrom(ctx context.Context, relative, target string, source io.Reader, limit int64, validate func(int64) error) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if l.Hooks.BeforeManifest != nil {
		if err := l.Hooks.BeforeManifest(); err != nil {
			return wrapError("io", "write_failed", "Retry the operation.", err)
		}
	}
	return atomicReplaceRelativeReaderPlatform(ctx, l.Root, relative, target, source, limit, validate, l.Hooks)
}
