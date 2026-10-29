// Package branch provides CLI commands for managing branches in a stack.
package branch

import (
	"github.com/getstackit/stackit/internal/actions/pop"
	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/cli/common"
)

// NewPopCmd creates the pop command
func NewPopCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pop",
		Short: "Delete the current branch but retain the state of files in the working tree",
		Long: `Delete the current branch but retain the state of files in the working tree.

This is useful when you want to remove a branch from the stack but keep
your uncommitted changes. The working tree will remain unchanged after
the branch is deleted.`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				// Run pop action
				return pop.Action(ctx.Context, pop.Deps{Engine: ctx.Engine, Out: ctx.Output})
			})
		},
	}

	return cmd
}
