//go:build windows

package library

import "context"

func atomicReplaceRelativePlatform(ctx context.Context, root, relative, target string, data []byte, hooks Hooks) error {
	if err := rejectSymlinkComponents(root, target); err != nil {
		return err
	}
	return atomicReplacePlatform(ctx, target, data, hooks)
}
