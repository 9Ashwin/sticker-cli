// Package search provides offline caption search over the personal library and
// installed pack manifests.
package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/9Ashwin/sticker-cli/internal/library"
)

const (
	packStateDirectory = ".sticker/packs"
	maxPackStateBytes  = 8 << 20
)

// Options controls one offline search.
type Options struct {
	Home      string
	Query     string
	Pack      string
	Favorites bool
	Limit     int
	Offset    int
}

// Result is the bounded, resumable search response.
type Result struct {
	Items         []Item `json:"items"`
	Total         int    `json:"total"`
	NextOffset    int    `json:"next_offset"`
	HasMore       bool   `json:"has_more"`
	SetupRequired bool   `json:"setup_required,omitempty"`
}

// Item is the metadata returned for a searchable image. Search deliberately
// does not read image bytes; get performs the complete integrity check.
type Item struct {
	ID          string   `json:"id"`
	MD5         string   `json:"md5"`
	SHA256      string   `json:"sha256"`
	Filename    string   `json:"filename"`
	Format      string   `json:"format"`
	Size        int64    `json:"size"`
	Caption     string   `json:"caption"`
	Path        string   `json:"path"`
	Favorite    bool     `json:"favorite"`
	Packs       []string `json:"packs"`
	PreviewPath string   `json:"preview_path,omitempty"`
}

type packState struct {
	SchemaVersion int              `json:"schema_version"`
	ID            string           `json:"id"`
	Source        string           `json:"source"`
	Revision      string           `json:"revision"`
	InstalledAt   string           `json:"installed_at"`
	Manifest      library.Manifest `json:"manifest"`
}

type candidate struct {
	item     library.Item
	caption  string
	favorite bool
	packs    map[string]struct{}
	personal *library.Item
	packItem map[string]library.Item
}

// Execute searches only local manifests. It performs no network access and
// does not validate image contents, so a later get can report a missing or
// damaged image without making search expensive for large collections.
func Execute(ctx context.Context, options Options) (Result, error) {
	if err := contextError(ctx); err != nil {
		return Result{}, err
	}
	if options.Home == "" {
		return Result{}, errorf("validation", "invalid_argument", "Choose a local data directory.", "search home is empty")
	}
	if options.Limit < 1 || options.Limit > 100 {
		return Result{}, errorf("validation", "invalid_argument", "Choose a page size from 1 through 100.", "limit must be between 1 and 100")
	}
	if options.Offset < 0 {
		return Result{}, errorf("validation", "invalid_argument", "Choose an offset of 0 or greater.", "offset cannot be negative")
	}
	if options.Pack != "" && !validPackID(options.Pack) {
		return Result{}, errorf("validation", "invalid_argument", "Use a valid pack ID.", "pack ID %q is invalid", options.Pack)
	}

	root, err := library.New(options.Home)
	if err != nil {
		return Result{}, err
	}
	personal, err := root.ReadManifest(ctx)
	if err != nil {
		return Result{}, err
	}
	packStates, err := readPackStates(ctx, root.Root)
	if err != nil {
		return Result{}, err
	}
	selectedPacks, err := selectPacks(packStates, options.Pack)
	if err != nil {
		return Result{}, err
	}

	merged, err := merge(root, personal, selectedPacks)
	if err != nil {
		return Result{}, err
	}
	query := strings.ToLower(options.Query)
	filtered := make([]Item, 0, len(merged))
	for _, item := range merged {
		if options.Pack != "" && !contains(item.Packs, options.Pack) {
			continue
		}
		if options.Favorites && !item.Favorite {
			continue
		}
		if !strings.Contains(strings.ToLower(item.Caption), query) {
			continue
		}
		filtered = append(filtered, item)
	}

	result := Result{
		Items:         make([]Item, 0),
		Total:         len(filtered),
		NextOffset:    options.Offset,
		HasMore:       false,
		SetupRequired: options.Pack == "" && len(selectedPacks) == 0 && len(personal.Items) == 0,
	}
	if options.Offset < len(filtered) {
		end := min(options.Offset+options.Limit, len(filtered))
		result.Items = append(result.Items, filtered[options.Offset:end]...)
		result.NextOffset = end
		result.HasMore = end < len(filtered)
	}
	if err := contextError(ctx); err != nil {
		return Result{}, err
	}
	return result, nil
}

func merge(root *library.Library, personal library.Manifest, packs []packState) ([]Item, error) {
	byID := make(map[string]*candidate, len(personal.Items))
	for index := range personal.Items {
		item := personal.Items[index]
		if err := addCandidate(byID, item, "", true); err != nil {
			return nil, err
		}
	}
	for _, pack := range packs {
		for _, item := range pack.Manifest.Items {
			if err := addCandidate(byID, item, pack.ID, false); err != nil {
				return nil, err
			}
		}
	}

	items := make([]Item, 0, len(byID))
	for _, value := range byID {
		if value.personal != nil {
			value.item = *value.personal
			value.caption = value.personal.Caption
			value.favorite = true
		} else {
			packIDs := make([]string, 0, len(value.packItem))
			for id := range value.packItem {
				packIDs = append(packIDs, id)
			}
			sort.Strings(packIDs)
			for _, id := range packIDs {
				if caption := value.packItem[id].Caption; caption != "" {
					value.caption = caption
					break
				}
			}
		}
		packIDs := make([]string, 0, len(value.packs))
		for id := range value.packs {
			packIDs = append(packIDs, id)
		}
		sort.Strings(packIDs)
		path, err := root.ItemPath(value.item)
		if err != nil {
			return nil, err
		}
		items = append(items, Item{
			ID:       value.item.MD5,
			MD5:      value.item.MD5,
			SHA256:   value.item.SHA256,
			Filename: value.item.Filename,
			Format:   value.item.Format,
			Size:     value.item.Size,
			Caption:  value.caption,
			Path:     path,
			Favorite: value.favorite,
			Packs:    packIDs,
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func addCandidate(byID map[string]*candidate, item library.Item, packID string, personal bool) error {
	value := byID[item.MD5]
	if value == nil {
		value = &candidate{item: item, packs: make(map[string]struct{}), packItem: make(map[string]library.Item)}
		byID[item.MD5] = value
	} else if value.item.SHA256 != item.SHA256 {
		return errorf("conflict", "digest_conflict", "Remove the conflicting manifest entry before searching.", "item %s has different SHA-256 values", item.MD5)
	}
	if personal {
		copy := item
		value.personal = &copy
		return nil
	}
	value.packs[packID] = struct{}{}
	value.packItem[packID] = item
	return nil
}

func selectPacks(states []packState, wanted string) ([]packState, error) {
	if wanted == "" {
		return states, nil
	}
	for _, state := range states {
		if state.ID == wanted {
			return []packState{state}, nil
		}
	}
	return nil, errorf("not_found", "pack_not_found", "Install the pack before searching it.", "pack %s is not installed", wanted)
}

func readPackStates(ctx context.Context, root string) ([]packState, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if err := rejectUnsafeRoot(root); err != nil {
		return nil, err
	}
	directory := filepath.Join(root, packStateDirectory)
	info, err := os.Lstat(directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, wrapError("io", "read_failed", "Check the installed pack directory.", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, errorf("validation", "unsafe_path", "Remove links from the local data directory.", "installed pack directory is not a real directory")
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, wrapError("io", "read_failed", "Check the installed pack directory.", err)
	}
	states := make([]packState, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		if err := contextError(ctx); err != nil {
			return nil, err
		}
		path := filepath.Join(directory, entry.Name())
		fileInfo, statErr := os.Lstat(path)
		if statErr != nil {
			return nil, wrapError("io", "read_failed", "Check the installed pack state.", statErr)
		}
		if fileInfo.Mode()&os.ModeSymlink != 0 {
			return nil, errorf("validation", "unsafe_path", "Remove links from the local data directory.", "pack state is a symbolic link")
		}
		data, readErr := readBounded(path, maxPackStateBytes)
		if readErr != nil {
			return nil, wrapError("io", "read_failed", "Check the installed pack state.", readErr)
		}
		var state packState
		if err := json.Unmarshal(data, &state); err != nil {
			return nil, errorf("integrity", "invalid_manifest", "Repair or reinstall the selected pack.", "pack state %s is invalid JSON", entry.Name())
		}
		if err := validatePackState(state, strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))); err != nil {
			return nil, err
		}
		states = append(states, state)
	}
	sort.Slice(states, func(i, j int) bool { return states[i].ID < states[j].ID })
	return states, nil
}

func validatePackState(state packState, filenameID string) error {
	if !validPackID(state.ID) || state.ID != filenameID {
		return errorf("integrity", "invalid_manifest", "Repair or reinstall the selected pack.", "pack state ID does not match its filename")
	}
	if state.SchemaVersion != 1 {
		return errorf("integrity", "invalid_manifest", "Install a pack with schema version 1.", "pack state %s has unsupported schema version %d", state.ID, state.SchemaVersion)
	}
	return library.ValidateManifest(state.Manifest, library.DefaultLimits())
}

func readBounded(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("file exceeds %d byte limit", limit)
	}
	return data, nil
}

func rejectUnsafeRoot(root string) error {
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return wrapError("io", "read_failed", "Check the local data directory.", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errorf("validation", "unsafe_path", "Choose a real local data directory.", "local data root is not a real directory")
	}
	return nil
}

func contextError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return errorf("cancelled", "interrupted", "Retry the operation when ready.", "operation cancelled: %v", err)
	}
	return nil
}

func errorf(kind, subtype, hint, format string, args ...any) *library.Error {
	return &library.Error{Kind: kind, Subtype: subtype, Hint: hint, Message: fmt.Sprintf(format, args...)}
}

func wrapError(kind, subtype, hint string, err error) *library.Error {
	return &library.Error{Kind: kind, Subtype: subtype, Hint: hint, Message: err.Error(), Err: err}
}

func validPackID(value string) bool {
	if value == "" || len(value) > 64 || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for _, character := range value[1:] {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' && character != '_' {
			return false
		}
	}
	return true
}

func contains(values []string, wanted string) bool {
	index := sort.SearchStrings(values, wanted)
	return index < len(values) && values[index] == wanted
}
