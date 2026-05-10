// Package stack provides CLI commands for operating on entire stacks.
package stack

import (
	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/actions"
	mergeAction "github.com/getstackit/stackit/internal/actions/merge"
	"github.com/getstackit/stackit/internal/actions/sync"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/cli/common"
	mergeCmd "github.com/getstackit/stackit/internal/cli/stack/merge"
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
		result, err := actions.CheckoutAction(ctx, actions.CheckoutOptions{
			CheckoutTrunk: true,
		}, nil)
		if err != nil {
			out.Newline()
			out.Error("%v", err)
			out.Newline()
			out.Info("%s", style.ColorYellow("To fix and continue:"))
			out.Info("  (1) Handle your local changes (e.g., %s or %s)", style.ColorCyan("git stash"), style.ColorCyan("git commit"))
			out.Info("  (2) Switch to trunk: %s", style.ColorCyan("stackit checkout --trunk"))
			out.Info("  (3) Sync your workspace: %s", style.ColorCyan("stackit sync --restack"))
			return nil
		}

		if result.WorktreeSwitchPath != "" {
			common.HandleCheckoutResult(ctx.Output, result)
		}

		// runner.Cleanup is nil-safe so no extra guard is needed.
		runner, handler := NewSyncUI(ctx.Output, ctx.Logger)
		defer runner.Cleanup()

		return sync.Action(ctx, sync.Options{
			Restack: true,
		}, handler)

	case mergeAction.PostMergeDone:
		return nil
	}

	return nil
}
