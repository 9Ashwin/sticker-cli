package cli

import (
	"errors"

	"github.com/9Ashwin/sticker-cli/internal/packs"
	"github.com/spf13/cobra"
)

func runPackInstall(cmd *cobra.Command, options *rootOptions, args []string) error {
	source, err := cmd.Flags().GetString("source")
	if err != nil {
		return err
	}
	dryRun, err := cmd.Flags().GetBool("dry-run")
	if err != nil {
		return err
	}
	home, err := resolveHome(options)
	if err != nil {
		return err
	}
	plan, err := packs.Plan(cmd.Context(), packs.PlanOptions{
		Home:   home,
		Source: source,
		PackID: args[0],
	})
	if err != nil {
		return normalizePackError(err)
	}
	if !dryRun {
		return placeholder(cmd, "packs install (image download and commit)")
	}
	return writeResult(cmd, options, map[string]any{
		"source":         plan.Source,
		"target":         plan.Target,
		"pack":           plan.Pack,
		"revision":       plan.Revision,
		"added":          plan.Added,
		"reused":         plan.Reused,
		"download_bytes": plan.DownloadBytes,
		"dry_run":        true,
	}, false)
}

func runPackList(cmd *cobra.Command, options *rootOptions) error {
	source, err := cmd.Flags().GetString("source")
	if err != nil {
		return err
	}
	offline, err := cmd.Flags().GetBool("offline")
	if err != nil {
		return err
	}
	result, err := packs.Discover(cmd.Context(), packs.Options{
		Home:    options.home,
		Source:  source,
		Offline: offline,
	})
	if err != nil {
		return normalizePackError(err)
	}
	data := map[string]any{
		"source":     result.Source,
		"items":      result.Items,
		"fetched_at": result.FetchedAtString(),
		"stale":      result.Stale,
	}
	return writeResult(cmd, options, data, false)
}

func normalizePackError(err error) error {
	var sourceErr *packs.Error
	if !errors.As(err, &sourceErr) {
		return err
	}
	return &cliError{
		Type:      sourceErr.Kind,
		Subtype:   sourceErr.Subtype,
		Message:   sourceErr.Message,
		Hint:      sourceErr.Hint,
		Retryable: sourceErr.Retryable,
		ExitCode:  packExitCode(sourceErr.Kind),
	}
}

func packExitCode(kind string) int {
	switch kind {
	case "validation":
		return 2
	case "not_found":
		return 3
	case "network":
		return 4
	case "integrity":
		return 5
	case "conflict":
		return 6
	case "io":
		return 7
	case "cancelled":
		return 130
	default:
		return 1
	}
}
