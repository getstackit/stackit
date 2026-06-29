package navigation

import (
	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/actions/navigation"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/cli/common"
	"github.com/getstackit/stackit/internal/cli/stack"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/utils"
)

// NewBottomCmd creates the bottom command
func NewBottomCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bottom",
		Short: "Switch to the branch closest to trunk in the current stack",
		Long: `Switch to the branch closest to trunk in the current stack.

This command navigates down the parent chain from the current branch until
it reaches the first branch that has trunk as its parent (or trunk itself).`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// bottom walks the parent chain only (DirectionBottom no longer builds
			// the full stack graph). In quiet mode no branch info is printed, so the
			// lighter branches-only load with lazy per-branch promotion suffices —
			// mirroring the quiet exact-checkout path in `co`. The managed-worktree
			// check is intentionally preserved (checkout relies on it).
			opts := common.GetGlobalOptions(cmd)
			if opts.Quiet {
				loadMode := engine.LoadModeBranchesOnly
				opts.EngineLoadMode = &loadMode
			}
			return common.RunWithOptions(cmd, opts, func(ctx *app.Context) error {
				handler := stack.NewNavigationUI(ctx.Output, utils.IsInteractive())
				result, err := navigation.SwitchBranchAction(navigation.DirectionBottom, ctx, handler)
				if err != nil {
					return err
				}
				return common.CompleteCheckout(ctx, result, actions.CheckoutOptions{BranchName: result.TargetBranch}, nil)
			})
		},
	}

	return cmd
}
