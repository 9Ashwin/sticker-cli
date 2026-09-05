// Package favorites manages the personal standard image library.
package favorites

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"unicode/utf8"

	"github.com/9Ashwin/sticker-cli/internal/library"
	stickersearch "github.com/9Ashwin/sticker-cli/internal/search"
)

// Options describes one favorites add operation. Caption is nil when the
// caller did not pass --caption; a non-nil empty string is an explicit clear.
type Options struct {
	Home    string
	Path    string
	ID      string
	Caption *string
	DryRun  bool
}

// Result is the machine-readable outcome of adding one favorite.
type Result struct {
	Item    stickersearch.Item `json:"item"`
	Added   bool               `json:"added"`
	Updated bool               `json:"updated"`
	DryRun  bool               `json:"dry_run,omitempty"`
}

// Execute validates, stages, and publishes one personal favorite. Image bytes
// are read before acquiring the manifest lock; the lock is held while the
// current manifest is re-read, the target image is committed, and the new
// manifest is atomically published.
func Execute(ctx context.Context, options Options) (Result, error) {
	if err := contextError(ctx); err != nil {
		return Result{}, err
	}
	if options.Home == "" {
		return Result{}, errorf("validation", "invalid_argument", "Choose a local data directory.", "favorites home is empty")
	}
	if options.Path == "" && options.ID == "" {
		return Result{}, errorf("validation", "invalid_argument", "Provide exactly one source.", "one of PATH or --id is required")
	}
	if options.Path != "" && options.ID != "" {
		return Result{}, errorf("validation", "invalid_argument", "Choose exactly one source.", "PATH and --id cannot be used together")
	}
	if options.Caption != nil && (len([]byte(*options.Caption)) > library.MaxCaptionBytes || !utf8.ValidString(*options.Caption)) {
		return Result{}, errorf("validation", "invalid_argument", "Use a valid UTF-8 caption within the supported size limit.", "caption exceeds the %d byte limit or is not valid UTF-8", library.MaxCaptionBytes)
	}

	root, err := library.New(options.Home)
	if err != nil {
		return Result{}, err
	}
	source, sourceResult, data, err := readSource(ctx, root, options)
	if err != nil {
		return Result{}, err
	}
	personal, err := root.ReadManifest(ctx)
	if err != nil {
		return Result{}, err
	}
	planned, added, updated, err := plan(personal, source, options.Caption)
	if err != nil {
		return Result{}, err
	}
	if err := checkTarget(ctx, root, planned); err != nil {
		return Result{}, err
	}
	result := Result{Item: resultItem(root, planned, sourceResult), Added: added, Updated: updated, DryRun: options.DryRun}
	if options.DryRun {
		return result, nil
	}

	var committed library.Item
	err = root.UpdateManifest(ctx, func(current library.Manifest) (library.Manifest, error) {
		currentItem, currentAdded, currentUpdated, err := plan(current, source, options.Caption)
		if err != nil {
			return library.Manifest{}, err
		}
		if err := checkTarget(ctx, root, currentItem); err != nil {
			return library.Manifest{}, err
		}
		if err := ensureTarget(ctx, root, currentItem, data); err != nil {
			return library.Manifest{}, err
		}
		committed = currentItem
		result.Added = currentAdded
		result.Updated = currentUpdated
		current.Items = upsert(current.Items, currentItem)
		return current, nil
	})
	if err != nil {
		return Result{}, err
	}
	result.Item = resultItem(root, committed, sourceResult)
	return result, nil
}

func readSource(ctx context.Context, root *library.Library, options Options) (library.Item, stickersearch.Item, []byte, error) {
	if options.Path != "" {
		item, data, err := library.ReadImage(ctx, options.Path)
		return item, stickersearch.Item{}, data, err
	}
	found, err := stickersearch.Find(ctx, root.Root, options.ID)
	if err != nil {
		return library.Item{}, stickersearch.Item{}, nil, err
	}
	item := library.Item{
		MD5:      found.MD5,
		SHA256:   found.SHA256,
		Filename: found.Filename,
		Format:   found.Format,
		Size:     found.Size,
		Caption:  found.Caption,
	}
	file, _, err := root.OpenVerified(ctx, item)
	if err != nil {
		return library.Item{}, stickersearch.Item{}, nil, err
	}
	defer func() { _ = file.Close() }()
	data, err := readVerifiedBytes(ctx, file)
	if err != nil {
		return library.Item{}, stickersearch.Item{}, nil, err
	}
	return item, found, data, nil
}

func readVerifiedBytes(ctx context.Context, file *os.File) ([]byte, error) {
	var buffer bytes.Buffer
	if _, err := copyContext(ctx, &buffer, io.LimitReader(file, library.MaxImageBytes+1)); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, errorf("cancelled", "interrupted", "Retry the operation when ready.", "source image read cancelled")
		}
		return nil, errorf("io", "read_failed", "Check the source image.", "could not read verified source image")
	}
	if int64(buffer.Len()) > library.MaxImageBytes {
		return nil, errorf("integrity", "invalid_image", "Use an image within the supported size limit.", "source image exceeds the supported size limit")
	}
	return buffer.Bytes(), nil
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

func plan(manifest library.Manifest, source library.Item, caption *string) (library.Item, bool, bool, error) {
	for _, existing := range manifest.Items {
		if existing.MD5 != source.MD5 {
			continue
		}
		if existing.SHA256 != source.SHA256 || existing.Size != source.Size {
			return library.Item{}, false, false, errorf("conflict", "digest_conflict", "Remove the conflicting personal entry before adding this image.", "item %s has different content", source.MD5)
		}
		candidate := existing
		updated := false
		if caption != nil && candidate.Caption != *caption {
			candidate.Caption = *caption
			updated = true
		}
		return candidate, false, updated, nil
	}
	candidate := source
	if caption != nil {
		candidate.Caption = *caption
	}
	return candidate, true, false, nil
}

func checkTarget(ctx context.Context, root *library.Library, item library.Item) error {
	if err := root.VerifyItem(ctx, item); err == nil {
		return nil
	} else if errors.Is(err, os.ErrNotExist) {
		return nil
	} else {
		return err
	}
}

func ensureTarget(ctx context.Context, root *library.Library, item library.Item, data []byte) error {
	if err := root.VerifyItem(ctx, item); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return root.WriteRelativeAtomic(ctx, item.Filename, data)
}

func upsert(items []library.Item, item library.Item) []library.Item {
	result := make([]library.Item, 0, len(items)+1)
	replaced := false
	for _, existing := range items {
		if existing.MD5 == item.MD5 {
			result = append(result, item)
			replaced = true
			continue
		}
		result = append(result, existing)
	}
	if !replaced {
		result = append(result, item)
	}
	return result
}

func resultItem(root *library.Library, item library.Item, source stickersearch.Item) stickersearch.Item {
	path, err := root.ItemPath(item)
	if err != nil {
		path = filepath.Join(root.Root, filepath.FromSlash(item.Filename))
	}
	packs := append([]string{}, source.Packs...)
	return stickersearch.Item{
		ID:       item.MD5,
		MD5:      item.MD5,
		SHA256:   item.SHA256,
		Filename: item.Filename,
		Format:   item.Format,
		Size:     item.Size,
		Caption:  item.Caption,
		Path:     path,
		Favorite: true,
		Packs:    packs,
	}
}

func errorf(kind, subtype, hint, format string, args ...any) *library.Error {
	return &library.Error{Kind: kind, Subtype: subtype, Hint: hint, Message: fmt.Sprintf(format, args...)}
}

func contextError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return errorf("cancelled", "interrupted", "Retry the operation when ready.", "operation cancelled: %v", err)
	}
	return nil
}
