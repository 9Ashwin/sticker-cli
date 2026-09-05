package packs

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"net"
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

// UpdatePlan is the preflight result for an explicitly requested update.
// It has the same fields as an installation plan so callers can render both
// operations with one result contract.
type UpdatePlan = InstallPlan

// InstallOptions controls a complete pack installation. Image transfer is
// performed before the library lock is acquired; only the final validation and
// state publication run under that lock.
type InstallOptions struct {
	Home       string
	Source     string
	PackID     string
	HTTPClient *http.Client
	Backoff    func(context.Context, time.Duration) error
	Now        func() time.Time
}

// InstallResult describes the files published by one installation.
type InstallResult struct {
	Source        string `json:"source"`
	Target        string `json:"target"`
	Pack          Pack   `json:"pack"`
	Revision      string `json:"revision"`
	Added         int    `json:"added"`
	Reused        int    `json:"reused"`
	DownloadBytes int64  `json:"download_bytes"`
}

// UpdateResult is the result of one explicitly requested pack update.
type UpdateResult = InstallResult

type preparedInstall struct {
	root     *library.Library
	home     string
	source   Source
	snapshot packSnapshot
}

type stagedImage struct {
	root string
}

// Plan validates one explicitly selected pack and compares every referenced
// image with the local content. A missing image is counted as added; an
// existing image must pass both hashes, its declared size, and its format
// signature before it can be reused.
func Plan(ctx context.Context, options PlanOptions) (InstallPlan, error) {
	prepared, err := prepareInstall(ctx, options)
	if err != nil {
		return InstallPlan{}, err
	}
	return makeInstallPlan(ctx, prepared)
}

func prepareInstall(ctx context.Context, options PlanOptions) (preparedInstall, error) {
	if err := contextErr(ctx); err != nil {
		return preparedInstall{}, wrapError("cancelled", "interrupted", "operation cancelled", "Retry the operation when ready.", err)
	}
	if !packIDPattern.MatchString(options.PackID) {
		return preparedInstall{}, newError("validation", "invalid_argument", "pack ID is required", "Provide one valid pack ID from packs list.")
	}
	source, err := Resolve(options.Source)
	if err != nil {
		return preparedInstall{}, err
	}
	if source.IsLocal() {
		if err := source.validateLocal(); err != nil {
			return preparedInstall{}, err
		}
	}
	home, err := resolveHome(options.Home)
	if err != nil {
		return preparedInstall{}, err
	}
	root, err := library.New(home)
	if err != nil {
		return preparedInstall{}, fromLibraryError(err)
	}
	personal, err := root.ReadManifest(ctx)
	if err != nil {
		return preparedInstall{}, fromLibraryError(err)
	}
	snapshot, err := fetchPackSnapshot(ctx, source, Options{
		HTTPClient: options.HTTPClient,
		Backoff:    options.Backoff,
	}, options.PackID)
	if err != nil {
		return preparedInstall{}, err
	}
	state, installed, err := readInstalledState(home, options.PackID)
	if err != nil {
		return preparedInstall{}, err
	}
	if installed {
		if err := validateInstalledForSnapshot(state, source, snapshot.pack.Revision, options.PackID); err != nil {
			return preparedInstall{}, err
		}
		snapshot.pack.Installed = true
	}
	if err := checkPersonalConflicts(personal, snapshot.manifest, options.PackID); err != nil {
		return preparedInstall{}, err
	}
	return preparedInstall{root: root, home: home, source: source, snapshot: snapshot}, nil
}

func validateInstalledForSnapshot(state installedState, source Source, revision, id string) error {
	stateSource, err := Resolve(state.Source)
	if err != nil {
		return invalidInstalledState(id, "source is invalid")
	}
	if stateSource.Canonical != source.Canonical {
		return newError("conflict", "source_conflict", fmt.Sprintf("pack %s is already installed from another source", id), "Remove the existing pack before installing it from a new source.")
	}
	if state.Revision != revision {
		return newError("conflict", "state_changed", fmt.Sprintf("pack %s is already installed at revision %s", id, state.Revision), "Use packs update for an installed pack with a different revision.")
	}
	return nil
}

func makeInstallPlan(ctx context.Context, prepared preparedInstall) (InstallPlan, error) {
	plan := InstallPlan{
		Source:   prepared.source.Canonical,
		Target:   prepared.root.Root,
		Pack:     prepared.snapshot.pack,
		Revision: prepared.snapshot.pack.Revision,
	}
	for _, item := range prepared.snapshot.manifest.Items {
		if err := contextErr(ctx); err != nil {
			return InstallPlan{}, wrapError("cancelled", "interrupted", "operation cancelled", "Retry the operation when ready.", err)
		}
		if err := prepared.root.VerifyItem(ctx, item); err != nil {
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

// Install downloads, validates, and publishes one selected pack. The source
// snapshot is fixed by its manifest revision. Incomplete transfers stay in a
// private staging directory and never make an installed state visible.
func Install(ctx context.Context, options InstallOptions) (InstallResult, error) {
	prepared, err := prepareInstall(ctx, PlanOptions{
		Home:       options.Home,
		Source:     options.Source,
		PackID:     options.PackID,
		HTTPClient: options.HTTPClient,
		Backoff:    options.Backoff,
	})
	if err != nil {
		return InstallResult{}, err
	}
	plan, err := makeInstallPlan(ctx, prepared)
	if err != nil {
		return InstallResult{}, err
	}
	result := InstallResult{
		Source:   plan.Source,
		Target:   plan.Target,
		Pack:     plan.Pack,
		Revision: plan.Revision,
	}

	stagingRoot, staged, downloaded, err := stageMissingImages(ctx, prepared, options.HTTPClient, options.Backoff)
	if err != nil {
		return InstallResult{}, err
	}
	result.DownloadBytes += downloaded

	result, err = publishInstall(ctx, prepared, staged, result, options.Now)
	if err != nil {
		return InstallResult{}, err
	}
	if stagingRoot != "" {
		_ = os.RemoveAll(stagingRoot)
	}
	return result, nil
}

func installCancelled(err error) *Error {
	return wrapError("cancelled", "interrupted", "operation cancelled", "Retry the operation when ready.", err)
}

func createStagingDirectory(root *library.Library) (string, error) {
	path, err := root.CreateRelativeTempDirectory(filepath.ToSlash(filepath.Join(".sticker", "staging")), "install-*")
	if err != nil {
		return "", fromLibraryError(err)
	}
	return path, nil
}

func stageMissingImages(ctx context.Context, prepared preparedInstall, client *http.Client, backoff func(context.Context, time.Duration) error) (string, map[string]stagedImage, int64, error) {
	stagingRoot, err := createStagingDirectory(prepared.root)
	if err != nil {
		return "", nil, 0, err
	}
	staged := make(map[string]stagedImage)
	stagingLibrary, err := library.New(stagingRoot)
	if err != nil {
		return stagingRoot, staged, 0, fromLibraryError(err)
	}
	if err := stagingLibrary.EnsureRelativeDirectory(library.EmoticonsDirectory); err != nil {
		return stagingRoot, staged, 0, fromLibraryError(err)
	}
	var downloaded int64
	for _, item := range prepared.snapshot.manifest.Items {
		if err := contextErr(ctx); err != nil {
			return stagingRoot, staged, downloaded, installCancelled(err)
		}
		if err := prepared.root.VerifyItem(ctx, item); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return stagingRoot, staged, downloaded, fromLibraryError(err)
		}
		candidate, found, err := findReusableStaged(ctx, prepared.home, item)
		if err != nil {
			return stagingRoot, staged, downloaded, err
		}
		if found {
			staged[item.MD5] = stagedImage{root: candidate}
			continue
		}
		if err := downloadImage(ctx, prepared.source, item, stagingRoot, client, backoff); err != nil {
			return stagingRoot, staged, downloaded, err
		}
		staged[item.MD5] = stagedImage{root: stagingRoot}
		downloaded += item.Size
	}
	return stagingRoot, staged, downloaded, nil
}

// UpdateOptions controls a refresh of one installed pack. The source is read
// from the installed state so an update cannot silently switch origins.
type UpdateOptions struct {
	Home       string
	PackID     string
	HTTPClient *http.Client
	Backoff    func(context.Context, time.Duration) error
	Now        func() time.Time
}

type preparedUpdate struct {
	prepared preparedInstall
	state    installedState
}

// PlanUpdate reads the saved source for an installed pack, fetches its latest
// validated manifest, and counts local work without creating library state or
// staging files.
func PlanUpdate(ctx context.Context, options UpdateOptions) (UpdatePlan, error) {
	prepared, err := prepareUpdate(ctx, options)
	if err != nil {
		return UpdatePlan{}, err
	}
	return makeInstallPlan(ctx, prepared.prepared)
}

func prepareUpdate(ctx context.Context, options UpdateOptions) (preparedUpdate, error) {
	if err := contextErr(ctx); err != nil {
		return preparedUpdate{}, installCancelled(err)
	}
	if !packIDPattern.MatchString(options.PackID) {
		return preparedUpdate{}, newError("validation", "invalid_argument", "pack ID is required", "Provide one valid pack ID from packs list.")
	}
	home, err := resolveHome(options.Home)
	if err != nil {
		return preparedUpdate{}, err
	}
	root, err := library.New(home)
	if err != nil {
		return preparedUpdate{}, fromLibraryError(err)
	}
	state, installed, err := readInstalledState(home, options.PackID)
	if err != nil {
		return preparedUpdate{}, err
	}
	if !installed {
		return preparedUpdate{}, newError("not_found", "pack_not_found", fmt.Sprintf("pack %s is not installed", options.PackID), "Install the pack before updating it.")
	}
	source, err := Resolve(state.Source)
	if err != nil {
		return preparedUpdate{}, invalidInstalledState(options.PackID, "source is invalid")
	}
	if source.IsLocal() {
		if err := source.validateLocal(); err != nil {
			return preparedUpdate{}, err
		}
	}
	personal, err := root.ReadManifest(ctx)
	if err != nil {
		return preparedUpdate{}, fromLibraryError(err)
	}
	snapshot, err := fetchPackSnapshot(ctx, source, Options{
		HTTPClient: options.HTTPClient,
		Backoff:    options.Backoff,
	}, options.PackID)
	if err != nil {
		return preparedUpdate{}, err
	}
	if err := checkPersonalConflicts(personal, snapshot.manifest, options.PackID); err != nil {
		return preparedUpdate{}, err
	}
	snapshot.pack.Installed = true
	return preparedUpdate{
		prepared: preparedInstall{root: root, home: home, source: source, snapshot: snapshot},
		state:    state,
	}, nil
}

// Update downloads and validates a new manifest revision before replacing
// the installed state. The old state remains authoritative until every image
// in the new manifest has been persisted and verified.
func Update(ctx context.Context, options UpdateOptions) (UpdateResult, error) {
	prepared, err := prepareUpdate(ctx, options)
	if err != nil {
		return UpdateResult{}, err
	}
	plan, err := makeInstallPlan(ctx, prepared.prepared)
	if err != nil {
		return UpdateResult{}, err
	}
	result := UpdateResult{
		Source:   plan.Source,
		Target:   plan.Target,
		Pack:     plan.Pack,
		Revision: plan.Revision,
	}
	stagingRoot, staged, downloaded, err := stageMissingImages(ctx, prepared.prepared, options.HTTPClient, options.Backoff)
	if err != nil {
		return UpdateResult{}, err
	}
	result.DownloadBytes = downloaded
	result, err = publishUpdate(ctx, prepared, staged, result, options.Now)
	if err != nil {
		return UpdateResult{}, err
	}
	if stagingRoot != "" {
		_ = os.RemoveAll(stagingRoot)
	}
	return result, nil
}

func publishUpdate(ctx context.Context, prepared preparedUpdate, staged map[string]stagedImage, result UpdateResult, now func() time.Time) (UpdateResult, error) {
	result.Pack.Installed = true
	err := prepared.prepared.root.WithWriteLock(ctx, func(personal library.Manifest) error {
		if err := checkPersonalConflicts(personal, prepared.prepared.snapshot.manifest, prepared.prepared.snapshot.pack.ID); err != nil {
			return err
		}
		state, installed, err := readInstalledState(prepared.prepared.home, prepared.prepared.snapshot.pack.ID)
		if err != nil {
			return err
		}
		if !installed {
			return newError("conflict", "state_changed", fmt.Sprintf("pack %s is no longer installed", prepared.prepared.snapshot.pack.ID), "Install the pack again before updating it.")
		}
		if err := validateUpdateState(state, prepared.prepared.source, prepared.state.Revision, prepared.prepared.snapshot.pack.ID); err != nil {
			return err
		}

		result.Added = 0
		result.Reused = 0
		for _, item := range prepared.prepared.snapshot.manifest.Items {
			if err := contextErr(ctx); err != nil {
				return installCancelled(err)
			}
			if err := prepared.prepared.root.VerifyItem(ctx, item); err == nil {
				result.Reused++
				continue
			} else if !errors.Is(err, os.ErrNotExist) {
				return fromLibraryError(err)
			}
			stagedItem, ok := staged[item.MD5]
			if !ok {
				return newError("integrity", "invalid_image", fmt.Sprintf("staged image %s is missing", item.MD5), "Retry the update to download the missing image.")
			}
			data, err := readStagedItem(ctx, stagedItem.root, item)
			if err != nil {
				return err
			}
			if err := prepared.prepared.root.WriteRelativeAtomic(ctx, item.Filename, data); err != nil {
				return fromLibraryError(err)
			}
			if err := prepared.prepared.root.VerifyItem(ctx, item); err != nil {
				return fromLibraryError(err)
			}
			result.Added++
		}

		if err := prepared.prepared.root.EnsureRelativeDirectory(filepath.ToSlash(filepath.Join(".sticker", "packs"))); err != nil {
			return fromLibraryError(err)
		}
		if now == nil {
			now = time.Now
		}
		stateBytes, err := encodeInstalledState(installedState{
			SchemaVersion: 1,
			ID:            prepared.prepared.snapshot.pack.ID,
			Source:        prepared.prepared.source.Canonical,
			Revision:      prepared.prepared.snapshot.pack.Revision,
			InstalledAt:   now().UTC(),
			Manifest:      append([]byte(nil), prepared.prepared.snapshot.manifestBytes...),
		})
		if err != nil {
			return err
		}
		if err := prepared.prepared.root.WriteRelativeAtomic(ctx, filepath.ToSlash(filepath.Join(".sticker", "packs", prepared.prepared.snapshot.pack.ID+".json")), stateBytes); err != nil {
			return fromLibraryError(err)
		}
		return nil
	})
	if err != nil {
		return UpdateResult{}, err
	}
	return result, nil
}

func validateUpdateState(state installedState, source Source, revision, id string) error {
	stateSource, err := Resolve(state.Source)
	if err != nil {
		return invalidInstalledState(id, "source is invalid")
	}
	if stateSource.Canonical != source.Canonical {
		return newError("conflict", "source_conflict", fmt.Sprintf("pack %s source changed while updating", id), "Restore the saved pack source or remove the installed pack before retrying.")
	}
	if state.Revision != revision {
		return newError("conflict", "state_changed", fmt.Sprintf("pack %s changed while updating", id), "Read the installed state and retry the update.")
	}
	return nil
}

func findReusableStaged(ctx context.Context, home string, item library.Item) (string, bool, error) {
	parent := filepath.Join(home, ".sticker", "staging")
	entries, err := os.ReadDir(parent)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, wrapError("io", "read_failed", "cannot read the staging directory", "Check the staging directory.", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		candidate := filepath.Join(parent, entry.Name())
		candidateLibrary, err := library.New(candidate)
		if err != nil {
			return "", false, fromLibraryError(err)
		}
		if err := candidateLibrary.VerifyItem(ctx, item); err == nil {
			return candidate, true, nil
		} else if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return "", false, installCancelled(err)
		}
	}
	return "", false, nil
}

func publishInstall(ctx context.Context, prepared preparedInstall, staged map[string]stagedImage, result InstallResult, now func() time.Time) (InstallResult, error) {
	result.Pack.Installed = true
	err := prepared.root.WithWriteLock(ctx, func(personal library.Manifest) error {
		if err := checkPersonalConflicts(personal, prepared.snapshot.manifest, prepared.snapshot.pack.ID); err != nil {
			return err
		}
		state, installed, err := readInstalledState(prepared.home, prepared.snapshot.pack.ID)
		if err != nil {
			return err
		}
		if installed {
			if err := validateInstalledForSnapshot(state, prepared.source, prepared.snapshot.pack.Revision, prepared.snapshot.pack.ID); err != nil {
				return err
			}
		}
		result.Added = 0
		result.Reused = 0
		for _, item := range prepared.snapshot.manifest.Items {
			if err := contextErr(ctx); err != nil {
				return installCancelled(err)
			}
			if err := prepared.root.VerifyItem(ctx, item); err == nil {
				result.Reused++
				continue
			} else if !errors.Is(err, os.ErrNotExist) {
				return fromLibraryError(err)
			}
			stagedItem, ok := staged[item.MD5]
			if !ok {
				return newError("integrity", "invalid_image", fmt.Sprintf("staged image %s is missing", item.MD5), "Retry the installation to download the missing image.")
			}
			data, err := readStagedItem(ctx, stagedItem.root, item)
			if err != nil {
				return err
			}
			if err := prepared.root.WriteRelativeAtomic(ctx, item.Filename, data); err != nil {
				return fromLibraryError(err)
			}
			if err := prepared.root.VerifyItem(ctx, item); err != nil {
				return fromLibraryError(err)
			}
			result.Added++
		}

		if installed {
			// A concurrent caller may already have published this exact
			// revision. The image checks above are still required because a
			// state file alone does not prove that every original remains.
			return nil
		}
		if err := prepared.root.EnsureRelativeDirectory(filepath.ToSlash(filepath.Join(".sticker", "packs"))); err != nil {
			return fromLibraryError(err)
		}
		if now == nil {
			now = time.Now
		}
		stateBytes, err := encodeInstalledState(installedState{
			SchemaVersion: 1,
			ID:            prepared.snapshot.pack.ID,
			Source:        prepared.source.Canonical,
			Revision:      prepared.snapshot.pack.Revision,
			InstalledAt:   now().UTC(),
			Manifest:      append([]byte(nil), prepared.snapshot.manifestBytes...),
		})
		if err != nil {
			return err
		}
		if err := prepared.root.WriteRelativeAtomic(ctx, filepath.ToSlash(filepath.Join(".sticker", "packs", prepared.snapshot.pack.ID+".json")), stateBytes); err != nil {
			return fromLibraryError(err)
		}
		return nil
	})
	if err != nil {
		return InstallResult{}, err
	}
	return result, nil
}

func encodeInstalledState(state installedState) ([]byte, error) {
	data, err := json.Marshal(state)
	if err != nil {
		return nil, wrapError("internal", "unexpected", "cannot encode installed pack state", "Retry the operation.", err)
	}
	return append(data, '\n'), nil
}

func readStagedItem(ctx context.Context, stageRoot string, item library.Item) ([]byte, error) {
	file, err := library.OpenRelative(ctx, stageRoot, filepath.FromSlash(item.Filename))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, newError("integrity", "invalid_image", fmt.Sprintf("staged image %s is missing", item.MD5), "Retry the installation to download the missing image.")
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, installCancelled(err)
		}
		return nil, fromLibraryError(err)
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(&contextReader{ctx: ctx, reader: file}, item.Size+1))
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, installCancelled(err)
		}
		return nil, wrapError("io", "read_failed", "cannot read staged image", "Check the staged image.", err)
	}
	if int64(len(data)) != item.Size {
		return nil, newError("integrity", "hash_mismatch", fmt.Sprintf("staged image %s does not match its declared size", item.MD5), "Retry the installation to download the image again.")
	}
	if err := verifyImageBytes(data, item); err != nil {
		return nil, err
	}
	return data, nil
}

func downloadImage(ctx context.Context, source Source, item library.Item, stagingRoot string, client *http.Client, backoff func(context.Context, time.Duration) error) error {
	if source.IsLocal() {
		file, err := library.OpenRelative(ctx, source.LocalRoot, filepath.FromSlash(item.Filename))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return newError("not_found", "source_not_found", fmt.Sprintf("source image %s was not found", item.MD5), "Check the source directory and manifest paths.")
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return installCancelled(err)
			}
			return fromLibraryError(err)
		}
		defer func() { _ = file.Close() }()
		return writeStagedStream(ctx, stagingRoot, file, item, false)
	}
	fetcher := newFetcher(client, backoff)
	return fetcher.downloadImage(ctx, source.manifestURL(item.Filename), item, stagingRoot)
}

func writeStagedStream(ctx context.Context, stagingRoot string, source io.Reader, item library.Item, network bool) error {
	stageLibrary, err := library.New(stagingRoot)
	if err != nil {
		return fromLibraryError(err)
	}
	digester := newImageDigestReader(source, network)
	err = stageLibrary.WriteRelativeAtomicFrom(ctx, item.Filename, digester, item.Size, func(count int64) error {
		return digester.validate(item, count)
	})
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return installCancelled(err)
	}
	if network {
		var transferErr *networkReadError
		if errors.As(err, &transferErr) {
			return &Error{Kind: "network", Subtype: "request_failed", Message: "source image transfer failed", Hint: "Retry the installation later.", Retryable: true, Err: transferErr.Err}
		}
	}
	if _, ok := err.(*Error); ok {
		return err
	}
	return fromLibraryError(err)
}

func (f fetcher) downloadImage(ctx context.Context, target string, item library.Item, stagingRoot string) error {
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := contextErr(ctx); err != nil {
			return installCancelled(err)
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			return newError("validation", "invalid_argument", "source URL is invalid", "Use an HTTPS source URL.")
		}
		response, err := f.client.Do(request)
		if err != nil {
			if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return installCancelled(ctx.Err())
			}
			if attempt < maxAttempts {
				if backoffErr := f.backoff(ctx, retryDelay(attempt)); backoffErr != nil {
					return installCancelled(backoffErr)
				}
				continue
			}
			subtype := "request_failed"
			var networkErr net.Error
			if errors.As(err, &networkErr) && networkErr.Timeout() {
				subtype = "timeout"
			}
			return &Error{Kind: "network", Subtype: subtype, Message: "source image request failed", Hint: "Check the network connection and source URL.", Retryable: true, Err: err}
		}
		statusRetry := retryableStatus(response.StatusCode)
		if response.StatusCode != http.StatusOK {
			_ = response.Body.Close()
			if statusRetry && attempt < maxAttempts {
				if backoffErr := f.backoff(ctx, retryAfter(response, attempt)); backoffErr != nil {
					return installCancelled(backoffErr)
				}
				continue
			}
			return &Error{Kind: "network", Subtype: "http_error", Message: fmt.Sprintf("source returned HTTP %d for image %s", response.StatusCode, item.MD5), Hint: "Check the source URL and retry later.", Retryable: statusRetry, Err: fmt.Errorf("HTTP %d", response.StatusCode)}
		}

		transferErr := writeStagedStream(ctx, stagingRoot, response.Body, item, true)
		_ = response.Body.Close()
		if transferErr == nil {
			return nil
		}
		var coded *Error
		if errors.As(transferErr, &coded) && coded.Kind == "network" && coded.Retryable && attempt < maxAttempts {
			if backoffErr := f.backoff(ctx, retryDelay(attempt)); backoffErr != nil {
				return installCancelled(backoffErr)
			}
			continue
		}
		return transferErr
	}
	return newError("network", "request_failed", "source image request failed", "Retry the operation later.")
}

type networkReadError struct {
	Err error
}

func (e *networkReadError) Error() string { return e.Err.Error() }

func (e *networkReadError) Unwrap() error { return e.Err }

type imageDigestReader struct {
	source  io.Reader
	network bool
	md5Hash hash.Hash
	shaHash hash.Hash
	header  []byte
}

func newImageDigestReader(source io.Reader, network bool) *imageDigestReader {
	return &imageDigestReader{source: source, network: network, md5Hash: md5.New(), shaHash: sha256.New()}
}

func (r *imageDigestReader) Read(p []byte) (int, error) {
	n, err := r.source.Read(p)
	if n > 0 {
		_, _ = r.md5Hash.Write(p[:n])
		_, _ = r.shaHash.Write(p[:n])
		if len(r.header) < 12 {
			need := min(12-len(r.header), n)
			r.header = append(r.header, p[:need]...)
		}
	}
	if err != nil && err != io.EOF && r.network {
		return n, &networkReadError{Err: err}
	}
	return n, err
}

func (r *imageDigestReader) validate(item library.Item, count int64) error {
	if count != item.Size || hex.EncodeToString(r.md5Hash.Sum(nil)) != item.MD5 || hex.EncodeToString(r.shaHash.Sum(nil)) != item.SHA256 {
		return newError("integrity", "hash_mismatch", fmt.Sprintf("source image %s does not match its manifest", item.MD5), "Use a stable source revision and retry the installation.")
	}
	if !imageSignature(r.header, item.Format) {
		return newError("integrity", "invalid_image", fmt.Sprintf("source image %s has an invalid %s signature", item.MD5, item.Format), "Use the original image in its declared format.")
	}
	return nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func verifyImageBytes(data []byte, item library.Item) error {
	md5Sum := md5.Sum(data)
	shaSum := sha256.Sum256(data)
	if hex.EncodeToString(md5Sum[:]) != item.MD5 || hex.EncodeToString(shaSum[:]) != item.SHA256 {
		return newError("integrity", "hash_mismatch", fmt.Sprintf("staged image %s does not match its manifest", item.MD5), "Retry the installation to download the image again.")
	}
	if !imageSignature(data, item.Format) {
		return newError("integrity", "invalid_image", fmt.Sprintf("staged image %s has an invalid %s signature", item.MD5, item.Format), "Use the original image in its declared format.")
	}
	return nil
}

func imageSignature(data []byte, format string) bool {
	switch format {
	case "gif":
		return len(data) >= 6 && (string(data[:6]) == "GIF87a" || string(data[:6]) == "GIF89a")
	case "png":
		return len(data) >= 8 && string(data[:8]) == "\x89PNG\r\n\x1a\n"
	case "jpg":
		return len(data) >= 3 && data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff
	case "webp":
		return len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP"
	default:
		return false
	}
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
