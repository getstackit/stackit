package navigation

import (
	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/cli/common"
)

// NewPrCmd creates the pr command.
func NewPrCmd() *cobra.Command {
	var stack bool

	cmd := &cobra.Command{
		Use:   "pr [branch|number]",
		Short: "Open a pull request in the default browser",
		Long: `Open the pull request for a branch in the default browser.

With no argument, opens the current branch's pull request. Accepts either a
branch name or a pull-request number. Use --stack to open the root pull
request of the stack instead of the target branch's own pull request.

Examples:
  stackit pr                  # Open the current branch's PR
  stackit pr feature-x        # Open feature-x's PR
  stackit pr 1234             # Open PR #1234
  stackit pr --stack          # Open the root PR of the current stack`,
		SilenceUsage: true,
		Args:         cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return common.RunReadOnlyCurrentBranch(cmd, func(ctx *app.Context) error {
				target := ""
				if len(args) > 0 {
					target = args[0]
				}
				return actions.OpenPRAction(ctx, actions.OpenPROptions{
					Target: target,
					Stack:  stack,
				})
			})
		},
	}

	cmd.Flags().BoolVar(&stack, "stack", false, "Open the root pull request of the current stack")

	return cmd
}
