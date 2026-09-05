package favorites

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"os"
	"path/filepath"

	"github.com/9Ashwin/sticker-cli/internal/library"
)

const importStagingDirectory = ".sticker/staging"

// ImportOptions controls a standard v1 material directory import.
type ImportOptions struct {
	Home              string
	Source            string
	OverwriteCaptions bool
	DryRun            bool
}

// ImportResult reports the result or planned result of an import. Counts in
// a dry run or failed operation describe the attempted merge and Committed
// makes clear whether those counts are present in the personal manifest.
type ImportResult struct {
	Added     int  `json:"added"`
	Skipped   int  `json:"skipped"`
	Updated   int  `json:"updated"`
	Conflicts int  `json:"conflicts"`
	Failed    int  `json:"failed"`
	Committed bool `json:"committed"`
	DryRun    bool `json:"dry_run,omitempty"`
}

type importPlan struct {
	target            *library.Library
	source            *library.Library
	plannedItems      []plannedImportItem
	result            ImportResult
	needsPublish      bool
	needsCopy         bool
	sameRoot          bool
	overwriteCaptions bool
}

type plannedImportItem struct {
	source    library.Item
	needsCopy bool
}

// Import validates and merges a standard v1 manifest and its emoticons into
// the personal library. All source images are validated before the personal
// manifest is published, and the final merge is repeated under the library
// write lock so concurrent favorites are retained.
func Import(ctx context.Context, options ImportOptions) (ImportResult, error) {
	plan, err := prepareImport(ctx, options)
	if err != nil {
		return plan.result, err
	}
	result := plan.result
	if options.DryRun {
		return result, nil
	}

	var stagingRoot string
	var staging *library.Library
	if plan.needsCopy {
		stagingRoot, err = createImportStagingDirectory(plan.target)
		if err != nil {
			result.Failed++
			return result, err
		}
		staging, err = library.New(stagingRoot)
		if err != nil {
			result.Failed++
			return result, err
		}
		if err := staging.EnsureRelativeDirectory(library.EmoticonsDirectory); err != nil {
			result.Failed++
			return result, err
		}
		for _, item := range plan.plannedItems {
			if !item.needsCopy {
				continue
			}
			if err := stageImportItem(ctx, plan.source, staging, item.source); err != nil {
				result.Failed++
				return result, err
			}
		}
	}

	if !plan.needsPublish {
		result.Committed = true
		return result, nil
	}

	result, err = publishImport(ctx, plan, staging, result)
	if err != nil {
		return result, err
	}
	result.Committed = true
	return result, nil
}

func prepareImport(ctx context.Context, options ImportOptions) (importPlan, error) {
	if err := contextError(ctx); err != nil {
		return importPlan{}, err
	}
	if options.Home == "" {
		return importPlan{}, errorf("validation", "invalid_argument", "Choose a local data directory.", "favorites home is empty")
	}
	if options.Source == "" {
		return importPlan{}, errorf("validation", "invalid_argument", "Provide a source material directory.", "import source is empty")
	}
	target, err := library.New(options.Home)
	if err != nil {
		return importPlan{}, err
	}
	source, err := library.New(options.Source)
	if err != nil {
		return importPlan{}, err
	}
	sourceManifest, err := source.ReadManifestRequired(ctx)
	if err != nil {
		return importPlan{}, err
	}
	personal, err := target.ReadManifest(ctx)
	if err != nil {
		return importPlan{}, err
	}

	plan := importPlan{
		target:            target,
		source:            source,
		result:            ImportResult{DryRun: options.DryRun},
		sameRoot:          target.Root == source.Root,
		overwriteCaptions: options.OverwriteCaptions,
	}
	for _, sourceItem := range sourceManifest.Items {
		if err := contextError(ctx); err != nil {
			return plan, err
		}
		existing, found := findImportItem(personal.Items, sourceItem.MD5)
		if found && !sameContent(existing, sourceItem) {
			plan.result.Conflicts++
			plan.result.Failed++
			return plan, digestConflict(sourceItem.MD5)
		}

		if err := verifySourceItem(ctx, source, sourceItem); err != nil {
			plan.result.Failed++
			return plan, err
		}

		candidate := sourceItem
		needsCopy := !found
		if found {
			candidate = existing
			if options.OverwriteCaptions {
				candidate.Caption = sourceItem.Caption
			}
			if candidate.Caption != existing.Caption {
				plan.result.Updated++
			} else {
				plan.result.Skipped++
			}
			if err := target.VerifyItem(ctx, existing); err != nil {
				if !repairableTargetError(err) {
					return plan, err
				}
				needsCopy = true
				candidate = sourceItem
				candidate.Caption = existing.Caption
				if options.OverwriteCaptions {
					candidate.Caption = sourceItem.Caption
				}
			}
		} else {
			plan.result.Added++
		}

		if needsCopy {
			if plan.sameRoot {
				return plan, errorf("integrity", "invalid_image", "Restore the source image before importing it.", "source and target are the same library and image %s is not usable", sourceItem.MD5)
			}
			plan.needsCopy = true
		}
		plan.plannedItems = append(plan.plannedItems, plannedImportItem{source: sourceItem, needsCopy: needsCopy})
	}
	plan.needsPublish = plan.needsCopy || plan.result.Updated > 0 || plan.result.Added > 0
	if plan.sameRoot && plan.needsCopy {
		return plan, errorf("integrity", "invalid_image", "Restore the source image before importing it.", "source and target are the same library and image data is not usable")
	}
	return plan, nil
}

func createImportStagingDirectory(root *library.Library) (string, error) {
	path, err := root.CreateRelativeTempDirectory(importStagingDirectory, "import-*")
	if err != nil {
		return "", err
	}
	return path, nil
}

func verifySourceItem(ctx context.Context, source *library.Library, item library.Item) error {
	file, _, err := source.OpenVerified(ctx, item)
	if err != nil {
		return err
	}
	return file.Close()
}

func stageImportItem(ctx context.Context, source, staging *library.Library, item library.Item) error {
	file, _, err := source.OpenVerified(ctx, item)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	reader := newImportDigestReader(&contextReader{ctx: ctx, reader: file})
	err = staging.WriteRelativeAtomicFrom(ctx, item.Filename, reader, item.Size, func(count int64) error {
		return reader.validate(item, count)
	})
	if err != nil {
		return err
	}
	return nil
}

func publishImport(ctx context.Context, plan importPlan, staging *library.Library, result ImportResult) (ImportResult, error) {
	committedResult := result
	err := plan.target.UpdateManifest(ctx, func(current library.Manifest) (library.Manifest, error) {
		actual := ImportResult{DryRun: false}
		defer func() { committedResult = actual }()
		items := append([]library.Item(nil), current.Items...)
		for _, planned := range plan.plannedItems {
			if err := contextError(ctx); err != nil {
				actual.Failed++
				return library.Manifest{}, err
			}
			existing, found := findImportItem(items, planned.source.MD5)
			if found && !sameContent(existing, planned.source) {
				actual.Conflicts++
				actual.Failed++
				return library.Manifest{}, digestConflict(planned.source.MD5)
			}

			candidate := planned.source
			needsCopy := !found
			if found {
				candidate = existing
				if plan.overwriteCaptions {
					candidate.Caption = planned.source.Caption
				}
				if candidate.Caption != existing.Caption {
					actual.Updated++
				} else {
					actual.Skipped++
				}
				if err := plan.target.VerifyItem(ctx, existing); err != nil {
					if !repairableTargetError(err) {
						actual.Failed++
						return library.Manifest{}, err
					}
					needsCopy = true
					candidate = planned.source
					candidate.Caption = existing.Caption
					if plan.overwriteCaptions {
						candidate.Caption = planned.source.Caption
					}
				}
			} else {
				actual.Added++
			}

			if needsCopy {
				if staging == nil {
					actual.Failed++
					return library.Manifest{}, errorf("integrity", "invalid_image", "Retry the import with an available staged source image.", "staged image %s is missing", planned.source.MD5)
				}
				if err := publishStagedItem(ctx, plan.target, staging, candidate); err != nil {
					actual.Failed++
					return library.Manifest{}, err
				}
			}
			items = upsert(items, candidate)
		}
		committedResult = actual
		current.Items = items
		return current, nil
	})
	if err != nil {
		result = committedResult
		if result.DryRun {
			result.DryRun = false
		}
		var coded *library.Error
		if errors.As(err, &coded) && coded.Committed {
			result.Committed = true
		}
		return result, err
	}
	return committedResult, nil
}

func publishStagedItem(ctx context.Context, target, staging *library.Library, item library.Item) error {
	file, err := library.OpenRelative(ctx, staging.Root, filepath.FromSlash(item.Filename))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errorf("integrity", "invalid_image", "Retry the import with an available staged source image.", "staged image %s is missing", item.MD5)
		}
		return err
	}
	defer func() { _ = file.Close() }()
	reader := newImportDigestReader(&contextReader{ctx: ctx, reader: file})
	if err := target.WriteRelativeAtomicFrom(ctx, item.Filename, reader, item.Size, func(count int64) error {
		return reader.validate(item, count)
	}); err != nil {
		return err
	}
	return target.VerifyItem(ctx, item)
}

func findImportItem(items []library.Item, md5 string) (library.Item, bool) {
	for _, item := range items {
		if item.MD5 == md5 {
			return item, true
		}
	}
	return library.Item{}, false
}

func sameContent(a, b library.Item) bool {
	return a.MD5 == b.MD5 && a.SHA256 == b.SHA256 && a.Size == b.Size
}

func repairableTargetError(err error) bool {
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	var coded *library.Error
	if !errors.As(err, &coded) {
		return false
	}
	return coded.Kind == "integrity" && (coded.Subtype == "invalid_image" || coded.Subtype == "hash_mismatch")
}

func digestConflict(id string) *library.Error {
	return errorf("conflict", "digest_conflict", "Resolve the existing personal entry before importing this image.", "item %s has different content", id)
}

type importDigestReader struct {
	source  io.Reader
	md5Hash hash.Hash
	shaHash hash.Hash
}

func newImportDigestReader(source io.Reader) *importDigestReader {
	return &importDigestReader{source: source, md5Hash: md5.New(), shaHash: sha256.New()}
}

func (r *importDigestReader) Read(p []byte) (int, error) {
	n, err := r.source.Read(p)
	if n > 0 {
		_, _ = r.md5Hash.Write(p[:n])
		_, _ = r.shaHash.Write(p[:n])
	}
	return n, err
}

func (r *importDigestReader) validate(item library.Item, count int64) error {
	if count != item.Size || hex.EncodeToString(r.md5Hash.Sum(nil)) != item.MD5 || hex.EncodeToString(r.shaHash.Sum(nil)) != item.SHA256 {
		return errorf("integrity", "hash_mismatch", "Restore the original image or retry the import.", "image %s does not match its manifest hashes", item.MD5)
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
