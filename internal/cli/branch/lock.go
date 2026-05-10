package branch

import (
	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/actions/lock"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/cli/common"
	"github.com/getstackit/stackit/internal/errors"
	"github.com/getstackit/stackit/internal/utils"
)

// NewLockCmd creates the lock command
func NewLockCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lock [branch]",
		Short: "Lock specified branch and its downstack",
		Long: `Lock specified branch and branches downstack of it.

Locking a branch prevents local modifications and restacking for everyone 
collaborating on the stack. The locked status is stored in remote metadata 
and shared with others when they 'st get' or 'st sync' the stack.

Use 'st lock' to signal to your team that certain branches are stable and 
should not be modified. For local-only protection (e.g. when building on 
top of a colleague's PR), use 'st freeze' instead.

This operation can be undone with 'st unlock'.`,
		SilenceUsage: true,
		Args:         cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				branchName, err := common.ResolveBranchArg(ctx, args, errors.ErrNotOnBranchNoBranchSpecified)
				if err != nil {
					return err
				}

				handler := NewLockUI(ctx.Output, utils.IsInteractive())
				return lock.Action(ctx, branchName, handler)
			})
		},
	}

	return cmd
}

// NewUnlockCmd creates the unlock command
func NewUnlockCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unlock [branch]",
		Short: "Unlock specified branch and its upstack",
		Long: `Unlock specified branch and branches upstack of it.

Unlocking a branch re-enables local modifications and restacking for everyone 
by clearing the shared lock in remote metadata. 

If the branch is also frozen locally, you will still need to run 'st unfreeze' 
to enable modifications.`,
		SilenceUsage: true,
		Args:         cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				branchName, err := common.ResolveBranchArg(ctx, args, errors.ErrNotOnBranchNoBranchSpecified)
				if err != nil {
					return err
				}

				handler := NewLockUI(ctx.Output, utils.IsInteractive())
				return lock.Unlock(ctx, branchName, handler)
			})
		},
	}
}
