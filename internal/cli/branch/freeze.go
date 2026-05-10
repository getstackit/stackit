package branch

import (
	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/cli/common"
	"github.com/getstackit/stackit/internal/errors"
)

// NewFreezeCmd creates the freeze command
func NewFreezeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "freeze [branch]",
		Short: "Freeze specified branch and its downstack locally",
		Long: `Freeze specified branch and branches downstack of it locally.

Freezing a branch prevents local modifications (like modify, squash, absorb) and 
restacking. Unlike 'st lock', freezing is strictly local to your machine and is 
not shared with collaborators.

Use 'st freeze' when you want to stack on top of someone else’s PRs without 
accidentally modifying them or affecting their metadata. Frozen branches are 
automatically updated from remote via 'st sync' or 'st get' using hard-resets.

This operation can be undone with 'st unfreeze'.`,
		SilenceUsage: true,
		Args:         cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				branchName, err := common.ResolveBranchArg(ctx, args, errors.ErrNotOnBranchNoBranchSpecified)
				if err != nil {
					return err
				}

				return actions.FreezeAction(ctx, branchName)
			})
		},
	}
}
