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

func runFavoriteList(ctx context.Context, cmd *cobra.Command, options *rootOptions) error {
	home, err := resolveHome(options)
	if err != nil {
		return err
	}
	collection, err := cmd.Flags().GetString("collection")
	if err != nil {
		return err
	}
	sortOrder, err := cmd.Flags().GetString("sort")
	if err != nil {
		return err
	}
	limit, err := cmd.Flags().GetInt("limit")
	if err != nil {
		return err
	}
	offset, err := cmd.Flags().GetInt("offset")
	if err != nil {
		return err
	}
	result, err := stickerfavorites.List(ctx, stickerfavorites.ListOptions{
		Home:       home,
		Collection: collection,
		Sort:       sortOrder,
		Limit:      limit,
		Offset:     offset,
	})
	if err != nil {
		return err
	}
	return writeSearchResult(cmd, options, result)
}

func runFavoriteDescribe(ctx context.Context, cmd *cobra.Command, options *rootOptions, id string) error {
	home, err := resolveHome(options)
	if err != nil {
		return err
	}
	caption, err := cmd.Flags().GetString("caption")
	if err != nil {
		return err
	}
	dryRun, err := cmd.Flags().GetBool("dry-run")
	if err != nil {
		return err
	}
	result, err := stickerfavorites.Describe(ctx, stickerfavorites.DescribeOptions{
		Home:    home,
		ID:      id,
		Caption: caption,
		DryRun:  dryRun,
	})
	if err != nil {
		return err
	}
	return writeResult(cmd, options, result, false)
}

func runFavoriteRemove(ctx context.Context, cmd *cobra.Command, options *rootOptions, ids []string) error {
	home, err := resolveHome(options)
	if err != nil {
		return err
	}
	dryRun, err := cmd.Flags().GetBool("dry-run")
	if err != nil {
		return err
	}
	result, err := stickerfavorites.Remove(ctx, stickerfavorites.RemoveOptions{
		Home:   home,
		IDs:    ids,
		DryRun: dryRun,
	})
	if err != nil {
		return err
	}
	return writeResult(cmd, options, result, false)
}

func runFavoriteImport(ctx context.Context, cmd *cobra.Command, options *rootOptions, args []string) error {
	home, err := resolveHome(options)
	if err != nil {
		return err
	}
	overwriteCaptions, err := cmd.Flags().GetBool("overwrite-captions")
	if err != nil {
		return err
	}
	dryRun, err := cmd.Flags().GetBool("dry-run")
	if err != nil {
		return err
	}
	result, err := stickerfavorites.Import(ctx, stickerfavorites.ImportOptions{
		Home:              home,
		Source:            args[0],
		OverwriteCaptions: overwriteCaptions,
		DryRun:            dryRun,
	})
	if err != nil {
		return err
	}
	return writeResult(cmd, options, result, false)
}

func runFavoriteExport(ctx context.Context, cmd *cobra.Command, options *rootOptions, args []string) error {
	home, err := resolveHome(options)
	if err != nil {
		return err
	}
	dryRun, err := cmd.Flags().GetBool("dry-run")
	if err != nil {
		return err
	}
	result, err := stickerfavorites.Export(ctx, stickerfavorites.ExportOptions{
		Home:        home,
		Destination: args[0],
		DryRun:      dryRun,
	})
	if err != nil {
		return err
	}
	return writeResult(cmd, options, result, false)
}
