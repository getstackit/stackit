package navigation

import (
	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/actions/navigation"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/cli/common"
	"github.com/getstackit/stackit/internal/cli/stack"
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
			return common.Run(cmd, func(ctx *app.Context) error {
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
