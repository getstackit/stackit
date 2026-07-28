package navigation

import (
	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/cli/common"
)

// NewPRCmd creates the pr command.
func NewPRCmd() *cobra.Command {
	var stack bool

	cmd := &cobra.Command{
		Use:   "pr [branch|PR]",
		Short: "Open a pull request in the default browser",
		Long: `Open the pull request for a branch or PR number in the default browser.

With no argument, opens the current branch's pull request. Accepts either a
branch name or a pull request number.

Examples:
  stackit pr                # Open the current branch's pull request
  stackit pr feature-x      # Open the pull request for branch feature-x
  stackit pr 1234           # Open pull request #1234
  stackit pr --stack        # Open the root pull request of the current stack`,
		SilenceUsage: true,
		Args:         cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				branchOrPR := ""
				if len(args) > 0 {
					branchOrPR = args[0]
				}

				return actions.PRAction(ctx, branchOrPR, actions.PROptions{
					Stack: stack,
				})
			})
		},
	}

	cmd.Flags().BoolVar(&stack, "stack", false, "Open the root pull request of the stack instead of the resolved branch's own pull request")

	return cmd
}
