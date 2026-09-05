package cli

import (
	"context"

	stickerfavorites "github.com/9Ashwin/sticker-cli/internal/favorites"
	"github.com/spf13/cobra"
)

func runFavoriteAdd(ctx context.Context, cmd *cobra.Command, options *rootOptions, args []string) error {
	home, err := resolveHome(options)
	if err != nil {
		return err
	}
	id, err := cmd.Flags().GetString("id")
	if err != nil {
		return err
	}
	dryRun, err := cmd.Flags().GetBool("dry-run")
	if err != nil {
		return err
	}
	var caption *string
	if cmd.Flags().Changed("caption") {
		value, err := cmd.Flags().GetString("caption")
		if err != nil {
			return err
		}
		caption = &value
	}
	path := ""
	if len(args) == 1 {
		path = args[0]
	}
	result, err := stickerfavorites.Execute(ctx, stickerfavorites.Options{
		Home:    home,
		Path:    path,
		ID:      id,
		Caption: caption,
		DryRun:  dryRun,
	})
	if err != nil {
		return err
	}
	return writeResult(cmd, options, result, false)
}
