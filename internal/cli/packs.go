package cli

import (
	"context"
	"errors"
	"os"

	"github.com/9Ashwin/sticker-cli/internal/packs"
	"github.com/spf13/cobra"
)

func runPackInstall(cmd *cobra.Command, options *rootOptions, args []string) error {
	source, err := cmd.Flags().GetString("source")
	if err != nil {
		return err
	}
	source = resolvePackSource(source)
	dryRun, err := cmd.Flags().GetBool("dry-run")
	if err != nil {
		return err
	}
	home, err := resolveHome(options)
	if err != nil {
		return err
	}
	data, err := executePackInstall(cmd.Context(), home, source, args[0], dryRun)
	if err != nil {
		return normalizePackError(err)
	}
	return writeResult(cmd, options, data, false)
}

func runSetup(cmd *cobra.Command, options *rootOptions) error {
	packID, err := cmd.Flags().GetString("pack")
	if err != nil {
		return err
	}
	source, err := cmd.Flags().GetString("source")
	if err != nil {
		return err
	}
	source = resolvePackSource(source)
	dryRun, err := cmd.Flags().GetBool("dry-run")
	if err != nil {
		return err
	}
	home, err := resolveHome(options)
	if err != nil {
		return err
	}
	data, err := executePackInstall(cmd.Context(), home, source, packID, dryRun)
	if err != nil {
		return normalizePackError(err)
	}
	data["setup"] = true
	return writeResult(cmd, options, data, false)
}

func executePackInstall(ctx context.Context, home, source, packID string, dryRun bool) (map[string]any, error) {
	if dryRun {
		plan, err := packs.Plan(ctx, packs.PlanOptions{
			Home:   home,
			Source: source,
			PackID: packID,
		})
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"source":         plan.Source,
			"target":         plan.Target,
			"pack":           plan.Pack,
			"revision":       plan.Revision,
			"added":          plan.Added,
			"reused":         plan.Reused,
			"download_bytes": plan.DownloadBytes,
			"dry_run":        true,
		}, nil
	}
	result, err := packs.Install(ctx, packs.InstallOptions{
		Home:   home,
		Source: source,
		PackID: packID,
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"source":         result.Source,
		"target":         result.Target,
		"pack":           result.Pack,
		"revision":       result.Revision,
		"added":          result.Added,
		"reused":         result.Reused,
		"download_bytes": result.DownloadBytes,
		"dry_run":        false,
	}, nil
}

func runPackUpdate(cmd *cobra.Command, options *rootOptions, args []string) error {
	dryRun, err := cmd.Flags().GetBool("dry-run")
	if err != nil {
		return err
	}
	home, err := resolveHome(options)
	if err != nil {
		return err
	}
	if dryRun {
		plan, err := packs.PlanUpdate(cmd.Context(), packs.UpdateOptions{Home: home, PackID: args[0]})
		if err != nil {
			return normalizePackError(err)
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
	result, err := packs.Update(cmd.Context(), packs.UpdateOptions{Home: home, PackID: args[0]})
	if err != nil {
		return normalizePackError(err)
	}
	return writeResult(cmd, options, map[string]any{
		"source":         result.Source,
		"target":         result.Target,
		"pack":           result.Pack,
		"revision":       result.Revision,
		"added":          result.Added,
		"reused":         result.Reused,
		"download_bytes": result.DownloadBytes,
		"dry_run":        false,
	}, false)
}

func runPackRemove(cmd *cobra.Command, options *rootOptions, args []string) error {
	dryRun, err := cmd.Flags().GetBool("dry-run")
	if err != nil {
		return err
	}
	home, err := resolveHome(options)
	if err != nil {
		return err
	}
	result, err := packs.Remove(cmd.Context(), packs.RemoveOptions{
		Home:   home,
		PackID: args[0],
		DryRun: dryRun,
	})
	if err != nil {
		return normalizePackError(err)
	}
	return writeResult(cmd, options, result, false)
}

func runPackRepair(cmd *cobra.Command, options *rootOptions, args []string) error {
	dryRun, err := cmd.Flags().GetBool("dry-run")
	if err != nil {
		return err
	}
	home, err := resolveHome(options)
	if err != nil {
		return err
	}
	result, err := packs.Repair(cmd.Context(), packs.RepairOptions{
		Home:   home,
		PackID: args[0],
		DryRun: dryRun,
	})
	if err != nil {
		return normalizePackError(err)
	}
	return writeResult(cmd, options, result, false)
}

func runPackList(cmd *cobra.Command, options *rootOptions) error {
	source, err := cmd.Flags().GetString("source")
	if err != nil {
		return err
	}
	source = resolvePackSource(source)
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

func resolvePackSource(source string) string {
	if source != "" {
		return source
	}
	return os.Getenv("STICKER_PACK_SOURCE")
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
