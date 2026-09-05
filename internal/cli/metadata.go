package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

func registerFlag(command command, metadata flagMetadata) {
	switch metadata.Type {
	case "bool":
		command.Command.Flags().BoolP(metadata.Name, metadata.Shorthand, boolDefault(metadata.Default), metadata.Description)
	case "int":
		command.Command.Flags().IntP(metadata.Name, metadata.Shorthand, intDefault(metadata.Default), metadata.Description)
	case "string[]":
		command.Command.Flags().StringSliceP(metadata.Name, metadata.Shorthand, stringSliceDefault(metadata.Default), metadata.Description)
	default:
		command.Command.Flags().StringP(metadata.Name, metadata.Shorthand, stringDefault(metadata.Default), metadata.Description)
	}
	if metadata.Required {
		_ = command.Command.MarkFlagRequired(metadata.Name)
	}
}

func joinExamples(examples []string) string {
	return strings.Join(examples, "\n  ")
}

func objectSchema(fields ...string) map[string]any {
	properties := make(map[string]any, len(fields)/2)
	for i := 0; i+1 < len(fields); i += 2 {
		properties[fields[i]] = map[string]any{"type": jsonType(fields[i+1])}
	}
	return map[string]any{"type": "object", "properties": properties}
}

func getResultSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"item": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":           map[string]any{"type": "string"},
					"md5":          map[string]any{"type": "string"},
					"sha256":       map[string]any{"type": "string"},
					"filename":     map[string]any{"type": "string"},
					"format":       map[string]any{"type": "string"},
					"size":         map[string]any{"type": "integer"},
					"caption":      map[string]any{"type": "string"},
					"path":         map[string]any{"type": "string"},
					"favorite":     map[string]any{"type": "boolean"},
					"packs":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"preview_path": map[string]any{"type": "string"},
				},
			},
		},
	}
}

func defaultErrors() []schemaError {
	return []schemaError{
		{Type: "validation", Subtype: "invalid_argument", ExitCode: 2},
		{Type: "validation", Subtype: "unsupported_schema", ExitCode: 2},
		{Type: "validation", Subtype: "unsafe_path", ExitCode: 2},
		{Type: "validation", Subtype: "output_limit", ExitCode: 2},
		{Type: "validation", Subtype: "unsupported_format", ExitCode: 2},
		{Type: "not_found", Subtype: "item_not_found", ExitCode: 3},
		{Type: "not_found", Subtype: "collection_not_found", ExitCode: 3},
		{Type: "not_found", Subtype: "pack_not_found", ExitCode: 3},
		{Type: "not_found", Subtype: "source_not_found", ExitCode: 3},
		{Type: "network", Subtype: "timeout", ExitCode: 4},
		{Type: "network", Subtype: "request_failed", ExitCode: 4},
		{Type: "network", Subtype: "http_error", ExitCode: 4},
		{Type: "integrity", Subtype: "hash_mismatch", ExitCode: 5},
		{Type: "integrity", Subtype: "invalid_manifest", ExitCode: 5},
		{Type: "integrity", Subtype: "invalid_image", ExitCode: 5},
		{Type: "integrity", Subtype: "invalid_collection", ExitCode: 5},
		{Type: "conflict", Subtype: "digest_conflict", ExitCode: 6},
		{Type: "conflict", Subtype: "source_conflict", ExitCode: 6},
		{Type: "conflict", Subtype: "destination_exists", ExitCode: 6},
		{Type: "conflict", Subtype: "library_busy", ExitCode: 6},
		{Type: "conflict", Subtype: "state_changed", ExitCode: 6},
		{Type: "io", Subtype: "permission_denied", ExitCode: 7},
		{Type: "io", Subtype: "disk_full", ExitCode: 7},
		{Type: "io", Subtype: "read_failed", ExitCode: 7},
		{Type: "io", Subtype: "write_failed", ExitCode: 7},
		{Type: "internal", Subtype: "unexpected", ExitCode: 1},
		{Type: "internal", Subtype: "unimplemented", ExitCode: 1},
		{Type: "cancelled", Subtype: "interrupted", ExitCode: 130},
	}
}

func helpMetadata() commandMetadata {
	return commandMetadata{
		Path:        "help",
		Use:         "help [command...]",
		Summary:     "Describe a command for humans",
		Description: "Render a command's help text inside the selected output envelope. Without a path, show the root command.",
		Effect:      effectRead,
		Parameters:  []parameterMetadata{{Name: "command", Type: "string[]", Source: "argument", Description: "Optional command path to describe"}},
		Result:      objectSchema("help", "string"),
		Errors:      defaultErrors(),
		Examples:    []string{"sticker help", "sticker help packs install"},
		Args:        cobra.ArbitraryArgs,
	}
}

func packListMetadata() commandMetadata {
	return commandMetadata{
		Path:        "packs list",
		Use:         "list",
		Summary:     "List available image packs",
		Description: "Read pack metadata without downloading original image files. Use --offline to require a cached catalog.",
		Effect:      effectRead,
		Flags: []flagMetadata{
			{Name: "source", Type: "string", Description: "Pack directory or HTTPS source"},
			{Name: "offline", Type: "bool", Default: false, Description: "Use only cached catalog data"},
		},
		Result:   objectSchema("items", "object[]", "fetched_at", "string", "stale", "bool"),
		Errors:   defaultErrors(),
		Examples: []string{"sticker packs list", "sticker packs list --offline"},
	}
}

func packInstallMetadata() commandMetadata {
	return commandMetadata{
		Path:        "packs install",
		Use:         "install ID",
		Summary:     "Install one selectable image pack",
		Description: "Install only the explicitly selected pack. Use --dry-run to inspect planned downloads without writing the local library.",
		Effect:      effectWrite,
		Parameters:  []parameterMetadata{{Name: "id", Type: "string", Source: "argument", Description: "Pack ID", Required: true}},
		Flags: []flagMetadata{
			{Name: "source", Type: "string", Description: "Pack directory or HTTPS source"},
			{Name: "dry-run", Type: "bool", Default: false, Description: "Show the plan without downloading or writing"},
		},
		Result:   objectSchema("source", "string", "target", "string", "pack", "object", "revision", "string", "added", "int", "reused", "int", "download_bytes", "int", "dry_run", "bool"),
		Errors:   defaultErrors(),
		Examples: []string{"sticker packs install curated", "sticker packs install all --dry-run"},
		Args:     cobra.ExactArgs(1),
	}
}

func packUpdateMetadata() commandMetadata {
	return commandMetadata{
		Path:        "packs update",
		Use:         "update ID",
		Summary:     "Refresh one installed image pack",
		Description: "Refresh the selected pack from its saved source while preserving the last usable installation if the update fails.",
		Effect:      effectWrite,
		Parameters:  []parameterMetadata{{Name: "id", Type: "string", Source: "argument", Description: "Installed pack ID", Required: true}},
		Flags:       []flagMetadata{{Name: "dry-run", Type: "bool", Default: false, Description: "Show the plan without writing"}},
		Result:      objectSchema("source", "string", "target", "string", "pack", "object", "revision", "string", "added", "int", "reused", "int", "download_bytes", "int", "dry_run", "bool"),
		Errors:      defaultErrors(),
		Examples:    []string{"sticker packs update curated", "sticker packs update curated --dry-run"},
		Args:        cobra.ExactArgs(1),
	}
}

func packRemoveMetadata() commandMetadata {
	return commandMetadata{
		Path:        "packs remove",
		Use:         "remove ID",
		Summary:     "Remove one installed pack relationship",
		Description: "Remove the selected pack state and report original files retained for other references.",
		Effect:      effectWrite,
		Parameters:  []parameterMetadata{{Name: "id", Type: "string", Source: "argument", Description: "Installed pack ID", Required: true}},
		Flags:       []flagMetadata{{Name: "dry-run", Type: "bool", Default: false, Description: "Show the plan without writing"}},
		Result:      objectSchema("removed", "bool", "retained_bytes", "int"),
		Errors:      defaultErrors(),
		Examples:    []string{"sticker packs remove curated", "sticker packs remove curated --dry-run"},
		Args:        cobra.ExactArgs(1),
	}
}

func searchMetadata() commandMetadata {
	return commandMetadata{
		Path:        "search",
		Use:         "search QUERY",
		Summary:     "Search installed packs and personal images",
		Description: "Search captions offline with a case-insensitive substring match. Results are bounded and sorted by item ID.",
		Effect:      effectRead,
		Parameters:  []parameterMetadata{{Name: "query", Type: "string", Source: "argument", Description: "Caption substring", Required: true}},
		Flags: []flagMetadata{
			{Name: "pack", Type: "string", Description: "Restrict results to one pack"},
			{Name: "favorites", Type: "bool", Default: false, Description: "Restrict results to personal favorites"},
			{Name: "limit", Type: "int", Default: 10, Description: "Maximum results (1-100)"},
			{Name: "offset", Type: "int", Default: 0, Description: "Number of matching results to skip"},
		},
		Result:   objectSchema("items", "object[]", "total", "int", "next_offset", "int", "has_more", "bool"),
		Errors:   defaultErrors(),
		Examples: []string{"sticker search 收到 --limit 5", "sticker search coffee --favorites"},
		Args:     cobra.ExactArgs(1),
	}
}

func getMetadata() commandMetadata {
	return commandMetadata{
		Path:        "get",
		Use:         "get ID",
		Summary:     "Return one verified original image path",
		Description: "Verify the selected item before returning its absolute local path. Use --preview to generate or reuse a PNG preview for a static WebP without changing the original. The result never embeds image bytes.",
		Effect:      effectRead,
		Parameters:  []parameterMetadata{{Name: "id", Type: "string", Source: "argument", Description: "Complete lowercase MD5 item ID", Required: true}},
		Flags:       []flagMetadata{{Name: "preview", Type: "bool", Default: false, Description: "Generate or reuse a PNG preview for a static WebP"}},
		Result:      getResultSchema(),
		Errors:      defaultErrors(),
		Examples:    []string{"sticker get 0123456789abcdef0123456789abcdef", "sticker get 0123456789abcdef0123456789abcdef --preview"},
		Args:        cobra.ExactArgs(1),
	}
}

func setupMetadata() commandMetadata {
	return commandMetadata{
		Path:        "setup",
		Use:         "setup",
		Summary:     "Initialize one selectable image pack",
		Description: "Install the curated pack by default. Pass --pack all to explicitly select the full pack; setup reuses the regular packs install contract.",
		Effect:      effectWrite,
		Flags: []flagMetadata{
			{Name: "pack", Type: "string", Default: "curated", Description: "Pack to install: curated (default) or all"},
			{Name: "source", Type: "string", Description: "Pack directory or HTTPS source"},
			{Name: "dry-run", Type: "bool", Default: false, Description: "Show the plan without downloading or writing"},
		},
		Result:   objectSchema("setup", "bool", "pack", "string", "revision", "string", "added", "int", "reused", "int", "download_bytes", "int"),
		Errors:   defaultErrors(),
		Examples: []string{"sticker setup", "sticker setup --pack curated --dry-run", "sticker setup --pack all"},
		Args:     cobra.NoArgs,
	}
}

func favoriteAddMetadata() commandMetadata {
	return commandMetadata{
		Path:        "favorites add",
		Use:         "add [PATH]",
		Summary:     "Add a local original image to personal favorites",
		Description: "Provide exactly one local PATH or --id from an installed item. A caption is optional and is stored in the standard personal manifest.",
		Effect:      effectWrite,
		Parameters:  []parameterMetadata{{Name: "path", Type: "string", Source: "argument", Description: "Local original image path"}},
		Flags: []flagMetadata{
			{Name: "id", Type: "string", Description: "Installed item ID"},
			{Name: "caption", Type: "string", Description: "Personal caption"},
			{Name: "dry-run", Type: "bool", Default: false, Description: "Validate without writing"},
		},
		Result:   objectSchema("item", "object", "added", "bool", "updated", "bool", "dry_run", "bool"),
		Errors:   defaultErrors(),
		Examples: []string{"sticker favorites add ./reaction.gif --caption '收到'", "sticker favorites add --id 0123456789abcdef0123456789abcdef"},
		Args:     cobra.MaximumNArgs(1),
	}
}

func favoriteListMetadata() commandMetadata {
	return commandMetadata{
		Path:        "favorites list",
		Use:         "list",
		Summary:     "List personal favorites",
		Description: "List personal manifest entries with stable bounded pagination.",
		Effect:      effectRead,
		Flags: []flagMetadata{
			{Name: "collection", Type: "string", Description: "Restrict results to one collection"},
			{Name: "sort", Type: "string", Default: "manual", Description: "Sort by manual, added, caption, or md5"},
			{Name: "limit", Type: "int", Default: 10, Description: "Maximum results (1-100)"},
			{Name: "offset", Type: "int", Default: 0, Description: "Number of results to skip"},
		},
		Result:   objectSchema("items", "object[]", "total", "int", "next_offset", "int", "has_more", "bool"),
		Errors:   defaultErrors(),
		Examples: []string{"sticker favorites list", "sticker favorites list --limit 20"},
	}
}

func favoriteCollectionsListMetadata() commandMetadata {
	return commandMetadata{
		Path:        "favorites collections list",
		Use:         "list",
		Summary:     "List personal favorite collections",
		Description: "List the default and custom collections with their member counts.",
		Effect:      effectRead,
		Result:      objectSchema("collections", "object[]"),
		Errors:      defaultErrors(),
		Examples:    []string{"sticker favorites collections list"},
		Args:        cobra.NoArgs,
	}
}

func favoriteCollectionsCreateMetadata() commandMetadata {
	return commandMetadata{
		Path:        "favorites collections create",
		Use:         "create NAME",
		Summary:     "Create a personal favorite collection",
		Description: "Create a custom collection without copying any original image files.",
		Effect:      effectWrite,
		Parameters:  []parameterMetadata{{Name: "name", Type: "string", Source: "argument", Description: "Collection name", Required: true}},
		Result:      objectSchema("collection", "object", "changed", "bool"),
		Errors:      defaultErrors(),
		Examples:    []string{"sticker favorites collections create work"},
		Args:        cobra.ExactArgs(1),
	}
}

func favoriteCollectionsRenameMetadata() commandMetadata {
	return commandMetadata{
		Path:        "favorites collections rename",
		Use:         "rename ID NAME",
		Summary:     "Rename a personal favorite collection",
		Description: "Replace a custom collection name while retaining its members and order.",
		Effect:      effectWrite,
		Parameters: []parameterMetadata{
			{Name: "id", Type: "string", Source: "argument", Description: "Collection ID", Required: true},
			{Name: "name", Type: "string", Source: "argument", Description: "New collection name", Required: true},
		},
		Result:   objectSchema("collection", "object", "changed", "bool"),
		Errors:   defaultErrors(),
		Examples: []string{"sticker favorites collections rename work team"},
		Args:     cobra.ExactArgs(2),
	}
}

func favoriteCollectionsRemoveMetadata() commandMetadata {
	return commandMetadata{
		Path:        "favorites collections remove",
		Use:         "remove ID",
		Summary:     "Remove a personal favorite collection",
		Description: "Remove a custom collection after explicitly handling its member relationships.",
		Effect:      effectWrite,
		Parameters:  []parameterMetadata{{Name: "id", Type: "string", Source: "argument", Description: "Collection ID", Required: true}},
		Result:      objectSchema("removed", "bool", "moved", "int"),
		Errors:      defaultErrors(),
		Examples:    []string{"sticker favorites collections remove work"},
		Args:        cobra.ExactArgs(1),
	}
}

func favoriteOrganizeMetadata() commandMetadata {
	return commandMetadata{
		Path:        "favorites organize",
		Use:         "organize",
		Summary:     "Reorganize personal favorite collections",
		Description: "Move, reorder, or remove favorite relationships atomically. Use --dry-run to inspect the change first.",
		Effect:      effectWrite,
		Flags: []flagMetadata{
			{Name: "collection", Type: "string", Description: "Collection to update", Required: true},
			{Name: "ids", Type: "string[]", Description: "Favorite IDs to move or remove"},
			{Name: "move-to", Type: "string", Description: "Destination collection ID"},
			{Name: "order", Type: "string[]", Description: "Complete favorite ID order for the collection"},
			{Name: "dry-run", Type: "bool", Default: false, Description: "Show the change without writing"},
		},
		Result:   objectSchema("moved", "int", "reordered", "bool", "removed", "int", "committed", "bool"),
		Errors:   defaultErrors(),
		Examples: []string{"sticker favorites organize --collection work --ids 0123456789abcdef0123456789abcdef --move-to team --dry-run"},
		Args:     cobra.NoArgs,
	}
}

func favoriteDescribeMetadata() commandMetadata {
	return commandMetadata{
		Path:        "favorites describe",
		Use:         "describe ID",
		Summary:     "Update one personal favorite caption",
		Description: "Replace the personal caption for an item. The --caption flag is required, including when intentionally setting an empty caption.",
		Effect:      effectWrite,
		Parameters:  []parameterMetadata{{Name: "id", Type: "string", Source: "argument", Description: "Favorite item ID", Required: true}},
		Flags:       []flagMetadata{{Name: "caption", Type: "string", Description: "New personal caption", Required: true}, {Name: "dry-run", Type: "bool", Default: false, Description: "Validate without writing"}},
		Result:      objectSchema("item", "object", "updated", "bool", "dry_run", "bool"),
		Errors:      defaultErrors(),
		Examples:    []string{"sticker favorites describe 0123456789abcdef0123456789abcdef --caption '收到'"},
		Args:        cobra.ExactArgs(1),
	}
}

func favoriteRemoveMetadata() commandMetadata {
	return commandMetadata{
		Path:        "favorites remove",
		Use:         "remove ID...",
		Summary:     "Remove personal favorite relationships",
		Description: "Remove one or more personal favorite relationships while retaining originals still referenced by installed packs.",
		Effect:      effectWrite,
		Parameters:  []parameterMetadata{{Name: "ids", Type: "string[]", Source: "argument", Description: "One or more favorite item IDs", Required: true}},
		Flags:       []flagMetadata{{Name: "dry-run", Type: "bool", Default: false, Description: "Show the change without writing"}},
		Result:      objectSchema("removed", "int", "retained_original", "int", "committed", "bool", "dry_run", "bool"),
		Errors:      defaultErrors(),
		Examples:    []string{"sticker favorites remove 0123456789abcdef0123456789abcdef --dry-run"},
		Args:        cobra.MinimumNArgs(1),
	}
}

func favoriteImportMetadata() commandMetadata {
	return commandMetadata{
		Path:        "favorites import",
		Use:         "import DIR",
		Summary:     "Import a standard v1 image manifest",
		Description: "Read DIR/manifest.json and its referenced emoticons into the personal standard library. A packs.json file is not required.",
		Effect:      effectWrite,
		Parameters:  []parameterMetadata{{Name: "dir", Type: "string", Source: "argument", Description: "Source v1 material directory", Required: true}},
		Flags:       []flagMetadata{{Name: "overwrite-captions", Type: "bool", Default: false, Description: "Replace existing personal captions"}, {Name: "dry-run", Type: "bool", Default: false, Description: "Validate without writing"}},
		Result:      objectSchema("added", "int", "skipped", "int", "updated", "int", "conflicts", "int", "failed", "int", "committed", "bool", "dry_run", "bool"),
		Errors:      defaultErrors(),
		Examples:    []string{"sticker favorites import ./my-pack", "sticker favorites import ./my-pack --dry-run"},
		Args:        cobra.ExactArgs(1),
	}
}

func favoriteExportMetadata() commandMetadata {
	return commandMetadata{
		Path:        "favorites export",
		Use:         "export DIR",
		Summary:     "Export personal favorites as a standard image pack",
		Description: "Write original files, a compatible v1 manifest, and pack metadata to a new directory without uploading or overwriting an existing directory.",
		Effect:      effectWrite,
		Parameters:  []parameterMetadata{{Name: "dir", Type: "string", Source: "argument", Description: "New export directory", Required: true}},
		Flags:       []flagMetadata{{Name: "dry-run", Type: "bool", Default: false, Description: "Show the export plan without writing"}},
		Result:      objectSchema("path", "string", "count", "int", "size", "int"),
		Errors:      defaultErrors(),
		Examples:    []string{"sticker favorites export ./shared-pack", "sticker favorites export ./shared-pack --dry-run"},
		Args:        cobra.ExactArgs(1),
	}
}

func boolDefault(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		parsed, _ := strconv.ParseBool(typed)
		return parsed
	default:
		return false
	}
}

func intDefault(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case string:
		parsed, _ := strconv.Atoi(typed)
		return parsed
	default:
		return 0
	}
}

func stringDefault(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func stringSliceDefault(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case string:
		if typed == "" {
			return nil
		}
		return strings.Split(typed, ",")
	default:
		return nil
	}
}
