package packs

import (
	"context"
	"path/filepath"

	"github.com/9Ashwin/sticker-cli/internal/library"
)

// RemoveOptions controls removal of one installed pack relationship.
type RemoveOptions struct {
	Home   string
	PackID string
	DryRun bool
}

// RemoveResult describes the state relationship removed by one operation.
// Original image bytes are retained for personal favorites and other packs.
type RemoveResult struct {
	Removed       bool  `json:"removed"`
	RetainedBytes int64 `json:"retained_bytes"`
	Committed     bool  `json:"committed"`
	DryRun        bool  `json:"dry_run,omitempty"`
}

// Remove uninstalls one pack by deleting only its installed state. It never
// deletes image files, so favorites and other installed packs remain usable.
// Repeating the operation after the state is absent is a successful no-op.
func Remove(ctx context.Context, options RemoveOptions) (RemoveResult, error) {
	if err := contextErr(ctx); err != nil {
		return RemoveResult{}, err
	}
	if !packIDPattern.MatchString(options.PackID) {
		return RemoveResult{}, newError("validation", "invalid_argument", "pack ID is required", "Provide one valid pack ID from packs list.")
	}
	home, err := resolveHome(options.Home)
	if err != nil {
		return RemoveResult{}, err
	}
	root, err := library.New(home)
	if err != nil {
		return RemoveResult{}, fromLibraryError(err)
	}
	if options.DryRun {
		state, installed, err := readInstalledState(home, options.PackID)
		if err != nil {
			return RemoveResult{}, err
		}
		if !installed {
			return RemoveResult{DryRun: true}, nil
		}
		retained, err := retainedBytes(state)
		if err != nil {
			return RemoveResult{}, err
		}
		return RemoveResult{Removed: true, RetainedBytes: retained, DryRun: true}, nil
	}

	result := RemoveResult{}
	stateRelative := filepath.ToSlash(filepath.Join(".sticker", "packs", options.PackID+".json"))
	err = root.WithWriteLock(ctx, func(_ library.Manifest) error {
		if err := contextErr(ctx); err != nil {
			return err
		}
		state, installed, err := readInstalledState(home, options.PackID)
		if err != nil {
			return err
		}
		if !installed {
			return nil
		}
		retained, err := retainedBytes(state)
		if err != nil {
			return err
		}
		removed, err := root.RemoveRelative(ctx, stateRelative)
		if err != nil {
			return fromLibraryError(err)
		}
		if !removed {
			return nil
		}
		result = RemoveResult{Removed: true, RetainedBytes: retained, Committed: true}
		return nil
	})
	if err != nil {
		return RemoveResult{}, err
	}
	return result, nil
}

func retainedBytes(state installedState) (int64, error) {
	manifest, err := decodeManifest(state.Manifest)
	if err != nil {
		return 0, invalidInstalledState(state.ID, err.Error())
	}
	var total int64
	for _, item := range manifest.Items {
		total += item.Size
	}
	return total, nil
}
