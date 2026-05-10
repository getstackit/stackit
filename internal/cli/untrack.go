package cli

import (
	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/actions/untrack"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/cli/common"
	"github.com/getstackit/stackit/internal/cli/stack"
	"github.com/getstackit/stackit/internal/errors"
	"github.com/getstackit/stackit/internal/utils"
)

// newUntrackCmd creates the untrack command
func newUntrackCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "untrack [branch]",
		Short: "Stop tracking a branch with stackit",
		Long: `Stop tracking the current (or provided) branch with stackit.
If the branch has children, they will also be untracked.`,
		SilenceUsage:      true,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: common.CompleteBranches,
		RunE: func(cmd *cobra.Command, args []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				branchName, err := common.ResolveBranchArg(ctx, args, errors.ErrNotOnBranch)
				if err != nil {
					return err
				}

				// Execute untrack action
				handler := stack.NewUntrackUI(ctx.Output, utils.IsInteractive())
				return untrack.Action(ctx, untrack.Options{
					BranchName: branchName,
					Force:      force,
				}, handler)
			})
		},
	}

	// Add flags
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Will not prompt for confirmation before untracking a branch with children")

	return cmd
}
