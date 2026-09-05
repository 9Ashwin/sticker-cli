package favorites

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/9Ashwin/sticker-cli/internal/library"
)

const exportPackID = "all"

type exportCommittedError struct{ err error }

func (e *exportCommittedError) Error() string { return e.err.Error() }

func (e *exportCommittedError) Unwrap() error { return e.err }

// ExportOptions controls a local export of the personal standard library.
type ExportOptions struct {
	Home        string
	Destination string
	DryRun      bool
}

// ExportResult reports the files that would be or were published by an
// export. Path is absolute so it can be passed directly to another command.
type ExportResult struct {
	Path   string `json:"path"`
	Count  int    `json:"count"`
	Size   int64  `json:"size"`
	DryRun bool   `json:"dry_run,omitempty"`
}

type exportPackDirectory struct {
	SchemaVersion int               `json:"schema_version"`
	Packs         []exportPackEntry `json:"packs"`
}

type exportPackEntry struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	Manifest       string `json:"manifest"`
	ManifestSHA256 string `json:"manifest_sha256"`
	Count          int    `json:"count"`
	Size           int64  `json:"size"`
}

// Export copies only the original files referenced by the personal manifest
// into a new standard v1 material directory. The directory is built beside
// the requested destination and published with a no-replace rename, so a
// failed export cannot expose a partial result and a competing creator cannot
// overwrite an existing destination.
func Export(ctx context.Context, options ExportOptions) (ExportResult, error) {
	if err := contextError(ctx); err != nil {
		return ExportResult{}, err
	}
	if options.Home == "" {
		return ExportResult{}, errorf("validation", "invalid_argument", "Choose a local data directory.", "favorites home is empty")
	}
	if options.Destination == "" {
		return ExportResult{}, errorf("validation", "invalid_argument", "Provide a new export directory.", "export destination is empty")
	}
	destination, err := absoluteExportPath(options.Destination)
	if err != nil {
		return ExportResult{}, err
	}
	if err := validateExportDestination(destination); err != nil {
		return ExportResult{}, err
	}

	root, err := library.New(options.Home)
	if err != nil {
		return ExportResult{}, err
	}
	var result ExportResult
	err = root.WithReadLock(ctx, func(manifest library.Manifest) error {
		collections, err := readCollections(ctx, root, manifest)
		if err != nil {
			return err
		}
		manifest.Items = append([]library.Item(nil), manifest.Items...)
		sort.Slice(manifest.Items, func(i, j int) bool { return manifest.Items[i].MD5 < manifest.Items[j].MD5 })
		if err := verifyExportItems(ctx, root, manifest.Items); err != nil {
			return err
		}

		result = ExportResult{Path: destination, Count: len(manifest.Items), Size: manifestSize(manifest.Items), DryRun: options.DryRun}
		if options.DryRun {
			return nil
		}
		return exportManifest(ctx, root, destination, manifest, collections)
	})
	if err != nil {
		return ExportResult{}, err
	}
	return result, nil
}

func exportManifest(ctx context.Context, root *library.Library, destination string, manifest library.Manifest, collections Collections) error {
	staging, err := createExportStaging(destination)
	if err != nil {
		return err
	}
	published := false
	defer func() {
		if !published {
			_ = cleanupExportStaging(staging)
		}
	}()

	stagingLibrary, err := library.New(staging)
	if err != nil {
		return err
	}
	if err := stagingLibrary.EnsureRelativeDirectoryNoState(library.EmoticonsDirectory); err != nil {
		return err
	}
	manifestBytes, err := encodeExportManifest(manifest)
	if err != nil {
		return err
	}
	for _, item := range manifest.Items {
		if err := copyExportItem(ctx, root, stagingLibrary, item); err != nil {
			return err
		}
	}
	if err := stagingLibrary.WriteRelativeAtomic(ctx, library.ManifestName, manifestBytes); err != nil {
		return err
	}
	directoryBytes, err := encodeExportDirectory(manifestBytes, manifest.Items)
	if err != nil {
		return err
	}
	if err := stagingLibrary.WriteRelativeAtomic(ctx, "packs.json", directoryBytes); err != nil {
		return err
	}
	collectionsBytes, err := encodeExportCollections(collections)
	if err != nil {
		return err
	}
	if err := stagingLibrary.WriteRelativeAtomic(ctx, CollectionsExtensionName, collectionsBytes); err != nil {
		return err
	}
	if err := verifyExportOutput(ctx, stagingLibrary, manifest, collections); err != nil {
		return err
	}
	if err := publishDirectoryNoReplace(staging, destination); err != nil {
		if errors.Is(err, errExportDestinationExists) {
			return errorf("conflict", "destination_exists", "Choose a new export directory.", "export destination already exists")
		}
		return wrapExportIO("publish export directory", err)
	}
	published = true
	return nil
}

func absoluteExportPath(value string) (string, error) {
	if strings.IndexByte(value, 0) >= 0 {
		return "", errorf("validation", "unsafe_path", "Choose a local export directory.", "export destination contains an invalid character")
	}
	abs, err := filepath.Abs(value)
	if err != nil {
		return "", errorf("validation", "unsafe_path", "Choose a local export directory.", "cannot resolve export destination: %v", err)
	}
	clean := filepath.Clean(abs)
	if clean == string(filepath.Separator) || clean == filepath.VolumeName(clean)+string(filepath.Separator) {
		return "", errorf("validation", "unsafe_path", "Choose a new directory below a local parent.", "export destination cannot be a filesystem root")
	}
	return clean, nil
}

func validateExportDestination(destination string) error {
	parent := filepath.Dir(destination)
	info, err := os.Stat(parent)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errorf("io", "read_failed", "Create the destination parent directory and retry.", "export destination parent does not exist")
		}
		return wrapExportIO("inspect export destination parent", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errorf("validation", "unsafe_path", "Choose a real local directory as the destination parent.", "export destination parent is not a directory")
	}
	if _, err := os.Lstat(destination); err == nil {
		return errorf("conflict", "destination_exists", "Choose a new export directory.", "export destination already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return wrapExportIO("inspect export destination", err)
	}
	return nil
}

func verifyExportItems(ctx context.Context, root *library.Library, items []library.Item) error {
	for _, item := range items {
		if err := contextError(ctx); err != nil {
			return err
		}
		if err := root.VerifyItem(ctx, item); err != nil {
			return err
		}
	}
	return nil
}

func createExportStaging(destination string) (string, error) {
	parent, err := filepath.EvalSymlinks(filepath.Dir(destination))
	if err != nil {
		return "", wrapExportIO("resolve export destination parent", err)
	}
	staging, err := os.MkdirTemp(parent, ".sticker-export-*")
	if err != nil {
		return "", wrapExportIO("create export staging directory", err)
	}
	if err := os.Chmod(staging, 0o700); err != nil {
		_ = cleanupExportStaging(staging)
		return "", wrapExportIO("secure export staging directory", err)
	}
	return staging, nil
}

func cleanupExportStaging(staging string) error {
	parent, err := filepath.EvalSymlinks(filepath.Dir(staging))
	if err != nil {
		return err
	}
	root, err := library.New(parent)
	if err != nil {
		return err
	}
	return root.RemoveRelativeDirectory(context.Background(), filepath.Base(staging))
}

func copyExportItem(ctx context.Context, source, staging *library.Library, item library.Item) error {
	file, _, err := source.OpenVerified(ctx, item)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	reader := newImportDigestReader(&contextReader{ctx: ctx, reader: file})
	if err := staging.WriteRelativeAtomicFrom(ctx, item.Filename, reader, item.Size, func(count int64) error {
		return reader.validate(item, count)
	}); err != nil {
		return err
	}
	return staging.VerifyItem(ctx, item)
}

func verifyExportOutput(ctx context.Context, staging *library.Library, manifest library.Manifest, collections Collections) error {
	output, err := staging.ReadManifestRequired(ctx)
	if err != nil {
		return err
	}
	if len(output.Items) != len(manifest.Items) {
		return errorf("integrity", "invalid_manifest", "Retry the export from an unchanged personal library.", "export manifest item count changed during publication")
	}
	for index, item := range output.Items {
		if item != manifest.Items[index] {
			return errorf("integrity", "invalid_manifest", "Retry the export from an unchanged personal library.", "export manifest differs from the personal manifest")
		}
	}
	if err := staging.Validate(ctx, output); err != nil {
		return err
	}
	actualCollections, present, err := readOptionalExtension(ctx, staging, output)
	if err != nil {
		return err
	}
	if !present {
		return errorf("integrity", "invalid_collection", "Retry the export from an unchanged personal library.", "export collections extension is missing")
	}
	expectedBytes, err := encodeExportCollections(collections)
	if err != nil {
		return err
	}
	actualBytes, err := encodeExportCollections(actualCollections)
	if err != nil {
		return err
	}
	if !bytes.Equal(actualBytes, expectedBytes) {
		return errorf("integrity", "invalid_collection", "Retry the export from an unchanged personal library.", "export collections extension differs from the personal collections")
	}
	return nil
}

func encodeExportCollections(collections Collections) ([]byte, error) {
	collections.Collections = append([]Collection(nil), collections.Collections...)
	for index := range collections.Collections {
		collections.Collections[index].Items = append([]CollectionItem(nil), collections.Collections[index].Items...)
	}
	normalizeCollections(&collections)
	if err := validateExportCollections(collections); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(collections, "", "  ")
	if err != nil {
		return nil, errorf("internal", "unexpected", "Retry the export.", "encode export collections: %v", err)
	}
	data = append(data, '\n')
	if int64(len(data)) > library.MaxManifestBytes {
		return nil, errorf("validation", "output_limit", "Reduce the number of collections and retry.", "export collections exceeds the %d byte limit", library.MaxManifestBytes)
	}
	return data, nil
}

func validateExportCollections(collections Collections) error {
	if collections.SchemaVersion != 1 || len(collections.Collections) == 0 {
		return invalidCollections("export collections metadata is empty")
	}
	for _, collection := range collections.Collections {
		if err := validateCollectionID(collection.ID); err != nil {
			return invalidCollections("export collection ID is invalid")
		}
		if err := validateCollectionName(collection.Name); err != nil {
			return invalidCollections("export collection name is invalid")
		}
	}
	return nil
}

func encodeExportManifest(manifest library.Manifest) ([]byte, error) {
	manifest.Items = append([]library.Item(nil), manifest.Items...)
	sort.Slice(manifest.Items, func(i, j int) bool { return manifest.Items[i].MD5 < manifest.Items[j].MD5 })
	if err := library.ValidateManifest(manifest, library.DefaultLimits()); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, errorf("internal", "unexpected", "Retry the export.", "encode export manifest: %v", err)
	}
	data = append(data, '\n')
	if int64(len(data)) > library.MaxManifestBytes {
		return nil, errorf("validation", "output_limit", "Reduce the number of favorites and retry.", "export manifest exceeds the %d byte limit", library.MaxManifestBytes)
	}
	return data, nil
}

func encodeExportDirectory(manifestBytes []byte, items []library.Item) ([]byte, error) {
	sum := sha256.Sum256(manifestBytes)
	directory := exportPackDirectory{
		SchemaVersion: 1,
		Packs: []exportPackEntry{{
			ID:             exportPackID,
			Name:           "Personal favorites",
			Description:    "Exported personal favorites",
			Manifest:       library.ManifestName,
			ManifestSHA256: hex.EncodeToString(sum[:]),
			Count:          len(items),
			Size:           manifestSize(items),
		}},
	}
	data, err := json.MarshalIndent(directory, "", "  ")
	if err != nil {
		return nil, errorf("internal", "unexpected", "Retry the export.", "encode export pack directory: %v", err)
	}
	data = append(data, '\n')
	if int64(len(data)) > library.MaxManifestBytes {
		return nil, errorf("validation", "output_limit", "Reduce the number of favorites and retry.", "export pack directory exceeds the %d byte limit", library.MaxManifestBytes)
	}
	return data, nil
}

func manifestSize(items []library.Item) int64 {
	var total int64
	for _, item := range items {
		total += item.Size
	}
	return total
}

func wrapExportIO(operation string, err error) error {
	var committed *exportCommittedError
	if errors.As(err, &committed) {
		return &library.Error{
			Kind:      "io",
			Subtype:   "write_failed",
			Message:   fmt.Sprintf("%s: %v", operation, err),
			Hint:      "Read the exported directory before retrying.",
			Err:       err,
			Committed: true,
		}
	}
	if err == nil {
		return errorf("io", "write_failed", "Retry the export.", "%s", operation)
	}
	return &library.Error{
		Kind:    "io",
		Subtype: "write_failed",
		Message: fmt.Sprintf("%s: %v", operation, err),
		Hint:    "Retry the export.",
		Err:     err,
	}
}
