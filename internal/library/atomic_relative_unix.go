//go:build !windows

package library

import "context"

func atomicReplaceRelativePlatform(ctx context.Context, root, relative, target string, data []byte, hooks Hooks) error {
	return atomicReplaceRelativeReaderPlatform(ctx, root, relative, target, bytesReader(data), 0, nil, hooks)
}
