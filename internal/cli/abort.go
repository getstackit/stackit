package cli

import (
	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/actions/abort"
	"github.com/getstackit/stackit/internal/actions/absorb"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/cli/common"
	"github.com/getstackit/stackit/internal/cli/stack"
	"github.com/getstackit/stackit/internal/config"
	"github.com/getstackit/stackit/internal/utils"
)

// newAbortCmd creates the abort command
func newAbortCmd() *cobra.Command {
	var (
		force bool
	)

	cmd := &cobra.Command{
		Use:   "abort",
		Short: "Abort the current stackit command halted by a conflict",
		Long: `Aborts the current stackit command halted by a conflict.

This command cancels any in-progress operation (such as restack, sync, merge,
or absorb) that has been paused due to a conflict. Branches and metadata roll
back to the snapshot that halted command took, and uncommitted changes the
command consumed (such as edits 'modify' amended or files staged before
'create') return to your working tree.

Abort only rolls back the halted command. If that command recorded no snapshot,
it leaves your branches as they are; use 'stackit undo' to pick a state instead.

Examples:
  stackit abort       # Abort after confirming
  stackit abort -f    # Abort without prompting`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				// A pending conflict rollback owns abort even after Git's rebase
				// has been unwound; otherwise detached HEAD on a retry is
				// mistaken for a failed absorb. A stale continuation, from a
				// conflict finished outside stackit, must not block absorb's
				// own cleanup.
				continuation, continuationErr := config.GetContinuationState(ctx.RepoRoot)
				ownedByContinuation := continuationErr == nil && abort.ContinuationOwnsAbort(ctx, continuation)
				if !ownedByContinuation && absorb.IsAbsorbInProgress(ctx) {
					return absorb.Abort(ctx)
				}

				// Otherwise use the standard abort action
				handler := stack.NewAbortUI(ctx.Output, utils.IsInteractive())
				return abort.Action(ctx, abort.Options{
					Force: force,
				}, handler)
			})
		},
	}

	// Add flags
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Do not prompt for confirmation; abort immediately.")

	return cmd
}
