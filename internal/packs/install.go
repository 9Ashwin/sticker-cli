package packs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/9Ashwin/sticker-cli/internal/library"
)

// PlanOptions controls an installation preflight. Planning reads the selected
// source manifest and the local library, but never downloads an image or
// creates local library, lock, or catalog files.
type PlanOptions struct {
	Home       string
	Source     string
	PackID     string
	HTTPClient *http.Client
	Backoff    func(context.Context, time.Duration) error
}

// InstallPlan is the aggregate work estimate returned by a preflight.
type InstallPlan struct {
	Source        string `json:"source"`
	Target        string `json:"target"`
	Pack          Pack   `json:"pack"`
	Revision      string `json:"revision"`
	Added         int    `json:"added"`
	Reused        int    `json:"reused"`
	DownloadBytes int64  `json:"download_bytes"`
}

// Plan validates one explicitly selected pack and compares every referenced
// image with the local content. A missing image is counted as added; an
// existing image must pass both hashes, its declared size, and its format
// signature before it can be reused.
func Plan(ctx context.Context, options PlanOptions) (InstallPlan, error) {
	if err := contextErr(ctx); err != nil {
		return InstallPlan{}, wrapError("cancelled", "interrupted", "operation cancelled", "Retry the operation when ready.", err)
	}
	if !packIDPattern.MatchString(options.PackID) {
		return InstallPlan{}, newError("validation", "invalid_argument", "pack ID is required", "Provide one valid pack ID from packs list.")
	}
	source, err := Resolve(options.Source)
	if err != nil {
		return InstallPlan{}, err
	}
	if source.IsLocal() {
		if err := source.validateLocal(); err != nil {
			return InstallPlan{}, err
		}
	}
	home, err := resolveHome(options.Home)
	if err != nil {
		return InstallPlan{}, err
	}
	root, err := library.New(home)
	if err != nil {
		return InstallPlan{}, fromLibraryError(err)
	}
	personal, err := root.ReadManifest(ctx)
	if err != nil {
		return InstallPlan{}, fromLibraryError(err)
	}
	snapshot, err := fetchPackSnapshot(ctx, source, Options{
		HTTPClient: options.HTTPClient,
		Backoff:    options.Backoff,
	}, options.PackID)
	if err != nil {
		return InstallPlan{}, err
	}
	state, installed, err := readInstalledState(home, options.PackID)
	if err != nil {
		return InstallPlan{}, err
	}
	if installed {
		stateSource, err := Resolve(state.Source)
		if err != nil {
			return InstallPlan{}, invalidInstalledState(options.PackID, "source is invalid")
		}
		if stateSource.Canonical != source.Canonical {
			return InstallPlan{}, newError("conflict", "source_conflict", fmt.Sprintf("pack %s is already installed from another source", options.PackID), "Remove the existing pack before installing it from a new source.")
		}
		if state.Revision != snapshot.pack.Revision {
			return InstallPlan{}, newError("conflict", "state_changed", fmt.Sprintf("pack %s is already installed at revision %s", options.PackID, state.Revision), "Use packs update for an installed pack with a different revision.")
		}
		snapshot.pack.Installed = true
	}
	if err := checkPersonalConflicts(personal, snapshot.manifest, options.PackID); err != nil {
		return InstallPlan{}, err
	}

	plan := InstallPlan{
		Source:   source.Canonical,
		Target:   root.Root,
		Pack:     snapshot.pack,
		Revision: snapshot.pack.Revision,
	}
	for _, item := range snapshot.manifest.Items {
		if err := contextErr(ctx); err != nil {
			return InstallPlan{}, wrapError("cancelled", "interrupted", "operation cancelled", "Retry the operation when ready.", err)
		}
		if err := root.VerifyItem(ctx, item); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				plan.Added++
				plan.DownloadBytes += item.Size
				continue
			}
			return InstallPlan{}, fromLibraryError(err)
		}
		plan.Reused++
	}
	return plan, nil
}

func checkPersonalConflicts(personal library.Manifest, selected library.Manifest, packID string) error {
	byID := make(map[string]library.Item, len(personal.Items))
	for _, item := range personal.Items {
		byID[item.MD5] = item
	}
	for _, item := range selected.Items {
		existing, ok := byID[item.MD5]
		if !ok {
			continue
		}
		if existing.SHA256 != item.SHA256 || existing.Size != item.Size || existing.Format != item.Format {
			return newError("conflict", "digest_conflict", fmt.Sprintf("item %s conflicts with the personal library", item.MD5), fmt.Sprintf("Resolve the existing personal entry before installing pack %s.", packID))
		}
	}
	return nil
}

func readInstalledState(home, id string) (installedState, bool, error) {
	path := filepath.Join(home, ".sticker", "packs", id+".json")
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return installedState{}, false, nil
	} else if err != nil {
		return installedState{}, false, wrapError("io", "read_failed", "cannot inspect installed pack state", "Check the local pack state permissions.", err)
	}
	relative := filepath.ToSlash(filepath.Join(".sticker", "packs", id+".json"))
	data, err := readHomeFile(home, relative, maxCacheBytes)
	if err != nil {
		return installedState{}, false, err
	}
	var state installedState
	if err := decodeStrict(data, &state); err != nil {
		return installedState{}, false, invalidInstalledState(id, err.Error())
	}
	if state.SchemaVersion != 1 || state.ID != id || state.Source == "" || !isLowerHex(state.Revision, sha256.Size) || len(state.Manifest) == 0 {
		return installedState{}, false, invalidInstalledState(id, "metadata is invalid")
	}
	sum := sha256.Sum256(state.Manifest)
	if hex.EncodeToString(sum[:]) != state.Revision {
		return installedState{}, false, invalidInstalledState(id, "manifest revision does not match its raw bytes")
	}
	if _, err := decodeManifest(state.Manifest); err != nil {
		return installedState{}, false, invalidInstalledState(id, err.Error())
	}
	state.Manifest = append([]byte(nil), state.Manifest...)
	return state, true, nil
}

func invalidInstalledState(id, reason string) *Error {
	return newError("integrity", "invalid_collection", fmt.Sprintf("installed pack state %q is invalid: %s", id, reason), "Repair or remove the installed pack state before retrying.")
}

func fromLibraryError(err error) error {
	if err == nil {
		return nil
	}
	var libraryErr *library.Error
	if errors.As(err, &libraryErr) {
		return &Error{
			Kind:      libraryErr.Kind,
			Subtype:   libraryErr.Subtype,
			Message:   libraryErr.Message,
			Hint:      libraryErr.Hint,
			Retryable: libraryErr.Retryable,
			Err:       err,
		}
	}
	return wrapError("io", "read_failed", "cannot inspect the local library", "Check the local library permissions.", err)
}
