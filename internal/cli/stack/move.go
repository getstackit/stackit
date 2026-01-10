// Package stack provides CLI commands for operating on entire stacks.
package stack

import (
	"fmt"

	"github.com/spf13/cobra"

	"stackit.dev/stackit/internal/actions/move"
	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/cli/common"
	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/tui"
)

// NewMoveCmd creates the move command
func NewMoveCmd() *cobra.Command {
	var (
		all    bool
		onto   string
		source string
	)

	cmd := &cobra.Command{
		Use:   "move",
		Short: "Rebase the current branch onto the target branch",
		Long: `Rebase the current branch onto the target branch and restack all of its descendants.

If no branch is passed in, opens an interactive selector to choose the target branch.`,
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

				// Handle interactive selection for onto if not provided
				ontoBranch := onto
				if ontoBranch == "" {
					var err error
					ontoBranch, err = interactiveOntoSelection(ctx, sourceBranch)
					if err != nil {
						return err
					}
				}

				// Create runner and handler
				runner, handler := NewMoveUI(ctx.Output, ctx.Logger, ctx.Interactive)
				if runner != nil {
					defer runner.Cleanup()
				}

				// Run move action
				return move.Action(ctx, move.Options{
					Source: sourceBranch,
					Onto:   ontoBranch,
				}, handler)
			})
		},
	}

	// Add flags
	cmd.Flags().BoolVarP(&all, "all", "a", false, "Show branches across all configured trunks in interactive selection.")
	cmd.Flags().StringVarP(&onto, "onto", "o", "", "Branch to move the current branch onto.")
	cmd.Flags().StringVar(&source, "source", "", "Branch to move (defaults to current branch).")

	_ = cmd.RegisterFlagCompletionFunc("onto", common.CompleteBranches)
	_ = cmd.RegisterFlagCompletionFunc("source", common.CompleteBranches)

	return cmd
}

// interactiveOntoSelection shows an interactive branch selector for choosing the "onto" branch
func interactiveOntoSelection(ctx *app.Context, sourceBranch string) (string, error) {
	eng := ctx.Engine

	// Get descendants of source to exclude them
	graph := engine.BuildStackGraph(eng, engine.SortStrategyAlphabetical, nil)
	descendants := graph.Range(eng.GetBranch(sourceBranch), engine.StackRange{
		RecursiveChildren: true,
		IncludeCurrent:    true,
		RecursiveParents:  false,
	})
	excludedBranches := make(map[string]bool)
	for _, d := range descendants {
		excludedBranches[d.GetName()] = true
	}

	// Show interactive selector
	selected, err := tui.PromptLogSelect(ctx.Context, ctx.Engine, ctx.GitHubClient, tui.LogOptions{
		Style:   "FULL",
		Exclude: excludedBranches,
		Logger:  ctx.Logger,
	})
	if err != nil {
		return "", err
	}

	return selected, nil
}
