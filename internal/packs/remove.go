package packs

import (
	"context"
	"errors"
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
	StateCorrupt  bool  `json:"state_corrupt,omitempty"`
	DryRun        bool  `json:"dry_run,omitempty"`
}

// Remove uninstalls one pack by deleting only its installed state. It never
// deletes image files, so favorites and other installed packs remain usable.
// Repeating the operation after the state is absent is a successful no-op.
func Remove(ctx context.Context, options RemoveOptions) (RemoveResult, error) {
	return cleanupPackState(ctx, options, false)
}

// cleanupPackState removes the installed state for one pack. When corruptOnly
// is true, a valid installed state is left untouched; this is the recovery
// path used by Repair. Invalid state metadata is safe to unlink because the
// operation never removes original image files.
func cleanupPackState(ctx context.Context, options RemoveOptions, corruptOnly bool) (RemoveResult, error) {
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
		observed, err := readRemovableState(home, options.PackID)
		if err != nil {
			return RemoveResult{}, err
		}
		if !observed.installed || (corruptOnly && !observed.corrupt) {
			return RemoveResult{DryRun: true}, nil
		}
		retained := int64(0)
		if !observed.corrupt {
			retained, err = retainedBytes(observed.state)
			if err != nil {
				return RemoveResult{}, err
			}
		}
		return RemoveResult{Removed: true, RetainedBytes: retained, StateCorrupt: observed.corrupt, DryRun: true}, nil
	}

	result := RemoveResult{}
	stateRelative := filepath.ToSlash(filepath.Join(".sticker", "packs", options.PackID+".json"))
	err = root.WithWriteLock(ctx, func(_ library.Manifest) error {
		if err := contextErr(ctx); err != nil {
			return err
		}
		observed, err := readRemovableState(home, options.PackID)
		if err != nil {
			return err
		}
		if !observed.installed || (corruptOnly && !observed.corrupt) {
			return nil
		}
		retained := int64(0)
		if !observed.corrupt {
			retained, err = retainedBytes(observed.state)
			if err != nil {
				return err
			}
		}
		removed, err := root.RemoveRelative(ctx, stateRelative)
		if err != nil {
			return fromLibraryError(err)
		}
		if !removed {
			return nil
		}
		result = RemoveResult{Removed: true, RetainedBytes: retained, StateCorrupt: observed.corrupt, Committed: true}
		return nil
	})
	if err != nil {
		return RemoveResult{}, err
	}
	return result, nil
}

type removableState struct {
	state     installedState
	installed bool
	corrupt   bool
}

func readRemovableState(home, id string) (removableState, error) {
	state, installed, err := readInstalledState(home, id)
	if err == nil {
		return removableState{state: state, installed: installed}, nil
	}
	var packErr *Error
	if errors.As(err, &packErr) && packErr.Kind == "integrity" && packErr.Subtype == "invalid_collection" {
		return removableState{state: installedState{ID: id}, installed: true, corrupt: true}, nil
	}
	return removableState{}, err
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
