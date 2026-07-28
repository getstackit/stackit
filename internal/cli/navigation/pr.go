package navigation

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/cli/common"
	"github.com/getstackit/stackit/internal/utils"
)

// NewPrCmd creates the pr command.
func NewPrCmd() *cobra.Command {
	var stack bool

	cmd := &cobra.Command{
		Use:   "pr [branch|PR]",
		Short: "Open a pull request in the default browser",
		Long: `Open the pull request for a branch, a PR number, or the current branch in
your default browser.

Examples:
  stackit pr                 # Open the current branch's pull request
  stackit pr feature-x       # Open the pull request for a specific branch
  stackit pr 123             # Open pull request #123
  stackit pr --stack         # Open the root pull request of the current stack`,
		ValidArgsFunction: common.CompleteBranches,
		SilenceUsage:      true,
		Args:              cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			branchOrPR := ""
			if len(args) > 0 {
				branchOrPR = args[0]
			}

			return common.Run(cmd, func(ctx *app.Context) error {
				result, err := actions.PROpenAction(ctx, actions.PROpenOptions{
					BranchOrPR: branchOrPR,
					Stack:      stack,
				})
				if err != nil {
					return err
				}

				ctx.Output.Info("Opening %s", result.URL)
				if err := utils.OpenBrowser(result.URL); err != nil {
					return fmt.Errorf("failed to open browser: %w", err)
				}
				return nil
			})
		},
	}

	cmd.Flags().BoolVarP(&stack, "stack", "s", false, "Open the root pull request of the current stack")

	return cmd
}
