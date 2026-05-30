package cli

import (
	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/cli/common"
)

// newStateCmd creates the state command.
func newStateCmd() *cobra.Command {
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "state",
		Short: "Show a snapshot of the stack, working tree, and any in-progress operation",
		Long: `Show a complete snapshot of the current stack in a single call.

Combines the current branch and trunk, working-tree state (staged/unstaged/
untracked), any in-progress rebase/merge with its conflicted files, and the full
stack (structure, PR/CI status, and per-branch needs_restack/locked/frozen/scope).

Use --json for one machine-readable snapshot — agents and scripts read everything
from a single call instead of combining git status, stackit log, and stackit info.

Examples:
  stackit state            # Human-readable summary
  stackit state --json     # Complete machine-readable snapshot`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				return actions.StateAction(ctx, actions.StateOptions{JSON: jsonOut})
			})
		},
	}

	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output a complete machine-readable snapshot as JSON")

	return cmd
}
