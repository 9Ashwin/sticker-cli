package favorites

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/9Ashwin/sticker-cli/internal/library"
)

const (
	// CollectionsRelativePath is private CLI metadata kept beside the library
	// lock. It is deliberately separate from the standard v1 manifest.
	CollectionsRelativePath = ".sticker/collections.json"
	// CollectionsExtensionName is the optional root-level export/import file.
	CollectionsExtensionName = "collections.json"
	DefaultCollectionID      = "favorites"
	DefaultCollectionName    = "我的收藏"
)

// CollectionItem records one personal item relationship and its manual order.
// AddedAt is optional for metadata written by older versions.
type CollectionItem struct {
	ID       string `json:"id"`
	Position int    `json:"position"`
	AddedAt  string `json:"added_at,omitempty"`
}

// Collection is one default or user-created favorite group.
type Collection struct {
	ID       string           `json:"id"`
	Name     string           `json:"name"`
	Position int              `json:"position"`
	Items    []CollectionItem `json:"items"`
}

// Collections is the versioned private metadata document.
type Collections struct {
	SchemaVersion int          `json:"schema_version"`
	Collections   []Collection `json:"collections"`
}

// CollectionsListResult is the result of listing groups.
type CollectionsListResult struct {
	Collections []Collection `json:"collections"`
}

// CollectionResult is returned by create and rename.
type CollectionResult struct {
	Collection Collection `json:"collection"`
	Changed    bool       `json:"changed"`
	DryRun     bool       `json:"dry_run,omitempty"`
}

// CollectionRemoveResult is returned by removing a custom group. Members are
// moved to the default group, so removing a group cannot silently drop a
// favorite relationship.
type CollectionRemoveResult struct {
	Removed bool `json:"removed"`
	Moved   int  `json:"moved"`
	DryRun  bool `json:"dry_run,omitempty"`
}

// CollectionListOptions controls listing groups.
type CollectionListOptions struct {
	Home string
}

// CollectionCreateOptions controls creation of a custom group. The command
// uses the name as its stable ID, so names must also satisfy the ID contract.
type CollectionCreateOptions struct {
	Home   string
	Name   string
	DryRun bool
}

// CollectionRenameOptions controls changing a custom group name.
type CollectionRenameOptions struct {
	Home   string
	ID     string
	Name   string
	DryRun bool
}

// CollectionRemoveOptions controls removing a custom group.
type CollectionRemoveOptions struct {
	Home   string
	ID     string
	DryRun bool
}

// OrganizeOptions controls adding or removing relationships. When MoveTo is
// set, IDs are removed from Collection and added to the destination group.
// Without MoveTo, IDs are removed from Collection. Order is accepted for the
// same atomic metadata update and must contain every member exactly once.
type OrganizeOptions struct {
	Home       string
	Collection string
	IDs        []string
	MoveTo     string
	Order      []string
	DryRun     bool
}

// OrganizeResult describes a relationship update or dry-run plan.
type OrganizeResult struct {
	Moved     int  `json:"moved"`
	Reordered bool `json:"reordered"`
	Removed   int  `json:"removed"`
	Committed bool `json:"committed"`
	DryRun    bool `json:"dry_run,omitempty"`
}

// ListCollections returns the default group and any custom groups. Missing
// metadata is treated as the legacy state in which every personal item is in
// the default group; the file is not created by this read operation.
func ListCollections(ctx context.Context, options CollectionListOptions) (CollectionsListResult, error) {
	if err := contextError(ctx); err != nil {
		return CollectionsListResult{}, err
	}
	root, err := collectionRoot(options.Home)
	if err != nil {
		return CollectionsListResult{}, err
	}
	var result CollectionsListResult
	if err := root.WithReadLock(ctx, func(manifest library.Manifest) error {
		state, err := readCollections(ctx, root, manifest)
		if err != nil {
			return err
		}
		result.Collections = state.Collections
		return nil
	}); err != nil {
		return CollectionsListResult{}, err
	}
	return result, nil
}

// CreateCollection creates a custom group whose ID is the supplied stable
// name. It does not copy or modify any image or standard manifest entry.
func CreateCollection(ctx context.Context, options CollectionCreateOptions) (CollectionResult, error) {
	if err := validateCollectionHomeAndName(options.Home, options.Name); err != nil {
		return CollectionResult{}, err
	}
	root, err := collectionRoot(options.Home)
	if err != nil {
		return CollectionResult{}, err
	}
	if options.DryRun {
		var result CollectionResult
		err := root.WithReadLock(ctx, func(manifest library.Manifest) error {
			state, err := readCollections(ctx, root, manifest)
			if err != nil {
				return err
			}
			if _, ok := findCollection(state, options.Name); ok {
				return collectionExists(options.Name)
			}
			state.Collections = append(state.Collections, Collection{ID: options.Name, Name: options.Name, Position: nextCollectionPosition(state), Items: []CollectionItem{}})
			normalizeCollections(&state)
			result = CollectionResult{Collection: *collectionByID(state, options.Name), Changed: true, DryRun: true}
			return nil
		})
		if err != nil {
			return CollectionResult{}, err
		}
		return result, nil
	}

	var result CollectionResult
	if err := root.WithWriteLock(ctx, func(manifest library.Manifest) error {
		state, err := readCollections(ctx, root, manifest)
		if err != nil {
			return err
		}
		if _, ok := findCollection(state, options.Name); ok {
			return collectionExists(options.Name)
		}
		created := Collection{ID: options.Name, Name: options.Name, Position: nextCollectionPosition(state), Items: []CollectionItem{}}
		state.Collections = append(state.Collections, created)
		normalizeCollections(&state)
		if err := writeCollections(ctx, root, state, manifest); err != nil {
			return err
		}
		result = CollectionResult{Collection: *collectionByID(state, options.Name), Changed: true}
		return nil
	}); err != nil {
		return CollectionResult{}, err
	}
	return result, nil
}

// RenameCollection changes only a custom group's display name.
func RenameCollection(ctx context.Context, options CollectionRenameOptions) (CollectionResult, error) {
	if err := validateCollectionHome(options.Home); err != nil {
		return CollectionResult{}, err
	}
	if err := validateCollectionID(options.ID); err != nil {
		return CollectionResult{}, err
	}
	if err := validateCollectionName(options.Name); err != nil {
		return CollectionResult{}, err
	}
	if options.ID == DefaultCollectionID {
		return CollectionResult{}, errorf("validation", "invalid_argument", "Choose a custom collection to rename.", "the default favorites collection cannot be renamed")
	}
	root, err := collectionRoot(options.Home)
	if err != nil {
		return CollectionResult{}, err
	}
	mutate := func(manifest library.Manifest, commit bool) (CollectionResult, error) {
		state, err := readCollections(ctx, root, manifest)
		if err != nil {
			return CollectionResult{}, err
		}
		collection, ok := findCollection(state, options.ID)
		if !ok {
			return CollectionResult{}, collectionNotFound(options.ID)
		}
		changed := collection.Name != options.Name
		collection.Name = options.Name
		state.Collections = replaceCollection(state.Collections, *collection)
		normalizeCollections(&state)
		if commit && changed {
			if err := writeCollections(ctx, root, state, manifest); err != nil {
				return CollectionResult{}, err
			}
		}
		return CollectionResult{Collection: *collectionByID(state, options.ID), Changed: changed, DryRun: options.DryRun}, nil
	}
	if options.DryRun {
		var result CollectionResult
		if err := root.WithReadLock(ctx, func(manifest library.Manifest) error {
			var err error
			result, err = mutate(manifest, false)
			return err
		}); err != nil {
			return CollectionResult{}, err
		}
		return result, nil
	}
	var result CollectionResult
	if err := root.WithWriteLock(ctx, func(manifest library.Manifest) error {
		var err error
		result, err = mutate(manifest, true)
		return err
	}); err != nil {
		return CollectionResult{}, err
	}
	return result, nil
}

// RemoveCollection deletes a custom group and moves its relationships to the
// default group. The default group itself is permanent.
func RemoveCollection(ctx context.Context, options CollectionRemoveOptions) (CollectionRemoveResult, error) {
	if err := validateCollectionHome(options.Home); err != nil {
		return CollectionRemoveResult{}, err
	}
	if err := validateCollectionID(options.ID); err != nil {
		return CollectionRemoveResult{}, err
	}
	if options.ID == DefaultCollectionID {
		return CollectionRemoveResult{}, errorf("validation", "invalid_argument", "Keep the default favorites collection.", "the default favorites collection cannot be removed")
	}
	root, err := collectionRoot(options.Home)
	if err != nil {
		return CollectionRemoveResult{}, err
	}
	mutate := func(manifest library.Manifest, commit bool) (CollectionRemoveResult, error) {
		state, err := readCollections(ctx, root, manifest)
		if err != nil {
			return CollectionRemoveResult{}, err
		}
		index := collectionIndex(state, options.ID)
		if index < 0 {
			return CollectionRemoveResult{}, collectionNotFound(options.ID)
		}
		defaultIndex := collectionIndex(state, DefaultCollectionID)
		if defaultIndex < 0 {
			return CollectionRemoveResult{}, invalidCollections("default favorites collection is missing")
		}
		moved := appendCollectionItems(&state.Collections[defaultIndex], state.Collections[index].Items)
		state.Collections = append(state.Collections[:index], state.Collections[index+1:]...)
		normalizeCollections(&state)
		if commit {
			if err := writeCollections(ctx, root, state, manifest); err != nil {
				return CollectionRemoveResult{}, err
			}
		}
		return CollectionRemoveResult{Removed: true, Moved: moved, DryRun: options.DryRun}, nil
	}
	if options.DryRun {
		var result CollectionRemoveResult
		if err := root.WithReadLock(ctx, func(manifest library.Manifest) error {
			var err error
			result, err = mutate(manifest, false)
			return err
		}); err != nil {
			return CollectionRemoveResult{}, err
		}
		return result, nil
	}
	var result CollectionRemoveResult
	if err := root.WithWriteLock(ctx, func(manifest library.Manifest) error {
		var err error
		result, err = mutate(manifest, true)
		return err
	}); err != nil {
		return CollectionRemoveResult{}, err
	}
	return result, nil
}

// Organize updates relationships between groups. It validates every ID and
// the complete requested order before writing one metadata document.
func Organize(ctx context.Context, options OrganizeOptions) (OrganizeResult, error) {
	if err := validateCollectionHome(options.Home); err != nil {
		return OrganizeResult{}, err
	}
	if err := validateCollectionID(options.Collection); err != nil {
		return OrganizeResult{}, err
	}
	if options.MoveTo != "" {
		if err := validateCollectionID(options.MoveTo); err != nil {
			return OrganizeResult{}, err
		}
		if options.MoveTo == options.Collection {
			return OrganizeResult{}, errorf("validation", "invalid_argument", "Choose a different destination collection.", "source and destination collections are the same")
		}
	}
	for _, id := range append(append([]string{}, options.IDs...), options.Order...) {
		if err := validateItemID(id); err != nil {
			return OrganizeResult{}, err
		}
	}
	root, err := collectionRoot(options.Home)
	if err != nil {
		return OrganizeResult{}, err
	}
	mutate := func(manifest library.Manifest, commit bool) (OrganizeResult, error) {
		state, err := readCollections(ctx, root, manifest)
		if err != nil {
			return OrganizeResult{}, err
		}
		result, changed, err := applyOrganize(&state, manifest, options)
		if err != nil {
			return OrganizeResult{}, err
		}
		if commit && changed {
			if err := writeCollections(ctx, root, state, manifest); err != nil {
				return OrganizeResult{}, err
			}
			result.Committed = true
		}
		result.DryRun = options.DryRun
		return result, nil
	}
	if options.DryRun {
		var result OrganizeResult
		if err := root.WithReadLock(ctx, func(manifest library.Manifest) error {
			var err error
			result, err = mutate(manifest, false)
			return err
		}); err != nil {
			return OrganizeResult{}, err
		}
		return result, nil
	}
	var result OrganizeResult
	if err := root.WithWriteLock(ctx, func(manifest library.Manifest) error {
		var err error
		result, err = mutate(manifest, true)
		return err
	}); err != nil {
		return OrganizeResult{}, err
	}
	return result, nil
}

func mergeImportedCollections(ctx context.Context, plan importPlan) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	return plan.target.WithWriteLock(ctx, func(manifest library.Manifest) error {
		state, err := readCollections(ctx, plan.target, manifest)
		if err != nil {
			return err
		}
		metadataPresent, err := collectionsMetadataPresent(plan.target)
		if err != nil {
			return err
		}
		for _, sourceCollection := range plan.sourceCollections.Collections {
			destination, ok := findCollection(state, sourceCollection.ID)
			if !ok {
				destination = &Collection{ID: sourceCollection.ID, Name: sourceCollection.Name, Position: nextCollectionPosition(state), Items: []CollectionItem{}}
				state.Collections = append(state.Collections, *destination)
				destination = collectionByID(state, sourceCollection.ID)
			}
			if !metadataPresent && sourceCollection.ID == DefaultCollectionID {
				mergeCollectionItemsInSourceOrder(destination, sourceCollection.Items)
				continue
			}
			appendCollectionItems(destination, sourceCollection.Items)
		}
		ensureUnassignedItems(&state, manifest)
		normalizeCollections(&state)
		return writeCollections(ctx, plan.target, state, manifest)
	})
}

func collectionsMetadataPresent(root *library.Library) (bool, error) {
	_, err := os.Lstat(filepath.Join(root.Root, filepath.FromSlash(CollectionsRelativePath)))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, errorf("io", "read_failed", "Check the collections metadata permissions.", "cannot inspect collections metadata")
	}
	return true, nil
}

func applyOrganize(state *Collections, manifest library.Manifest, options OrganizeOptions) (OrganizeResult, bool, error) {
	sourceIndex := collectionIndex(*state, options.Collection)
	if sourceIndex < 0 {
		return OrganizeResult{}, false, collectionNotFound(options.Collection)
	}
	manifestIDs := make(map[string]struct{}, len(manifest.Items))
	for _, item := range manifest.Items {
		manifestIDs[item.MD5] = struct{}{}
	}
	for _, id := range options.IDs {
		if _, ok := manifestIDs[id]; !ok {
			return OrganizeResult{}, false, itemNotFound(id)
		}
	}
	for _, id := range options.Order {
		if _, ok := manifestIDs[id]; !ok {
			return OrganizeResult{}, false, itemNotFound(id)
		}
	}

	result := OrganizeResult{}
	changed := false
	if options.MoveTo != "" {
		destinationIndex := collectionIndex(*state, options.MoveTo)
		if destinationIndex < 0 {
			return OrganizeResult{}, false, collectionNotFound(options.MoveTo)
		}
		selected := make(map[string]struct{}, len(options.IDs))
		selectedItems := make(map[string]CollectionItem, len(options.IDs))
		for _, id := range options.IDs {
			if _, duplicate := selected[id]; duplicate {
				return OrganizeResult{}, false, invalidCollections("organize IDs contain duplicates")
			}
			selected[id] = struct{}{}
		}
		if len(selected) == 0 {
			return OrganizeResult{}, false, errorf("validation", "invalid_argument", "Provide at least one favorite ID to move.", "move operation has no IDs")
		}
		for _, id := range options.IDs {
			if !containsCollectionItem(state.Collections[sourceIndex].Items, id) {
				return OrganizeResult{}, false, invalidCollections("item is not a member of the source collection")
			}
		}
		remaining := make([]CollectionItem, 0, len(state.Collections[sourceIndex].Items))
		for _, item := range state.Collections[sourceIndex].Items {
			if _, ok := selected[item.ID]; !ok {
				remaining = append(remaining, item)
				continue
			}
			selectedItems[item.ID] = item
			result.Removed++
			changed = true
		}
		state.Collections[sourceIndex].Items = remaining
		for _, id := range options.IDs {
			item := CollectionItem{ID: id, Position: nextCollectionItemPosition(state.Collections[destinationIndex])}
			if existing, ok := selectedItems[id]; ok {
				item = existing
			}
			item.Position = nextCollectionItemPosition(state.Collections[destinationIndex])
			if containsCollectionItem(state.Collections[destinationIndex].Items, id) {
				continue
			}
			state.Collections[destinationIndex].Items = append(state.Collections[destinationIndex].Items, item)
			result.Moved++
			changed = true
		}
	} else if len(options.IDs) > 0 {
		selected := make(map[string]struct{}, len(options.IDs))
		for _, id := range options.IDs {
			if _, duplicate := selected[id]; duplicate {
				return OrganizeResult{}, false, invalidCollections("organize IDs contain duplicates")
			}
			selected[id] = struct{}{}
		}
		remaining := make([]CollectionItem, 0, len(state.Collections[sourceIndex].Items))
		for _, item := range state.Collections[sourceIndex].Items {
			if _, ok := selected[item.ID]; ok {
				result.Removed++
				changed = true
				continue
			}
			remaining = append(remaining, item)
		}
		state.Collections[sourceIndex].Items = remaining
	}

	if len(options.Order) > 0 {
		if err := reorderCollection(&state.Collections[sourceIndex], options.Order); err != nil {
			return OrganizeResult{}, false, err
		}
		result.Reordered = true
		changed = true
	}
	normalizeCollections(state)
	return result, changed, nil
}

func reorderCollection(collection *Collection, order []string) error {
	if len(order) != len(collection.Items) {
		return invalidCollections("order must contain every collection member exactly once")
	}
	seen := make(map[string]struct{}, len(order))
	items := make(map[string]CollectionItem, len(collection.Items))
	for _, item := range collection.Items {
		items[item.ID] = item
	}
	ordered := make([]CollectionItem, 0, len(order))
	for index, id := range order {
		if _, duplicate := seen[id]; duplicate {
			return invalidCollections("order contains duplicate item IDs")
		}
		item, ok := items[id]
		if !ok {
			return invalidCollections("order contains an item outside the collection")
		}
		seen[id] = struct{}{}
		item.Position = index
		ordered = append(ordered, item)
	}
	collection.Items = ordered
	return nil
}

func collectionRoot(home string) (*library.Library, error) {
	if err := validateCollectionHome(home); err != nil {
		return nil, err
	}
	return library.New(home)
}

func validateCollectionHome(home string) error {
	if home == "" {
		return errorf("validation", "invalid_argument", "Choose a local data directory.", "favorites home is empty")
	}
	return nil
}

func validateCollectionHomeAndName(home, name string) error {
	if err := validateCollectionHome(home); err != nil {
		return err
	}
	if err := validateCollectionID(name); err != nil {
		return err
	}
	return validateCollectionName(name)
}

func validateCollectionID(value string) error {
	if len(value) == 0 || len(value) > 64 || value[0] < 'a' || value[0] > 'z' {
		return errorf("validation", "invalid_argument", "Use a lowercase collection ID up to 64 characters.", "collection ID must start with a lowercase letter")
	}
	for _, character := range value[1:] {
		if !isCollectionIDCharacter(character) {
			return errorf("validation", "invalid_argument", "Use lowercase letters, digits, hyphens, or underscores in the collection ID.", "collection ID contains an unsupported character")
		}
	}
	return nil
}

func validateCollectionName(value string) error {
	if len(value) == 0 || len([]byte(value)) > 128 || !utf8.ValidString(value) {
		return errorf("validation", "invalid_argument", "Use a non-empty UTF-8 collection name up to 128 bytes.", "collection name is invalid")
	}
	return nil
}

func validateItemID(value string) error {
	if len(value) != 32 || strings.ToLower(value) != value {
		return errorf("validation", "invalid_argument", "Use a complete lowercase MD5 item ID.", "favorite item ID is invalid")
	}
	for _, character := range value {
		if !isItemIDCharacter(character) {
			return errorf("validation", "invalid_argument", "Use a complete lowercase MD5 item ID.", "favorite item ID is invalid")
		}
	}
	return nil
}

func isCollectionIDCharacter(character rune) bool {
	return character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-' || character == '_'
}

func isItemIDCharacter(character rune) bool {
	return character >= 'a' && character <= 'f' || character >= '0' && character <= '9'
}

func readCollections(ctx context.Context, root *library.Library, manifest library.Manifest) (Collections, error) {
	data, err := library.ReadRelative(ctx, root.Root, CollectionsRelativePath, library.MaxManifestBytes)
	if errors.Is(err, os.ErrNotExist) {
		state := defaultCollections(manifest)
		return state, nil
	}
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return Collections{}, contextError(ctx)
		}
		var coded *library.Error
		if errors.As(err, &coded) && coded.Kind == "validation" {
			return Collections{}, err
		}
		if !errors.As(err, &coded) {
			return Collections{}, errorf("io", "read_failed", "Check the collections metadata permissions.", "cannot read collections metadata")
		}
		return Collections{}, invalidCollections("cannot read collections metadata")
	}
	state, err := decodeCollections(data)
	if err != nil {
		return Collections{}, err
	}
	if err := validateCollections(state, manifest); err != nil {
		return Collections{}, err
	}
	ensureUnassignedItems(&state, manifest)
	normalizeCollections(&state)
	return state, nil
}

func readOptionalExtension(ctx context.Context, root *library.Library, manifest library.Manifest) (Collections, bool, error) {
	data, err := library.ReadRelative(ctx, root.Root, CollectionsExtensionName, library.MaxManifestBytes)
	if errors.Is(err, os.ErrNotExist) {
		return defaultCollections(manifest), false, nil
	}
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return Collections{}, false, contextError(ctx)
		}
		var coded *library.Error
		if errors.As(err, &coded) && coded.Kind == "validation" {
			return Collections{}, false, err
		}
		if !errors.As(err, &coded) {
			return Collections{}, false, errorf("io", "read_failed", "Check the collections extension permissions.", "cannot read collections extension")
		}
		return Collections{}, false, invalidCollections("cannot read collections extension")
	}
	state, err := decodeCollections(data)
	if err != nil {
		return Collections{}, false, err
	}
	if err := validateCollections(state, manifest); err != nil {
		return Collections{}, false, err
	}
	ensureUnassignedItems(&state, manifest)
	normalizeCollections(&state)
	return state, true, nil
}

func writeCollections(ctx context.Context, root *library.Library, state Collections, manifest library.Manifest) error {
	if err := validateCollections(state, manifest); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return errorf("internal", "unexpected", "Retry the collection update.", "encode collections metadata: %v", err)
	}
	data = append(data, '\n')
	if len(data) > library.MaxManifestBytes {
		return errorf("validation", "output_limit", "Reduce the number of collection entries and retry.", "collections metadata exceeds the %d byte limit", library.MaxManifestBytes)
	}
	return root.WriteRelativeAtomic(ctx, CollectionsRelativePath, data)
}

func decodeCollections(data []byte) (Collections, error) {
	if !utf8.Valid(data) || len(data) == 0 {
		return Collections{}, invalidCollections("collections metadata is not valid UTF-8 JSON")
	}
	if err := rejectDuplicateJSONKeys(data); err != nil {
		return Collections{}, invalidCollections("collections metadata contains duplicate JSON keys")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var state Collections
	if err := decoder.Decode(&state); err != nil {
		return Collections{}, invalidCollections("collections metadata JSON is invalid")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return Collections{}, invalidCollections("collections metadata contains trailing JSON")
	}
	return state, nil
}

func validateCollections(state Collections, manifest library.Manifest) error {
	if state.SchemaVersion != 1 || state.Collections == nil || len(state.Collections) == 0 {
		return invalidCollections("collections metadata must contain schema_version 1 and at least one collection")
	}
	manifestIDs := make(map[string]struct{}, len(manifest.Items))
	for _, item := range manifest.Items {
		manifestIDs[item.MD5] = struct{}{}
	}
	seenCollections := make(map[string]struct{}, len(state.Collections))
	seenPositions := make(map[int]struct{}, len(state.Collections))
	defaultFound := false
	for _, collection := range state.Collections {
		if err := validateCollectionID(collection.ID); err != nil {
			return invalidCollections("collection ID is invalid")
		}
		if err := validateCollectionName(collection.Name); err != nil {
			return invalidCollections("collection name is invalid")
		}
		if collection.Position < 0 {
			return invalidCollections("collection position is negative")
		}
		if _, duplicate := seenCollections[collection.ID]; duplicate {
			return invalidCollections("collection IDs must be unique")
		}
		if _, duplicate := seenPositions[collection.Position]; duplicate {
			return invalidCollections("collection positions must be unique")
		}
		seenCollections[collection.ID] = struct{}{}
		seenPositions[collection.Position] = struct{}{}
		if collection.ID == DefaultCollectionID {
			defaultFound = true
		}
		if collection.Items == nil {
			return invalidCollections("collection items must be an array")
		}
		seenItems := make(map[string]struct{}, len(collection.Items))
		seenItemPositions := make(map[int]struct{}, len(collection.Items))
		for _, item := range collection.Items {
			if err := validateItemID(item.ID); err != nil {
				return invalidCollections("collection item ID is invalid")
			}
			if _, exists := manifestIDs[item.ID]; !exists {
				return invalidCollections("collection item does not exist in the personal manifest")
			}
			if _, duplicate := seenItems[item.ID]; duplicate {
				return invalidCollections("collection item IDs must be unique")
			}
			if item.Position < 0 {
				return invalidCollections("collection item position is negative")
			}
			if _, duplicate := seenItemPositions[item.Position]; duplicate {
				return invalidCollections("collection item positions must be unique")
			}
			if item.AddedAt != "" {
				if _, err := time.Parse(time.RFC3339Nano, item.AddedAt); err != nil {
					return invalidCollections("collection item added_at is invalid")
				}
			}
			seenItems[item.ID] = struct{}{}
			seenItemPositions[item.Position] = struct{}{}
		}
	}
	if !defaultFound {
		return invalidCollections("default favorites collection is missing")
	}
	return nil
}

func defaultCollections(manifest library.Manifest) Collections {
	items := make([]CollectionItem, 0, len(manifest.Items))
	for index, item := range manifest.Items {
		items = append(items, CollectionItem{ID: item.MD5, Position: index})
	}
	return Collections{SchemaVersion: 1, Collections: []Collection{{ID: DefaultCollectionID, Name: DefaultCollectionName, Position: 0, Items: items}}}
}

func normalizeCollections(state *Collections) {
	if state == nil {
		return
	}
	sort.SliceStable(state.Collections, func(i, j int) bool {
		if state.Collections[i].Position != state.Collections[j].Position {
			return state.Collections[i].Position < state.Collections[j].Position
		}
		return state.Collections[i].ID < state.Collections[j].ID
	})
	for index := range state.Collections {
		sort.SliceStable(state.Collections[index].Items, func(i, j int) bool {
			if state.Collections[index].Items[i].Position != state.Collections[index].Items[j].Position {
				return state.Collections[index].Items[i].Position < state.Collections[index].Items[j].Position
			}
			return state.Collections[index].Items[i].ID < state.Collections[index].Items[j].ID
		})
	}
}

func ensureUnassignedItems(state *Collections, manifest library.Manifest) {
	defaultIndex := collectionIndex(*state, DefaultCollectionID)
	if defaultIndex < 0 {
		return
	}
	members := make(map[string]struct{}, len(manifest.Items))
	for _, collection := range state.Collections {
		for _, item := range collection.Items {
			members[item.ID] = struct{}{}
		}
	}
	position := nextCollectionItemPosition(state.Collections[defaultIndex])
	for _, item := range manifest.Items {
		if _, ok := members[item.MD5]; ok {
			continue
		}
		state.Collections[defaultIndex].Items = append(state.Collections[defaultIndex].Items, CollectionItem{ID: item.MD5, Position: position})
		position++
	}
}

func findCollection(state Collections, id string) (*Collection, bool) {
	for index := range state.Collections {
		if state.Collections[index].ID == id {
			return &state.Collections[index], true
		}
	}
	return nil, false
}

func collectionByID(state Collections, id string) *Collection {
	collection, _ := findCollection(state, id)
	return collection
}

func collectionIndex(state Collections, id string) int {
	for index := range state.Collections {
		if state.Collections[index].ID == id {
			return index
		}
	}
	return -1
}

func replaceCollection(collections []Collection, replacement Collection) []Collection {
	result := append([]Collection(nil), collections...)
	for index := range result {
		if result[index].ID == replacement.ID {
			result[index] = replacement
			return result
		}
	}
	return result
}

func nextCollectionPosition(state Collections) int {
	position := 0
	for _, collection := range state.Collections {
		if collection.Position >= position {
			position = collection.Position + 1
		}
	}
	return position
}

func appendCollectionItems(collection *Collection, items []CollectionItem) int {
	if collection == nil {
		return 0
	}
	moved := 0
	for _, item := range items {
		if containsCollectionItem(collection.Items, item.ID) {
			continue
		}
		item.Position = nextCollectionItemPosition(*collection)
		collection.Items = append(collection.Items, item)
		moved++
	}
	return moved
}

func mergeCollectionItemsInSourceOrder(collection *Collection, source []CollectionItem) {
	if collection == nil {
		return
	}
	// The source extension is authoritative for a newly created target's
	// imported relationship and its manual order. Entries that only exist in
	// the target are restored to the default collection by ensureUnassignedItems.
	collection.Items = append([]CollectionItem(nil), source...)
	for index := range collection.Items {
		collection.Items[index].Position = index
	}
}

func nextCollectionItemPosition(collection Collection) int {
	position := 0
	for _, item := range collection.Items {
		if item.Position >= position {
			position = item.Position + 1
		}
	}
	return position
}

func containsCollectionItem(items []CollectionItem, id string) bool {
	_, ok := findCollectionItem(items, id)
	return ok
}

func findCollectionItem(items []CollectionItem, id string) (CollectionItem, bool) {
	for _, item := range items {
		if item.ID == id {
			return item, true
		}
	}
	return CollectionItem{}, false
}

func invalidCollections(reason string) error {
	return errorf("integrity", "invalid_collection", "Repair or remove the invalid collections metadata before retrying.", "%s", reason)
}

func collectionNotFound(id string) error {
	return errorf("not_found", "collection_not_found", "Choose a collection listed by favorites collections list.", "collection %q was not found", id)
}

func collectionExists(id string) error {
	return errorf("conflict", "collection_exists", "Choose a different collection ID.", "collection %q already exists", id)
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
		_, err = decoder.Token()
	case '[':
		for decoder.More() {
			if err := scanJSONValue(decoder); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
	}
	return err
}
