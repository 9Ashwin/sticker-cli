package packs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/9Ashwin/sticker-cli/internal/library"
)

const (
	maxCatalogBytes = 8 << 20
	maxCacheBytes   = 8 << 20
	requestTimeout  = 60 * time.Second
	maxAttempts     = 3
)

var (
	packIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
)

// Pack is the metadata shown by packs list. It intentionally excludes item
// bytes and image paths; discovering a source only reads catalog metadata.
type Pack struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Revision    string `json:"revision"`
	Count       int    `json:"count"`
	Size        int64  `json:"size"`
	Installed   bool   `json:"installed"`
}

// Result is the data returned by a catalog discovery operation.
type Result struct {
	Source    string    `json:"source"`
	Items     []Pack    `json:"items"`
	FetchedAt time.Time `json:"-"`
	Stale     bool      `json:"stale"`
}

// FetchedAtString returns the stable wire representation used by the CLI.
func (r Result) FetchedAtString() string { return r.FetchedAt.UTC().Format(time.RFC3339Nano) }

// Options controls source discovery. Home is only used for the cache and may
// be empty, in which case DefaultHome resolves the platform data directory.
type Options struct {
	Home       string
	Source     string
	Offline    bool
	Now        func() time.Time
	HTTPClient *http.Client
	Backoff    func(context.Context, time.Duration) error
}

// Discover reads and validates a source catalog, using a source-keyed cache.
// Online discovery fetches packs.json and the referenced manifests, but never
// fetches any image files. Offline discovery reads only an existing cache.
func Discover(ctx context.Context, options Options) (Result, error) {
	if err := contextErr(ctx); err != nil {
		return Result{}, wrapError("cancelled", "interrupted", "operation cancelled", "Retry the operation when ready.", err)
	}
	source, err := Resolve(options.Source)
	if err != nil {
		return Result{}, err
	}
	home, err := resolveHome(options.Home)
	if err != nil {
		return Result{}, err
	}
	now := time.Now
	if options.Now != nil {
		now = options.Now
	}
	if options.Offline {
		result, err := readCache(ctx, source, home, now())
		if err != nil {
			return Result{}, err
		}
		return withInstalledState(home, source, result)
	}
	if source.IsLocal() {
		if err := source.validateLocal(); err != nil {
			return Result{}, err
		}
	}
	catalog, err := fetchCatalog(ctx, source, options)
	if err != nil {
		return Result{}, err
	}
	fetchedAt := now().UTC()
	result := Result{Source: source.Canonical, Items: catalog, FetchedAt: fetchedAt}
	if err := writeCache(source, home, cacheRecord{SchemaVersion: 1, Source: source.Canonical, FetchedAt: fetchedAt, Items: catalog}); err != nil {
		return Result{}, err
	}
	return withInstalledState(home, source, result)
}

type directory struct {
	SchemaVersion int     `json:"schema_version"`
	Packs         []entry `json:"packs"`
}

type entry struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	Manifest       string `json:"manifest"`
	ManifestSHA256 string `json:"manifest_sha256"`
	Count          int    `json:"count"`
	Size           int64  `json:"size"`
}

type cacheRecord struct {
	SchemaVersion int       `json:"schema_version"`
	Source        string    `json:"source"`
	FetchedAt     time.Time `json:"fetched_at"`
	Items         []Pack    `json:"items"`
}

func fetchCatalog(ctx context.Context, source Source, options Options) ([]Pack, error) {
	descriptors, err := fetchDirectory(ctx, source, options)
	if err != nil {
		return nil, err
	}
	result := make([]Pack, 0, len(descriptors))
	for _, descriptor := range descriptors {
		manifestBytes, err := readManifest(ctx, source, descriptor.Manifest, options)
		if err != nil {
			return nil, err
		}
		if err := validateManifestBytes(manifestBytes, descriptor); err != nil {
			return nil, err
		}
		result = append(result, Pack{
			ID:          descriptor.ID,
			Name:        descriptor.Name,
			Description: descriptor.Description,
			Revision:    descriptor.ManifestSHA256,
			Count:       descriptor.Count,
			Size:        descriptor.Size,
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

// packSnapshot is one validated pack descriptor and its raw v1 manifest.
// Keeping the raw bytes is important because the descriptor revision is the
// SHA-256 of the bytes published by the source, rather than a re-encoded JSON
// representation.
type packSnapshot struct {
	descriptor    entry
	pack          Pack
	manifest      library.Manifest
	manifestBytes []byte
}

func fetchDirectory(ctx context.Context, source Source, options Options) ([]entry, error) {
	var directoryBytes []byte
	var err error
	if source.IsLocal() {
		directoryBytes, err = readLocal(ctx, source.LocalRoot, "packs.json", maxCatalogBytes)
	} else {
		fetcher := newFetcher(options.HTTPClient, options.Backoff)
		directoryBytes, err = fetcher.get(ctx, source.manifestURL("packs.json"), maxCatalogBytes)
	}
	if err != nil {
		return nil, err
	}
	var directory directory
	if err := decodeStrict(directoryBytes, &directory); err != nil {
		return nil, invalidCollection("packs.json is invalid: %v", err)
	}
	if directory.SchemaVersion != 1 || directory.Packs == nil {
		return nil, invalidCollection("packs.json must declare schema_version 1 and a packs array")
	}
	seen := make(map[string]struct{}, len(directory.Packs))
	for _, descriptor := range directory.Packs {
		if err := validateEntry(descriptor, seen); err != nil {
			return nil, err
		}
	}
	return directory.Packs, nil
}

func fetchPackSnapshot(ctx context.Context, source Source, options Options, id string) (packSnapshot, error) {
	descriptors, err := fetchDirectory(ctx, source, options)
	if err != nil {
		return packSnapshot{}, err
	}
	for _, descriptor := range descriptors {
		if descriptor.ID != id {
			continue
		}
		manifestBytes, err := readManifest(ctx, source, descriptor.Manifest, options)
		if err != nil {
			return packSnapshot{}, err
		}
		if err := validateManifestBytes(manifestBytes, descriptor); err != nil {
			return packSnapshot{}, err
		}
		manifest, err := decodeManifest(manifestBytes)
		if err != nil {
			return packSnapshot{}, newError("integrity", "invalid_manifest", fmt.Sprintf("manifest for pack %q is invalid: %v", id, err), "Repair the source manifest.")
		}
		return packSnapshot{
			descriptor:    descriptor,
			pack:          Pack{ID: descriptor.ID, Name: descriptor.Name, Description: descriptor.Description, Revision: descriptor.ManifestSHA256, Count: descriptor.Count, Size: descriptor.Size},
			manifest:      manifest,
			manifestBytes: append([]byte(nil), manifestBytes...),
		}, nil
	}
	return packSnapshot{}, newError("not_found", "pack_not_found", fmt.Sprintf("pack %s was not found in the source", id), "Run packs list to see available pack IDs.")
}

func validateEntry(value entry, seen map[string]struct{}) error {
	if !packIDPattern.MatchString(value.ID) {
		return invalidCollection("pack ID %q is invalid", value.ID)
	}
	if _, ok := seen[value.ID]; ok {
		return invalidCollection("packs.json repeats pack ID %q", value.ID)
	}
	seen[value.ID] = struct{}{}
	if value.Name == "" || !utf8.ValidString(value.Name) || !utf8.ValidString(value.Description) {
		return invalidCollection("pack %q has invalid name or description", value.ID)
	}
	if !isLowerHex(value.ManifestSHA256, sha256.Size) {
		return invalidCollection("pack %q has an invalid manifest_sha256", value.ID)
	}
	if value.Count < 0 || value.Size < 0 {
		return invalidCollection("pack %q has a negative count or size", value.ID)
	}
	if !validManifestPath(value.Manifest, value.ID) {
		return newError("validation", "unsafe_path", fmt.Sprintf("pack %q has an unsafe manifest path", value.ID), "Use manifest.json or packs/<id>.json inside the source directory.")
	}
	return nil
}

func validManifestPath(value, id string) bool {
	if value == "manifest.json" {
		return id == "all"
	}
	return value == "packs/"+id+".json"
}

func readManifest(ctx context.Context, source Source, relative string, options Options) ([]byte, error) {
	if err := contextErr(ctx); err != nil {
		return nil, wrapError("cancelled", "interrupted", "operation cancelled", "Retry the operation when ready.", err)
	}
	if source.IsLocal() {
		return readLocal(ctx, source.LocalRoot, relative, maxCatalogBytes)
	}
	return newFetcher(options.HTTPClient, options.Backoff).get(ctx, source.manifestURL(relative), maxCatalogBytes)
}

func validateManifestBytes(data []byte, descriptor entry) error {
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != descriptor.ManifestSHA256 {
		return integrityError("manifest for pack %q has sha256 %s, expected %s", descriptor.ID, got, descriptor.ManifestSHA256)
	}
	manifest, err := decodeManifest(data)
	if err != nil {
		return newError("integrity", "invalid_manifest", fmt.Sprintf("manifest for pack %q is invalid: %v", descriptor.ID, err), "Repair the source manifest.")
	}
	if err := library.ValidateManifest(manifest, library.DefaultLimits()); err != nil {
		var libraryErr *library.Error
		if errors.As(err, &libraryErr) {
			return &Error{
				Kind:      libraryErr.Kind,
				Subtype:   libraryErr.Subtype,
				Message:   fmt.Sprintf("manifest for pack %q is invalid: %s", descriptor.ID, libraryErr.Message),
				Hint:      libraryErr.Hint,
				Retryable: libraryErr.Retryable,
				Err:       err,
			}
		}
		return newError("integrity", "invalid_manifest", fmt.Sprintf("manifest for pack %q is invalid: %v", descriptor.ID, err), "Repair the source manifest.")
	}
	if len(manifest.Items) != descriptor.Count {
		return integrityError("pack %q declares %d items but manifest contains %d", descriptor.ID, descriptor.Count, len(manifest.Items))
	}
	var size int64
	for _, item := range manifest.Items {
		if item.Size > (1<<63-1)-size {
			return integrityError("pack %q image size total overflows", descriptor.ID)
		}
		size += item.Size
	}
	if size != descriptor.Size {
		return integrityError("pack %q declares %d bytes but manifest contains %d", descriptor.ID, descriptor.Size, size)
	}
	return nil
}

func decodeManifest(data []byte) (library.Manifest, error) {
	if err := rejectDuplicateJSONKeys(data); err != nil {
		return library.Manifest{}, err
	}
	var manifest library.Manifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&manifest); err != nil {
		return library.Manifest{}, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return library.Manifest{}, errors.New("trailing JSON")
	}
	return manifest, nil
}

func invalidCollection(format string, args ...any) *Error {
	return newError("integrity", "invalid_collection", fmt.Sprintf(format, args...), "Repair the source packs.json and manifests.")
}

func integrityError(format string, args ...any) *Error {
	return newError("integrity", "hash_mismatch", fmt.Sprintf(format, args...), "Use a stable source revision and retry discovery.")
}

func isLowerHex(value string, size int) bool {
	if len(value) != size*2 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func contextErr(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func resolveHome(value string) (string, error) {
	if value == "" {
		value = os.Getenv("STICKER_HOME")
	}
	if value == "" {
		config, err := os.UserConfigDir()
		if err != nil {
			return "", wrapError("io", "read_failed", "cannot determine the user data directory", "Set STICKER_HOME or --home.", err)
		}
		value = filepath.Join(config, "sticker")
	}
	abs, err := filepath.Abs(value)
	if err != nil {
		return "", wrapError("validation", "unsafe_path", "cannot resolve home directory", "Use a valid local data directory.", err)
	}
	return filepath.Clean(abs), nil
}

func readLocal(ctx context.Context, root, relative string, limit int64) ([]byte, error) {
	if err := validateRelativePath(relative); err != nil {
		return nil, err
	}
	data, err := library.ReadRelative(ctx, root, relative, limit)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, newError("not_found", "source_not_found", fmt.Sprintf("source file %s was not found", relative), "Check the source directory and manifest paths.")
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, wrapError("cancelled", "interrupted", "operation cancelled", "Retry the operation when ready.", err)
		}
		var libraryErr *library.Error
		if errors.As(err, &libraryErr) {
			return nil, &Error{Kind: libraryErr.Kind, Subtype: libraryErr.Subtype, Message: fmt.Sprintf("cannot read source file %s: %s", relative, libraryErr.Message), Hint: libraryErr.Hint, Retryable: libraryErr.Retryable, Err: err}
		}
		return nil, wrapError("io", "read_failed", fmt.Sprintf("cannot read source file %s", relative), "Check the source permissions.", err)
	}
	return data, nil
}

func validateRelativePath(value string) error {
	if value == "" || strings.Contains(value, "\\") || filepath.IsAbs(filepath.FromSlash(value)) || filepath.VolumeName(value) != "" {
		return newError("validation", "unsafe_path", "source manifest path is unsafe", "Use a relative path beneath the source directory.")
	}
	clean := path.Clean(value)
	if clean != value || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return newError("validation", "unsafe_path", "source manifest path escapes its directory", "Use a relative path beneath the source directory.")
	}
	return nil
}

func writeCache(source Source, home string, record cacheRecord) error {
	directory := filepath.Join(home, cacheDirectory)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return wrapError("io", "write_failed", "cannot create catalog cache", "Choose a writable --home directory.", err)
	}
	data, err := json.Marshal(record)
	if err != nil {
		return wrapError("internal", "unexpected", "cannot encode catalog cache", "Retry the operation.", err)
	}
	data = append(data, '\n')
	temporary, err := os.CreateTemp(directory, ".catalog-*.tmp")
	if err != nil {
		return wrapError("io", "write_failed", "cannot create catalog cache", "Choose a writable --home directory.", err)
	}
	temporaryName := temporary.Name()
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryName)
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return wrapError("io", "write_failed", "cannot protect catalog cache", "Choose a writable --home directory.", err)
	}
	if _, err := temporary.Write(data); err != nil {
		return wrapError("io", "write_failed", "cannot write catalog cache", "Choose a writable --home directory.", err)
	}
	if err := temporary.Sync(); err != nil {
		return wrapError("io", "write_failed", "cannot sync catalog cache", "Check available disk space.", err)
	}
	if err := temporary.Close(); err != nil {
		return wrapError("io", "write_failed", "cannot close catalog cache", "Check the cache directory.", err)
	}
	if err := os.Rename(temporaryName, source.cachePath(home)); err != nil {
		return wrapError("io", "write_failed", "cannot publish catalog cache", "Check the cache directory permissions.", err)
	}
	return nil
}

func readCache(ctx context.Context, source Source, home string, now time.Time) (Result, error) {
	if err := contextErr(ctx); err != nil {
		return Result{}, wrapError("cancelled", "interrupted", "operation cancelled", "Retry the operation when ready.", err)
	}
	data, err := os.ReadFile(source.cachePath(home))
	if err != nil {
		if os.IsNotExist(err) {
			return Result{}, newError("not_found", "source_not_found", "no cached catalog exists for this source", "Run packs list without --offline once while online.")
		}
		return Result{}, wrapError("io", "read_failed", "cannot read cached catalog", "Check the cache file permissions.", err)
	}
	if len(data) > maxCacheBytes {
		return Result{}, invalidCollection("cached catalog exceeds the %d byte limit", maxCacheBytes)
	}
	var record cacheRecord
	if err := decodeStrict(data, &record); err != nil {
		return Result{}, invalidCollection("cached catalog is invalid: %v", err)
	}
	if record.SchemaVersion != 1 || record.Source != source.Canonical || record.Items == nil || record.FetchedAt.IsZero() {
		return Result{}, invalidCollection("cached catalog metadata is invalid")
	}
	if err := validateCachedItems(record.Items); err != nil {
		return Result{}, err
	}
	stale := !now.Before(record.FetchedAt) && now.Sub(record.FetchedAt) > cacheTTL
	return Result{Source: record.Source, Items: record.Items, FetchedAt: record.FetchedAt, Stale: stale}, nil
}

type installedState struct {
	SchemaVersion int             `json:"schema_version"`
	ID            string          `json:"id"`
	Source        string          `json:"source"`
	Revision      string          `json:"revision"`
	InstalledAt   time.Time       `json:"installed_at"`
	Manifest      json.RawMessage `json:"manifest"`
	// ManifestRaw keeps the exact source bytes used by Revision. Manifest is
	// retained for compatibility with states written before this field existed.
	ManifestRaw []byte `json:"manifest_raw,omitempty"`
}

func withInstalledState(home string, source Source, result Result) (Result, error) {
	installed, err := readInstalledStates(home, source)
	if err != nil {
		return Result{}, err
	}
	for index := range result.Items {
		result.Items[index].Installed = installed[result.Items[index].ID]
	}
	return result, nil
}

func readInstalledStates(home string, source Source) (map[string]bool, error) {
	directory := filepath.Join(home, ".sticker", "packs")
	entries, err := os.ReadDir(directory)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]bool{}, nil
		}
		return nil, wrapError("io", "read_failed", "cannot read installed pack states", "Check the installed pack state directory.", err)
	}
	result := make(map[string]bool)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		if !packIDPattern.MatchString(id) {
			return nil, invalidCollection("installed pack state has an invalid filename %q", entry.Name())
		}
		relative := filepath.ToSlash(filepath.Join(".sticker", "packs", entry.Name()))
		data, err := readHomeFile(home, relative, maxCacheBytes)
		if err != nil {
			return nil, err
		}
		var state installedState
		if err := decodeStrict(data, &state); err != nil {
			return nil, invalidCollection("installed pack state %q is invalid: %v", id, err)
		}
		if state.SchemaVersion != 1 || state.ID != id || state.Source == "" || !isLowerHex(state.Revision, sha256.Size) {
			return nil, invalidCollection("installed pack state %q has invalid metadata", id)
		}
		resolved, err := Resolve(state.Source)
		if err != nil || resolved.Canonical != source.Canonical {
			continue
		}
		result[id] = true
	}
	return result, nil
}

func readHomeFile(root, relative string, limit int64) ([]byte, error) {
	if err := validateRelativePath(relative); err != nil {
		return nil, err
	}
	current := filepath.Clean(root)
	for _, part := range strings.Split(filepath.FromSlash(relative), string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if statErr != nil {
			if os.IsNotExist(statErr) {
				return nil, newError("not_found", "source_not_found", "installed pack state was not found", "Repair the local pack state.")
			}
			return nil, wrapError("io", "read_failed", "cannot inspect installed pack state", "Check the local pack state permissions.", statErr)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, newError("validation", "unsafe_path", "installed pack state contains a symbolic link", "Remove symbolic links from the local data directory.")
		}
	}
	data, err := os.ReadFile(current)
	if err != nil {
		return nil, wrapError("io", "read_failed", "cannot read installed pack state", "Check the local pack state permissions.", err)
	}
	if int64(len(data)) > limit {
		return nil, invalidCollection("installed pack state exceeds the %d byte limit", limit)
	}
	return data, nil
}

func validateCachedItems(items []Pack) error {
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if err := validateEntry(entry{ID: item.ID, Name: item.Name, Description: item.Description, Manifest: "packs/" + item.ID + ".json", ManifestSHA256: item.Revision, Count: item.Count, Size: item.Size}, seen); err != nil {
			return invalidCollection("cached catalog contains invalid pack metadata: %v", err)
		}
	}
	return nil
}

type fetcher struct {
	client  *http.Client
	backoff func(context.Context, time.Duration) error
}

func newFetcher(client *http.Client, backoff func(context.Context, time.Duration) error) fetcher {
	if client == nil {
		client = &http.Client{}
	}
	clientCopy := *client
	if clientCopy.Timeout <= 0 || clientCopy.Timeout > requestTimeout {
		clientCopy.Timeout = requestTimeout
	}
	redirect := clientCopy.CheckRedirect
	clientCopy.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		if err := validateRedirect(next.URL); err != nil {
			return err
		}
		if redirect != nil {
			return redirect(next, via)
		}
		return nil
	}
	if backoff == nil {
		backoff = waitBackoff
	}
	return fetcher{client: &clientCopy, backoff: backoff}
}

func (f fetcher) get(ctx context.Context, target string, limit int64) ([]byte, error) {
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := contextErr(ctx); err != nil {
			return nil, wrapError("cancelled", "interrupted", "operation cancelled", "Retry the operation when ready.", err)
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			return nil, newError("validation", "invalid_argument", "source URL is invalid", "Use an HTTPS source URL.")
		}
		response, err := f.client.Do(request)
		if err != nil {
			if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return nil, wrapError("cancelled", "interrupted", "operation cancelled", "Retry the operation when ready.", ctx.Err())
			}
			if attempt < maxAttempts {
				if err := f.backoff(ctx, retryDelay(attempt)); err != nil {
					return nil, wrapError("cancelled", "interrupted", "operation cancelled", "Retry the operation when ready.", err)
				}
				continue
			}
			subtype := "request_failed"
			var networkErr net.Error
			if errors.As(err, &networkErr) && networkErr.Timeout() {
				subtype = "timeout"
			}
			return nil, &Error{Kind: "network", Subtype: subtype, Message: "source request failed", Hint: "Check the network connection and source URL.", Retryable: true, Err: err}
		}
		data, readErr := readResponse(response, limit)
		statusRetry := retryableStatus(response.StatusCode)
		_ = response.Body.Close()
		if readErr != nil {
			if statusRetry && attempt < maxAttempts {
				if err := f.backoff(ctx, retryAfter(response, attempt)); err != nil {
					return nil, wrapError("cancelled", "interrupted", "operation cancelled", "Retry the operation when ready.", err)
				}
				continue
			}
			if statusRetry {
				return nil, &Error{Kind: "network", Subtype: "http_error", Message: fmt.Sprintf("source returned HTTP %d", response.StatusCode), Hint: "Retry the operation later.", Retryable: true, Err: readErr}
			}
			return nil, readErr
		}
		if response.StatusCode != http.StatusOK {
			if statusRetry && attempt < maxAttempts {
				if err := f.backoff(ctx, retryAfter(response, attempt)); err != nil {
					return nil, wrapError("cancelled", "interrupted", "operation cancelled", "Retry the operation when ready.", err)
				}
				continue
			}
			return nil, &Error{Kind: "network", Subtype: "http_error", Message: fmt.Sprintf("source returned HTTP %d", response.StatusCode), Hint: "Check the source URL and retry later.", Retryable: statusRetry, Err: fmt.Errorf("HTTP %d", response.StatusCode)}
		}
		return data, nil
	}
	return nil, newError("network", "request_failed", "source request failed", "Retry the operation later.")
}

func readResponse(response *http.Response, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, wrapError("network", "request_failed", "source response could not be read", "Retry the operation later.", err)
	}
	if int64(len(data)) > limit {
		return nil, invalidCollection("source response exceeds the %d byte limit", limit)
	}
	return data, nil
}

func retryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || status == http.StatusBadGateway || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout
}

func retryAfter(response *http.Response, attempt int) time.Duration {
	value, err := strconv.Atoi(strings.TrimSpace(response.Header.Get("Retry-After")))
	if err == nil && value >= 0 {
		return min(time.Duration(value)*time.Second, 10*time.Second)
	}
	return retryDelay(attempt)
}

func retryDelay(attempt int) time.Duration {
	if attempt <= 1 {
		return time.Second
	}
	return 2 * time.Second
}

func waitBackoff(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func validateRedirect(value *url.URL) error {
	if value == nil || !strings.EqualFold(value.Scheme, "https") || value.Host == "" || value.User != nil || value.RawQuery != "" || value.Fragment != "" {
		return errors.New("redirect must remain an HTTPS URL without credentials or query parameters")
	}
	return nil
}

func decodeStrict(data []byte, value any) error {
	if err := rejectDuplicateJSONKeys(data); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}

func rejectDuplicateJSONKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := scanJSONValue(decoder); err != nil {
		return err
	}
	var token any
	if err := decoder.Decode(&token); err != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}

func scanJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]struct{}{}
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok {
				return errors.New("invalid object key")
			}
			if _, exists := seen[name]; exists {
				return errors.New("duplicate object key")
			}
			seen[name] = struct{}{}
			if err := scanJSONValue(decoder); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := scanJSONValue(decoder); err != nil {
				return err
			}
		}
	}
	_, err = decoder.Token()
	return err
}
