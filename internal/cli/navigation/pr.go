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
		Use:   "pr [branch|PR]",
		Short: "Open a pull request in your default browser",
		Long: `Open the pull request for a branch, a PR number, or the current branch in
your default browser.

With no argument, opens the current branch's pull request. Pass a branch name
or a PR number to open a specific one. Pass --stack to open the root pull
request of the stack instead of the target branch's own PR.

Examples:
  stackit pr                  # open the current branch's pull request
  stackit pr feature-x        # open feature-x's pull request
  stackit pr 1234             # open PR #1234
  stackit pr --stack          # open the root pull request of the current stack
  stackit pr feature-x --stack   # open the root pull request of feature-x's stack`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			branchOrPR := ""
			if len(args) > 0 {
				branchOrPR = args[0]
			}
			return common.Run(cmd, func(ctx *app.Context) error {
				_, err := actions.PrAction(ctx, actions.PrOptions{
					BranchOrPR: branchOrPR,
					Stack:      stack,
				})
				return err
			})
		},
	}

	cmd.Flags().BoolVar(&stack, "stack", false, "Open the root pull request of the stack instead of the target branch's own PR")

	return cmd
}
