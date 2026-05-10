// Package stack provides CLI commands for operating on entire stacks.
package stack

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/actions/move"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/cli/common"
)

// NewMoveCmd creates the move command
func NewMoveCmd() *cobra.Command {
	var (
		all    bool
		dryRun bool
		onto   string
		rename bool
		source string
		yes    bool
	)

	cmd := &cobra.Command{
		Use:   "move",
		Short: "Move this branch to a different parent in the stack",
		Long: `Move a branch to a new parent, rebasing only its own commits onto the target.

This command changes the branch's parent pointer and rebases the branch's commits
onto the new parent. Only the branch's own commits are moved - commits from
ancestor branches are NOT included.

After moving, all descendant branches are automatically restacked.

Examples:
  stackit move --onto main           # Move current branch to be a child of main
  stackit move --source feature-b --onto feature-a  # Move feature-b onto feature-a

If no --onto is specified, opens an interactive selector to choose the target.`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				// Default source to current branch
				sourceBranch := source
				if sourceBranch == "" {
					currentBranch := ctx.Engine.CurrentBranch()
					if currentBranch == nil {
						return fmt.Errorf("not on a branch and no source branch specified")
					}
					sourceBranch = currentBranch.GetName()
				}

				ontoBranch := onto

				// Create runner and handler
				runner, handler := NewMoveUI(ctx.Output, ctx.Logger, ctx.Interactive)
				if runner != nil {
					defer runner.Cleanup()
				}

				// Run move action
				return move.Action(ctx, move.Options{
					Source:      sourceBranch,
					Onto:        ontoBranch,
					SkipConfirm: yes,
					DryRun:      dryRun,
					AutoRename:  rename,
				}, handler)
			})
		},
	}

	// Add flags
	cmd.Flags().BoolVarP(&all, "all", "a", false, "Show branches across all configured trunks in interactive selection.")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would happen without making changes.")
	cmd.Flags().StringVarP(&onto, "onto", "o", "", "Branch to move the current branch onto.")
	cmd.Flags().StringVar(&source, "source", "", "Branch to move (defaults to current branch).")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt.")
	cmd.Flags().BoolVar(&rename, "rename", false, "Auto-rename branch when scope changes during move.")

	_ = cmd.RegisterFlagCompletionFunc("onto", common.CompleteBranches)
	_ = cmd.RegisterFlagCompletionFunc("source", common.CompleteBranches)

	return cmd
}
