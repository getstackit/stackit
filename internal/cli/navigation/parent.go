package navigation

import (
	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/cli/common"
	"github.com/getstackit/stackit/internal/errors"
	"github.com/getstackit/stackit/internal/tui/style"
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
			return common.RunReadOnlyCurrentBranch(cmd, func(ctx *app.Context) error {
				// Get current branch
				currentBranch := ctx.Engine.CurrentBranch()
				if currentBranch == nil {
					return errors.ErrNotOnBranch
				}

				// Check if on trunk
				if currentBranch.IsTrunk() {
					ctx.Output.Info("%s is trunk and has no parent.", style.ColorBranchName(currentBranch.GetName(), true))
					return nil
				}

				// Get parent
				parent := currentBranch.GetParent()
				if parent == nil {
					ctx.Output.Info("%s has no parent (untracked branch).", style.ColorBranchName(currentBranch.GetName(), true))
					return nil
				}

				// Print parent
				ctx.Output.Info("%s", parent.GetName())
				return nil
			})
		},
	}

	return cmd
}
