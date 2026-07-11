package navigation

import (
	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/cli/common"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/errors"
	"github.com/getstackit/stackit/internal/tui/style"
)

// NewChildrenCmd creates the children command
func NewChildrenCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "children",
		Short: "Show the children of the current branch",
		Long: `Show the children of the current branch.

Lists all branches that have the current branch as their parent in the stack.
This is useful for understanding the structure of your stack and seeing which
branches depend on the current branch.`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				// Get current branch
				currentBranch := ctx.Engine.CurrentBranch()
				if currentBranch == nil {
					return errors.ErrNotOnBranch
				}

				// Get children
				graph := ctx.Engine.Graph(engine.SortStrategyAlphabetical)
				children := graph.ChildBranches(*currentBranch)
				if len(children) == 0 {
					ctx.Output.Info("%s has no children.", style.ColorCurrentBranch(currentBranch.GetName()))
					return nil
				}

				// Print children
				for _, child := range children {
					ctx.Output.Info("%s", child.GetName())
				}
				return nil
			})
		},
	}

	return cmd
}
