package search

import (
	"context"
	"encoding/hex"

	"github.com/9Ashwin/sticker-cli/internal/library"
)

// Find locates one item across the personal manifest and installed pack
// manifests. It reads metadata only; callers must verify the returned path
// before using it.
func Find(ctx context.Context, home, id string) (Item, error) {
	if err := contextError(ctx); err != nil {
		return Item{}, err
	}
	if home == "" {
		return Item{}, errorf("validation", "invalid_argument", "Choose a local data directory.", "search home is empty")
	}
	if len(id) != 32 {
		return Item{}, errorf("validation", "invalid_argument", "Use a lowercase 32-character MD5 ID.", "invalid item ID")
	}
	if _, err := hex.DecodeString(id); err != nil || id != lowerASCII(id) {
		return Item{}, errorf("validation", "invalid_argument", "Use a lowercase 32-character MD5 ID.", "invalid item ID")
	}

	root, err := library.New(home)
	if err != nil {
		return Item{}, err
	}
	personal, err := root.ReadManifest(ctx)
	if err != nil {
		return Item{}, err
	}
	packStates, err := readPackStates(ctx, root.Root)
	if err != nil {
		return Item{}, err
	}
	merged, err := merge(root, personal, packStates)
	if err != nil {
		return Item{}, err
	}
	for _, item := range merged {
		if item.ID == id {
			return item, nil
		}
	}
	return Item{}, errorf("not_found", "item_not_found", "Choose an ID listed by the library.", "item %s was not found", id)
}

func lowerASCII(value string) string {
	var changed bool
	result := []byte(value)
	for index, character := range result {
		if character >= 'A' && character <= 'F' {
			result[index] = character + ('a' - 'A')
			changed = true
		}
	}
	if changed {
		return string(result)
	}
	return value
}
