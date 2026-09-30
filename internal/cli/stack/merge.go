// Package stack provides CLI commands for operating on entire stacks.
package stack

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/actions"
	mergeAction "github.com/getstackit/stackit/internal/actions/merge"
	"github.com/getstackit/stackit/internal/actions/sync"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/cli/common"
	mergeCmd "github.com/getstackit/stackit/internal/cli/stack/merge"
	"github.com/getstackit/stackit/internal/tui"
	"github.com/getstackit/stackit/internal/tui/style"
)

// NewMergeCmd creates the merge command
func NewMergeCmd() *cobra.Command {
	return mergeCmd.NewMergeCmd(handlePostMergeAction)
}

// handlePostMergeAction handles post-merge follow-up actions
func handlePostMergeAction(ctx *app.Context, action mergeAction.PostMergeAction) error {
	out := ctx.Output

	switch action {
	case mergeAction.PostMergeSyncTrunk:
		// The sync UI starts only once trunk is checked out. runner.Cleanup is
		// nil-safe, so the deferred call is fine when it never started.
		var runner *tui.Runner
		defer func() { runner.Cleanup() }()

		// PostMergeSync never enters the conflict resolution workflow:
		// EnterConflictWorkflow detaches HEAD, which violates the safety
		// invariant that merge next must never leave the user in detached HEAD
		// state. Conflicts are reported in the sync summary with instructions
		// to run 'stackit restack' manually.
		_, err := mergeAction.PostMergeSync(ctx, mergeAction.PostMergeSyncOptions{
			OnTrunkCheckedOut: func(result actions.CheckoutResult) {
				if result.WorktreeSwitchPath != "" {
					common.HandleCheckoutResult(ctx.Output, result)
				}
			},
		}, func() sync.Handler {
			var handler sync.Handler
			runner, handler = NewSyncUI(ctx.Output, ctx.Logger)
			return handler
		})

		var checkoutErr *mergeAction.PostMergeCheckoutError
		if errors.As(err, &checkoutErr) {
			out.Newline()
			out.Error("%v", checkoutErr.Err)
			out.Newline()
			out.Info("%s", style.ColorYellow("To fix and continue:"))
			out.Info("  (1) Handle your local changes (e.g., %s or %s)", style.ColorCyan("git stash"), style.ColorCyan("git commit"))
			out.Info("  (2) Switch to trunk: %s", style.ColorCyan("stackit checkout --trunk"))
			out.Info("  (3) Sync your workspace: %s", style.ColorCyan("stackit sync --restack"))
			return nil
		}
		return err

	case mergeAction.PostMergeDone:
		return nil
	}

	return nil
}
