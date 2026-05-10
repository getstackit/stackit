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

// NewTopCmd creates the top command
func NewTopCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "top",
		Short: "Switch to the tip branch of the current stack",
		Long: `Switch to the tip branch of the current stack. Prompts if ambiguous.

This command navigates up the children chain from the current branch until
it reaches a branch with no children (the tip of the stack). If multiple
children exist at any level, you will be prompted to select which branch
to follow.`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				handler := stack.NewNavigationUI(ctx.Output, utils.IsInteractive())
				result, err := navigation.SwitchBranchAction(navigation.DirectionTop, ctx, handler)
				if err != nil {
					return err
				}
				return common.CompleteCheckout(ctx, result, actions.CheckoutOptions{BranchName: result.TargetBranch}, nil)
			})
		},
	}

	return cmd
}
