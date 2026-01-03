package navigation

import (
	"github.com/spf13/cobra"

	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/cli/common"
	"stackit.dev/stackit/internal/errors"
	"stackit.dev/stackit/internal/tui/style"
)

// NewParentCmd creates the parent command
func NewParentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "parent",
		Short: "Show the parent of the current branch",
		Long: `Show the parent of the current branch.

Displays the name of the branch that is the parent of the current branch
in the stack. This is useful for understanding the structure of your stack
and seeing which branch the current branch is based on.`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				// Get current branch
				currentBranch := ctx.Engine.CurrentBranch()
				if currentBranch == nil {
					return errors.ErrNotOnBranch
				}

				// Check if on trunk
				if currentBranch.IsTrunk() {
					ctx.Splog.Info("%s is trunk and has no parent.", style.ColorBranchName(currentBranch.GetName(), true))
					return nil
				}

				// Get parent
				parent := currentBranch.GetParent()
				if parent == nil {
					ctx.Splog.Info("%s has no parent (untracked branch).", style.ColorBranchName(currentBranch.GetName(), true))
					return nil
				}

				// Print parent
				ctx.Splog.Info("%s", parent.GetName())
				return nil
			})
		},
	}

	return cmd
}
