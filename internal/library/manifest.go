package library

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	ManifestName       = "manifest.json"
	EmoticonsDirectory = "emoticons"
	MaxManifestBytes   = 8 << 20
	MaxImageBytes      = 32 << 20
	MaxManifestItems   = 20_000
	MaxCaptionBytes    = 4 << 10
)

// Limits bounds input accepted by the library. Zero fields use the defaults.
type Limits struct {
	ManifestBytes int64
	ImageBytes    int64
	Items         int
	CaptionBytes  int
}

func DefaultLimits() Limits {
	return Limits{ManifestBytes: MaxManifestBytes, ImageBytes: MaxImageBytes, Items: MaxManifestItems, CaptionBytes: MaxCaptionBytes}
}

func (l Limits) withDefaults() Limits {
	d := DefaultLimits()
	if l.ManifestBytes <= 0 {
		l.ManifestBytes = d.ManifestBytes
	}
	if l.ImageBytes <= 0 {
		l.ImageBytes = d.ImageBytes
	}
	if l.Items <= 0 {
		l.Items = d.Items
	}
	if l.CaptionBytes <= 0 {
		l.CaptionBytes = d.CaptionBytes
	}
	return l
}

// Item is one standard v1 manifest entry. Filename is relative to the library root.
type Item struct {
	MD5      string `json:"md5"`
	SHA256   string `json:"sha256"`
	Filename string `json:"filename"`
	Format   string `json:"format"`
	Size     int64  `json:"size"`
	Caption  string `json:"caption,omitempty"`
}

// Manifest is the existing standard v1 format shared by public packs and personal libraries.
type Manifest struct {
	SchemaVersion int    `json:"schema_version"`
	Collection    string `json:"collection"`
	Items         []Item `json:"items"`
}

// Library confines all paths used by this package to Root.
type Library struct {
	Root   string
	Limits Limits
	Hooks  Hooks
}

// Hooks make filesystem failure boundaries testable without changing production behavior.
type Hooks struct {
	BeforeRename   func(string) error
	AfterRename    func(string) error
	SyncDirectory  func(string) error
	BeforeManifest func() error
}

func New(root string) (*Library, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, wrapError("validation", "unsafe_path", "Choose a local library directory.", err)
	}
	return &Library{Root: filepath.Clean(absolute), Limits: DefaultLimits()}, nil
}

// ValidateManifest validates metadata without reading the referenced files.
func ValidateManifest(manifest Manifest, limits Limits) error {
	limits = limits.withDefaults()
	if manifest.SchemaVersion != 1 {
		return errorf("integrity", "invalid_manifest", "Use a standard v1 manifest.", "unsupported manifest schema version %d", manifest.SchemaVersion)
	}
	if manifest.Collection == "" || !utf8.ValidString(manifest.Collection) {
		return errorf("integrity", "invalid_manifest", "Set a non-empty collection name.", "manifest collection is invalid")
	}
	if manifest.Items == nil {
		return errorf("integrity", "invalid_manifest", "Set items to a JSON array.", "manifest items field is missing or null")
	}
	if len(manifest.Items) > limits.Items {
		return errorf("integrity", "invalid_manifest", "Reduce the manifest to the supported item limit.", "manifest contains %d items; maximum is %d", len(manifest.Items), limits.Items)
	}
	seen := make(map[string]struct{}, len(manifest.Items))
	for i, item := range manifest.Items {
		if !isLowerHex(item.MD5, md5.Size) {
			return errorf("integrity", "invalid_manifest", "Check the item MD5 value.", "item %d has an invalid md5", i)
		}
		if _, ok := seen[item.MD5]; ok {
			return errorf("integrity", "invalid_manifest", "Remove duplicate item IDs.", "manifest repeats item %s", item.MD5)
		}
		seen[item.MD5] = struct{}{}
		if !isLowerHex(item.SHA256, sha256.Size) {
			return errorf("integrity", "invalid_manifest", "Check the item SHA-256 value.", "item %s has an invalid sha256", item.MD5)
		}
		if item.Format != "gif" && item.Format != "png" && item.Format != "jpg" && item.Format != "webp" {
			return errorf("validation", "unsupported_format", "Use gif, png, jpg, or webp.", "item %s has unsupported format %q", item.MD5, item.Format)
		}
		if item.Filename != filepath.ToSlash(filepath.Join(EmoticonsDirectory, item.MD5+"."+item.Format)) {
			return errorf("integrity", "invalid_manifest", "Use emoticons/<md5>.<format> filenames.", "item %s has an invalid filename", item.MD5)
		}
		if item.Size <= 0 || item.Size > limits.ImageBytes {
			return errorf("integrity", "invalid_manifest", "Check the declared image size.", "item %s has size %d outside the supported range", item.MD5, item.Size)
		}
		if !utf8.ValidString(item.Caption) || len([]byte(item.Caption)) > limits.CaptionBytes {
			return errorf("integrity", "invalid_manifest", "Shorten the caption to the supported UTF-8 limit.", "item %s has an invalid caption", item.MD5)
		}
	}
	return nil
}

func isLowerHex(value string, decodedBytes int) bool {
	if len(value) != decodedBytes*2 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

// ReadManifest reads the root manifest. A missing manifest is an empty personal library;
// an existing malformed manifest is always an integrity error.
func (l *Library) ReadManifest(ctx context.Context) (Manifest, error) {
	if err := contextErr(ctx); err != nil {
		return Manifest{}, err
	}
	if err := l.ensureRoot(false); err != nil {
		return Manifest{}, err
	}
	lock, err := acquireReadLockIfPresent(ctx, l.Root, l.lockTimeout())
	if err != nil {
		return Manifest{}, err
	}
	if lock != nil {
		defer func() { _ = lock.Close() }()
	}
	return l.readManifestUnlocked(ctx)
}

func (l *Library) readManifestUnlocked(ctx context.Context) (Manifest, error) {
	path, err := l.rootPath(ManifestName)
	if err != nil {
		return Manifest{}, err
	}
	if err := rejectExistingSymlink(path); err != nil {
		return Manifest{}, err
	}
	data, err := readBoundedRelative(ctx, l.Root, ManifestName, l.Limits.withDefaults().ManifestBytes)
	if errors.Is(err, os.ErrNotExist) {
		return Manifest{SchemaVersion: 1, Collection: "personal", Items: []Item{}}, nil
	}
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return Manifest{}, errorf("cancelled", "interrupted", "Retry the operation when ready.", "manifest read cancelled")
		}
		return Manifest{}, wrapError("io", "read_failed", "Check the library manifest permissions.", err)
	}
	manifest, err := decodeManifest(data, l.Limits)
	if err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// ItemPath resolves a manifest item's standard filename beneath the library root.
// It checks path components but does not read the image; use ReadItem when content
// integrity must be established before returning a usable path.
func (l *Library) ItemPath(item Item) (string, error) { return l.itemPath(item) }

func decodeManifest(data []byte, limits Limits) (Manifest, error) {
	if len(data) > int(limits.withDefaults().ManifestBytes) {
		return Manifest{}, errorf("integrity", "invalid_manifest", "Use a smaller manifest.", "manifest exceeds the %d byte limit", limits.ManifestBytes)
	}
	if !utf8.Valid(data) {
		return Manifest{}, errorf("integrity", "invalid_manifest", "Use valid UTF-8 JSON.", "manifest is not valid UTF-8")
	}
	if err := rejectDuplicateJSONKeys(data); err != nil {
		return Manifest{}, errorf("integrity", "invalid_manifest", "Remove duplicate JSON keys.", "manifest has duplicate JSON keys")
	}
	var manifest Manifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, errorf("integrity", "invalid_manifest", "Repair or replace the manifest.", "manifest JSON is invalid: %v", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return Manifest{}, errorf("integrity", "invalid_manifest", "Repair or replace the manifest.", "manifest contains trailing JSON")
	}
	if err := ValidateManifest(manifest, limits); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// Validate checks every referenced file and its declared format, size, MD5, and SHA-256.
func (l *Library) Validate(ctx context.Context, manifest Manifest) error {
	if err := ValidateManifest(manifest, l.Limits); err != nil {
		return err
	}
	for _, item := range manifest.Items {
		if err := l.verifyItem(ctx, item); err != nil {
			return err
		}
	}
	return nil
}

// ReadItem locates and verifies one item, returning its absolute path only after
// all declared integrity checks have passed. It performs no image reads for other items.
func (l *Library) ReadItem(ctx context.Context, id string) (Item, string, error) {
	if !isLowerHex(id, md5.Size) {
		return Item{}, "", errorf("validation", "invalid_argument", "Use a lowercase 32-character MD5 ID.", "invalid item ID")
	}
	manifest, err := l.ReadManifest(ctx)
	if err != nil {
		return Item{}, "", err
	}
	for _, item := range manifest.Items {
		if item.MD5 == id {
			if err := l.verifyItem(ctx, item); err != nil {
				return Item{}, "", err
			}
			path, err := l.itemPath(item)
			return item, path, err
		}
	}
	return Item{}, "", errorf("not_found", "item_not_found", "Choose an ID listed by the library.", "item %s was not found", id)
}

func (l *Library) verifyItem(ctx context.Context, item Item) error {
	_, err := l.itemPath(item)
	if err != nil {
		return err
	}
	file, err := openRelativeNoFollow(l.Root, filepath.FromSlash(item.Filename))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errorf("integrity", "invalid_image", "Restore the missing image or remove its manifest entry.", "image %s is missing", item.MD5)
		}
		return wrapError("io", "read_failed", "Check the image permissions.", err)
	}
	defer func() { _ = file.Close() }()
	return verifyFileHandle(ctx, file, item, l.Limits)
}

// VerifyFile validates one standard manifest item at an already resolved path.
// The caller remains responsible for resolving that path beneath its source root.
func VerifyFile(ctx context.Context, path string, item Item, limits Limits) error {
	if err := ValidateManifest(Manifest{SchemaVersion: 1, Collection: "verify", Items: []Item{item}}, limits); err != nil {
		return err
	}
	file, err := openNoFollow(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errorf("integrity", "invalid_image", "Restore the missing image or remove its manifest entry.", "image %s is missing", item.MD5)
		}
		return wrapError("io", "read_failed", "Check the image permissions.", err)
	}
	defer func() { _ = file.Close() }()
	return verifyFileHandle(ctx, file, item, limits)
}

func verifyFileHandle(ctx context.Context, file *os.File, item Item, limits Limits) error {
	md5Hash := md5.New()
	shaHash := sha256.New()
	reader := io.Reader(file)
	if _, err := copyContext(ctx, io.MultiWriter(md5Hash, shaHash), io.LimitReader(reader, limits.withDefaults().ImageBytes+1)); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return errorf("cancelled", "interrupted", "Retry the operation when ready.", "image verification cancelled")
		}
		return wrapError("io", "read_failed", "Check the image file.", err)
	}
	info, err := file.Stat()
	if err != nil {
		return wrapError("io", "read_failed", "Check the image file.", err)
	}
	if info.Size() != item.Size || info.Size() > limits.withDefaults().ImageBytes {
		return errorf("integrity", "hash_mismatch", "Restore the original image or remove the stale entry.", "image %s has size %d; manifest declares %d", item.MD5, info.Size(), item.Size)
	}
	if hex.EncodeToString(md5Hash.Sum(nil)) != item.MD5 || hex.EncodeToString(shaHash.Sum(nil)) != item.SHA256 {
		return errorf("integrity", "hash_mismatch", "Restore the original image or remove the stale entry.", "image %s does not match its manifest hashes", item.MD5)
	}
	if !hasImageSignature(file, item.Format) {
		return errorf("integrity", "invalid_image", "Restore the original image in its declared format.", "image %s does not have a %s signature", item.MD5, item.Format)
	}
	return nil
}

// WriteManifest validates metadata and atomically replaces the root manifest.
func (l *Library) WriteManifest(ctx context.Context, manifest Manifest) error {
	if err := ValidateManifest(manifest, l.Limits); err != nil {
		return err
	}
	manifest.Items = append([]Item{}, manifest.Items...)
	sort.Slice(manifest.Items, func(i, j int) bool { return manifest.Items[i].MD5 < manifest.Items[j].MD5 })
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return wrapError("internal", "unexpected", "Retry the operation.", err)
	}
	data = append(data, '\n')
	if int64(len(data)) > l.Limits.withDefaults().ManifestBytes {
		return errorf("validation", "output_limit", "Reduce the manifest size.", "encoded manifest exceeds the %d byte limit", l.Limits.ManifestBytes)
	}
	if err := l.ensureRoot(true); err != nil {
		return err
	}
	lock, err := acquireLock(ctx, l.Root, true, l.lockTimeout())
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	if _, err := l.readManifestUnlocked(ctx); err != nil {
		return err
	}
	return l.atomicReplace(ctx, filepath.Join(l.Root, ManifestName), data)
}

// UpdateManifest executes fn under the cross-process write lock and atomically publishes one manifest.
func (l *Library) UpdateManifest(ctx context.Context, fn func(Manifest) (Manifest, error)) error {
	if fn == nil {
		return errorf("validation", "invalid_argument", "Provide a manifest update function.", "manifest update function is nil")
	}
	if err := l.ensureRoot(true); err != nil {
		return err
	}
	lock, err := acquireLock(ctx, l.Root, true, l.lockTimeout())
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	current, err := l.readManifestUnlocked(ctx)
	if err != nil {
		return err
	}
	next, err := fn(current)
	if err != nil {
		return err
	}
	return l.writeManifestUnlocked(ctx, next)
}

func (l *Library) writeManifestUnlocked(ctx context.Context, manifest Manifest) error {
	if err := ValidateManifest(manifest, l.Limits); err != nil {
		return err
	}
	manifest.Items = append([]Item{}, manifest.Items...)
	sort.Slice(manifest.Items, func(i, j int) bool { return manifest.Items[i].MD5 < manifest.Items[j].MD5 })
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return wrapError("internal", "unexpected", "Retry the operation.", err)
	}
	data = append(data, '\n')
	if int64(len(data)) > l.Limits.withDefaults().ManifestBytes {
		return errorf("validation", "output_limit", "Reduce the manifest size.", "encoded manifest exceeds the %d byte limit", l.Limits.ManifestBytes)
	}
	path, err := l.rootPath(ManifestName)
	if err != nil {
		return err
	}
	return l.atomicReplace(ctx, path, data)
}

func (l *Library) lockTimeout() time.Duration { return 5 * time.Second }

func contextErr(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return errorf("cancelled", "interrupted", "Retry the operation when ready.", "operation cancelled: %v", err)
	}
	return nil
}

func copyContext(ctx context.Context, dst io.Writer, src io.Reader) (int64, error) {
	buffer := make([]byte, 32*1024)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		n, readErr := src.Read(buffer)
		if n > 0 {
			written, writeErr := dst.Write(buffer[:n])
			total += int64(written)
			if writeErr != nil {
				return total, writeErr
			}
			if written != n {
				return total, io.ErrShortWrite
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				return total, nil
			}
			return total, readErr
		}
	}
}
