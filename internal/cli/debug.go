package cli

import (
	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/cli/common"
)

// newDebugCmd creates the debug command
func newDebugCmd() *cobra.Command {
	var (
		limit      int
		showRemote bool
	)

	cmd := &cobra.Command{
		Use:   "debug",
		Short: "Dump debugging information about recent commands and stack state",
		Long: `Dump comprehensive debugging information including recent command history
and complete stack state. This is useful for diagnosing issues when stacks
get into a bad state.

The output includes:
  - Recent commands and their parameters (from undo snapshots)
  - Complete stack state (branches, relationships, metadata, PR info)
  - Continuation state (if exists)
  - Repository information
  - Remote metadata state (with --remote flag)

Output is formatted as pretty-printed JSON for easy reading and parsing.`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				// Run debug action
				return actions.DebugAction(ctx, actions.DebugOptions{
					Limit:      limit,
					ShowRemote: showRemote,
				})
			})
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 0, "Limit the number of recent commands to show (0 = all)")
	cmd.Flags().BoolVar(&showRemote, "remote", false, "Fetch and show remote metadata state")

	return cmd
}
