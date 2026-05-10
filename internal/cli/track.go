package cli

import (
	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/actions/track"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/cli/common"
	"github.com/getstackit/stackit/internal/cli/stack"
	"github.com/getstackit/stackit/internal/errors"
	"github.com/getstackit/stackit/internal/utils"
)

// newTrackCmd creates the track command
func newTrackCmd() *cobra.Command {
	var (
		force  bool
		parent string
	)

	cmd := &cobra.Command{
		Use:   "track [branch]",
		Short: "Start tracking a branch with stackit by selecting its parent",
		Long: `Start tracking the current (or provided) branch with stackit by selecting its parent.
Can recursively track a stack of branches by specifying each branch's parent interactively.
This command can also be used to fix corrupted stackit metadata.`,
		ValidArgsFunction: common.CompleteBranches,
		SilenceUsage:      true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				branchName, err := common.ResolveBranchArg(ctx, args, errors.ErrNotOnBranch)
				if err != nil {
					return err
				}

				// Execute track action
				handler := stack.NewTrackUI(ctx.Output, utils.IsInteractive())
				return track.Action(ctx, track.Options{
					BranchName: branchName,
					Force:      force,
					Parent:     parent,
				}, handler)
			})
		},
	}

	// Add flags
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Sets the parent to the most recent tracked ancestor of the branch being tracked to skip prompts. Takes precedence over --parent")
	cmd.Flags().StringVarP(&parent, "parent", "p", "", "The tracked branch's parent. Must be set to a tracked branch. If provided, only one branch can be tracked at a time.")

	_ = cmd.RegisterFlagCompletionFunc("parent", common.CompleteBranches)

	return cmd
}
