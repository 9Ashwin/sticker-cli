package favorites

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/9Ashwin/sticker-cli/internal/library"
	stickersearch "github.com/9Ashwin/sticker-cli/internal/search"
)

// ListOptions controls one personal favorites listing.
type ListOptions struct {
	Home       string
	Collection string
	Sort       string
	Limit      int
	Offset     int
}

// List reads the personal manifest and returns one bounded page of favorites.
// It does not read image bytes, so callers can list metadata even when an
// original needs to be restored separately.
func List(ctx context.Context, options ListOptions) (stickersearch.Result, error) {
	if err := contextError(ctx); err != nil {
		return stickersearch.Result{}, err
	}
	if options.Home == "" {
		return stickersearch.Result{}, errorf("validation", "invalid_argument", "Choose a local data directory.", "favorites home is empty")
	}
	if options.Limit < 1 || options.Limit > 100 {
		return stickersearch.Result{}, errorf("validation", "invalid_argument", "Choose a page size from 1 through 100.", "limit must be between 1 and 100")
	}
	if options.Offset < 0 {
		return stickersearch.Result{}, errorf("validation", "invalid_argument", "Choose an offset of 0 or greater.", "offset cannot be negative")
	}
	if options.Sort == "" {
		options.Sort = "manual"
	}
	switch options.Sort {
	case "manual", "added", "caption", "md5":
	default:
		return stickersearch.Result{}, errorf("validation", "invalid_argument", "Choose manual, added, caption, or md5 ordering.", "unsupported favorite sort %q", options.Sort)
	}
	if options.Collection != "" && options.Collection != "favorites" {
		return stickersearch.Result{}, errorf("not_found", "collection_not_found", "Use the default favorites collection until custom collections are configured.", "favorite collection %q was not found", options.Collection)
	}

	root, err := library.New(options.Home)
	if err != nil {
		return stickersearch.Result{}, err
	}
	manifest, err := root.ReadManifest(ctx)
	if err != nil {
		return stickersearch.Result{}, err
	}
	items := make([]stickersearch.Item, 0, len(manifest.Items))
	for _, item := range manifest.Items {
		favorite, err := favoriteItem(root, item)
		if err != nil {
			return stickersearch.Result{}, err
		}
		items = append(items, favorite)
	}
	sortFavoriteItems(items, options.Sort)

	result := stickersearch.Result{
		Items:      []stickersearch.Item{},
		Total:      len(items),
		NextOffset: options.Offset,
		HasMore:    false,
	}
	if options.Offset < len(items) {
		end := min(options.Offset+options.Limit, len(items))
		result.Items = append(result.Items, items[options.Offset:end]...)
		result.NextOffset = end
		result.HasMore = end < len(items)
	}
	if err := contextError(ctx); err != nil {
		return stickersearch.Result{}, err
	}
	return result, nil
}

func sortFavoriteItems(items []stickersearch.Item, order string) {
	if order == "manual" || order == "added" {
		return
	}
	sort.SliceStable(items, func(i, j int) bool {
		if order == "md5" {
			return items[i].MD5 < items[j].MD5
		}
		left := strings.ToLower(items[i].Caption)
		right := strings.ToLower(items[j].Caption)
		if left != right {
			return left < right
		}
		return items[i].MD5 < items[j].MD5
	})
}

// DescribeOptions controls one personal caption update.
type DescribeOptions struct {
	Home    string
	ID      string
	Caption string
	DryRun  bool
}

// DescribeResult is the machine-readable outcome of a caption update.
type DescribeResult struct {
	Item    stickersearch.Item `json:"item"`
	Updated bool               `json:"updated"`
	DryRun  bool               `json:"dry_run,omitempty"`
}

// Describe replaces the personal caption for one favorite. The update is
// performed against the manifest snapshot acquired under the write lock.
func Describe(ctx context.Context, options DescribeOptions) (DescribeResult, error) {
	if err := contextError(ctx); err != nil {
		return DescribeResult{}, err
	}
	if options.Home == "" {
		return DescribeResult{}, errorf("validation", "invalid_argument", "Choose a local data directory.", "favorites home is empty")
	}
	if err := validateID(options.ID); err != nil {
		return DescribeResult{}, err
	}
	if !utf8.ValidString(options.Caption) || len([]byte(options.Caption)) > library.MaxCaptionBytes {
		return DescribeResult{}, errorf("validation", "invalid_argument", "Use a valid UTF-8 caption within the supported size limit.", "caption exceeds the %d byte limit or is not valid UTF-8", library.MaxCaptionBytes)
	}
	root, err := library.New(options.Home)
	if err != nil {
		return DescribeResult{}, err
	}
	if options.DryRun {
		manifest, err := root.ReadManifest(ctx)
		if err != nil {
			return DescribeResult{}, err
		}
		item, ok := findItem(manifest, options.ID)
		if !ok {
			return DescribeResult{}, itemNotFound(options.ID)
		}
		updated := item.Caption != options.Caption
		item.Caption = options.Caption
		favorite, err := favoriteItem(root, item)
		if err != nil {
			return DescribeResult{}, err
		}
		return DescribeResult{Item: favorite, Updated: updated, DryRun: true}, nil
	}

	var committed library.Item
	var updated bool
	if err := root.UpdateManifest(ctx, func(current library.Manifest) (library.Manifest, error) {
		currentItem, ok := findItem(current, options.ID)
		if !ok {
			return library.Manifest{}, itemNotFound(options.ID)
		}
		updated = currentItem.Caption != options.Caption
		currentItem.Caption = options.Caption
		committed = currentItem
		current.Items = replaceItem(current.Items, currentItem)
		return current, nil
	}); err != nil {
		return DescribeResult{}, err
	}
	favorite, err := favoriteItem(root, committed)
	if err != nil {
		return DescribeResult{}, err
	}
	return DescribeResult{Item: favorite, Updated: updated}, nil
}

// RemoveOptions controls removal of personal favorite relationships.
type RemoveOptions struct {
	Home   string
	IDs    []string
	DryRun bool
}

// RemoveResult is the machine-readable outcome of removing favorite
// relationships. Original files are deliberately retained.
type RemoveResult struct {
	Removed          int  `json:"removed"`
	RetainedOriginal int  `json:"retained_original"`
	Committed        bool `json:"committed"`
	DryRun           bool `json:"dry_run,omitempty"`
}

// Remove deletes only personal manifest entries. It never deletes image
// files, allowing installed packs and other references to continue using the
// same original bytes.
func Remove(ctx context.Context, options RemoveOptions) (RemoveResult, error) {
	if err := contextError(ctx); err != nil {
		return RemoveResult{}, err
	}
	if options.Home == "" {
		return RemoveResult{}, errorf("validation", "invalid_argument", "Choose a local data directory.", "favorites home is empty")
	}
	if len(options.IDs) == 0 {
		return RemoveResult{}, errorf("validation", "invalid_argument", "Provide at least one favorite ID.", "favorite IDs are empty")
	}
	ids := make(map[string]struct{}, len(options.IDs))
	for _, id := range options.IDs {
		if err := validateID(id); err != nil {
			return RemoveResult{}, err
		}
		ids[id] = struct{}{}
	}
	root, err := library.New(options.Home)
	if err != nil {
		return RemoveResult{}, err
	}
	manifest, err := root.ReadManifest(ctx)
	if err != nil {
		return RemoveResult{}, err
	}
	planned := countItems(manifest, ids)
	result := RemoveResult{Removed: planned, RetainedOriginal: planned, DryRun: options.DryRun}
	if options.DryRun {
		return result, nil
	}

	var committed int
	if err := root.UpdateManifest(ctx, func(current library.Manifest) (library.Manifest, error) {
		kept := make([]library.Item, 0, len(current.Items))
		for _, item := range current.Items {
			if _, remove := ids[item.MD5]; remove {
				committed++
				continue
			}
			kept = append(kept, item)
		}
		current.Items = kept
		return current, nil
	}); err != nil {
		return RemoveResult{}, err
	}
	result.Removed = committed
	result.RetainedOriginal = committed
	result.Committed = committed > 0
	return result, nil
}

func findItem(manifest library.Manifest, id string) (library.Item, bool) {
	for _, item := range manifest.Items {
		if item.MD5 == id {
			return item, true
		}
	}
	return library.Item{}, false
}

func countItems(manifest library.Manifest, ids map[string]struct{}) int {
	count := 0
	for _, item := range manifest.Items {
		if _, ok := ids[item.MD5]; ok {
			count++
		}
	}
	return count
}

func replaceItem(items []library.Item, replacement library.Item) []library.Item {
	result := make([]library.Item, len(items))
	copy(result, items)
	for index := range result {
		if result[index].MD5 == replacement.MD5 {
			result[index] = replacement
			break
		}
	}
	return result
}

func favoriteItem(root *library.Library, item library.Item) (stickersearch.Item, error) {
	path, err := root.ItemPath(item)
	if err != nil {
		return stickersearch.Item{}, err
	}
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
		Packs:    []string{},
	}, nil
}

func validateID(id string) error {
	if len(id) != md5.Size*2 {
		return errorf("validation", "invalid_argument", "Use a lowercase 32-character MD5 ID.", "invalid item ID")
	}
	decoded, err := hex.DecodeString(id)
	if err != nil || len(decoded) != md5.Size || strings.ToLower(id) != id {
		return errorf("validation", "invalid_argument", "Use a lowercase 32-character MD5 ID.", "invalid item ID")
	}
	return nil
}

func itemNotFound(id string) error {
	return errorf("not_found", "item_not_found", "Choose an ID listed by personal favorites.", "favorite item %s was not found", id)
}
