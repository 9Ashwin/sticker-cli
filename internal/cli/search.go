package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	stickersearch "github.com/9Ashwin/sticker-cli/internal/search"
	"github.com/spf13/cobra"
)

func runSearch(ctx context.Context, cmd *cobra.Command, options *rootOptions, args []string) error {
	home, err := resolveHome(options)
	if err != nil {
		return err
	}
	pack, _ := cmd.Flags().GetString("pack")
	favorites, _ := cmd.Flags().GetBool("favorites")
	limit, _ := cmd.Flags().GetInt("limit")
	offset, _ := cmd.Flags().GetInt("offset")
	result, err := stickersearch.Execute(ctx, stickersearch.Options{
		Home:      home,
		Query:     args[0],
		Pack:      pack,
		Favorites: favorites,
		Limit:     limit,
		Offset:    offset,
	})
	if err != nil {
		return err
	}
	return writeSearchResult(cmd, options, result)
}

func resolveHome(options *rootOptions) (string, error) {
	root := options.home
	if root == "" {
		root = os.Getenv("STICKER_HOME")
	}
	if root == "" {
		configDir, err := os.UserConfigDir()
		if err != nil {
			return "", &cliError{Type: "io", Subtype: "read_failed", Message: "failed to determine the user config directory", Hint: "Set --home or STICKER_HOME to a local data directory.", ExitCode: 7}
		}
		root = filepath.Join(configDir, "sticker")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", validationError("unsafe_path", "failed to resolve the local data directory", "Set --home to a valid local directory.")
	}
	return filepath.Clean(absolute), nil
}

func writeSearchResult(cmd *cobra.Command, options *rootOptions, result stickersearch.Result) error {
	for {
		if searchResultFits(result) {
			return writeResult(cmd, options, result, false)
		}
		if len(result.Items) == 0 {
			return validationError("output_limit", "a single search result exceeds 256 KiB", "Use a shorter query or caption, or retrieve one item at a time.")
		}
		result.Items = result.Items[:len(result.Items)-1]
		result.NextOffset -= 1
		result.HasMore = result.NextOffset < result.Total
	}
}

func searchResultFits(result stickersearch.Result) bool {
	encoded, err := json.Marshal(successEnvelope{OK: true, Data: result, Meta: map[string]int{"schema_version": 1}})
	return err == nil && len(encoded)+1 <= maxOutputBytes
}
