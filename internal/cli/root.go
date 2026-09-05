// Package cli exposes the local image library command line.
package cli

import (
	"context"
	"encoding/json"
	"io"

	"github.com/spf13/cobra"
)

// Run executes one invocation without changing process-wide streams or state.
func Run(ctx context.Context, args []string, out, errOut io.Writer, version, commit string) int {
	root := &cobra.Command{
		Use:           "emoticon",
		Short:         "Install, search and collect local reaction images",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.SetArgs(args)
	root.SetOut(out)
	root.SetErr(errOut)
	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the CLI build version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{
				"ok":   true,
				"data": map[string]string{"version": version, "commit": commit},
				"meta": map[string]int{"schema_version": 1},
			})
		},
	})
	if err := root.ExecuteContext(ctx); err != nil {
		_ = json.NewEncoder(errOut).Encode(map[string]any{
			"ok":    false,
			"error": map[string]string{"type": "validation", "subtype": "invalid_argument", "message": err.Error()},
		})
		return 2
	}
	return 0
}
