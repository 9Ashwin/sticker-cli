package cli

import (
	"context"

	"github.com/9Ashwin/sticker-cli/internal/library"
	"github.com/9Ashwin/sticker-cli/internal/preview"
	stickersearch "github.com/9Ashwin/sticker-cli/internal/search"
	"github.com/spf13/cobra"
)

func runGet(ctx context.Context, cmd *cobra.Command, options *rootOptions, id string) error {
	home, err := resolveHome(options)
	if err != nil {
		return err
	}
	found, err := stickersearch.Find(ctx, home, id)
	if err != nil {
		return err
	}
	item := library.Item{
		MD5:      found.MD5,
		SHA256:   found.SHA256,
		Filename: found.Filename,
		Format:   found.Format,
		Size:     found.Size,
		Caption:  found.Caption,
	}
	libraryRoot, err := library.New(home)
	if err != nil {
		return err
	}
	file, path, err := libraryRoot.OpenVerified(ctx, item)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	found.Path = path
	if previewRequested, _ := cmd.Flags().GetBool("preview"); previewRequested {
		previewPath, err := preview.Generate(ctx, home, item, file)
		if err != nil {
			return err
		}
		found.PreviewPath = previewPath
	}
	return writeResult(cmd, options, map[string]any{"item": found}, false)
}
