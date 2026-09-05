//go:build windows

package library

import (
	"bytes"
	"context"
	"errors"
	"io"
)

func atomicReplaceRelativePlatform(ctx context.Context, root, relative, target string, data []byte, hooks Hooks) error {
	if err := rejectSymlinkComponents(root, target); err != nil {
		return err
	}
	return atomicReplacePlatform(ctx, target, data, hooks)
}

func atomicReplaceRelativeReaderPlatform(ctx context.Context, root, relative, target string, source io.Reader, limit int64, validate func(int64) error, hooks Hooks) error {
	var data bytes.Buffer
	reader := source
	if limit > 0 {
		reader = io.LimitReader(source, limit+1)
	}
	count, err := copyContext(ctx, &data, reader)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return errorf("cancelled", "interrupted", "Retry the operation when ready.", "write cancelled")
		}
		return wrapError("io", "read_failed", "Check the source stream.", err)
	}
	if validate != nil {
		if err := validate(count); err != nil {
			return err
		}
	}
	return atomicReplaceRelativePlatform(ctx, root, relative, target, data.Bytes(), hooks)
}
