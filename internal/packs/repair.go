package packs

import "context"

// RepairOptions controls recovery of one invalid installed pack state.
type RepairOptions struct {
	Home   string
	PackID string
	DryRun bool
}

// RepairResult describes whether an invalid pack state was cleared. Original
// image files are retained and can be reused by a later installation.
type RepairResult struct {
	Repaired      bool  `json:"repaired"`
	RetainedBytes int64 `json:"retained_bytes"`
	Committed     bool  `json:"committed"`
	DryRun        bool  `json:"dry_run,omitempty"`
}

// Repair clears only a corrupt installed state. A valid state and an absent
// state are left untouched, making the command safe to run before reinstall.
func Repair(ctx context.Context, options RepairOptions) (RepairResult, error) {
	result, err := cleanupPackState(ctx, RemoveOptions(options), true)
	if err != nil {
		return RepairResult{}, err
	}
	return RepairResult{
		Repaired:      result.Removed && result.StateCorrupt,
		RetainedBytes: result.RetainedBytes,
		Committed:     result.Committed,
		DryRun:        result.DryRun,
	}, nil
}
