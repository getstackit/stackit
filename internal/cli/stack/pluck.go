package stack

import (
	"fmt"

	"github.com/spf13/cobra"

	"stackit.dev/stackit/internal/actions/pluck"
	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/cli/common"
	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/tui"
)

// NewPluckCmd creates the pluck command
func NewPluckCmd() *cobra.Command {
	var (
		onto   string
		source string
		yes    bool
	)

	cmd := &cobra.Command{
		Use:   "pluck",
		Short: "Extract a branch from the middle of a stack",
		Long: `Pluck extracts a single branch from its current position and moves it to a new parent.

Unlike 'move', pluck does NOT bring descendants along. The direct children of the
plucked branch are reparented to the plucked branch's former parent (grandparent).

This is useful when you realize a change doesn't belong in a stack and should be
independent, or when reorganizing stacks without moving entire subtrees.

Examples:
  stackit pluck --onto main           # Pluck current branch to be a child of main
  stackit pluck --source feat-b --onto feat-x  # Pluck feat-b onto feat-x

If no --onto is specified, opens an interactive selector to choose the target.

Note: This operation requires validation that rebases will succeed. If conflicts
would occur, the pluck will abort without making any changes.`,
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
					ontoBranch, err = interactivePluckOntoSelection(ctx, sourceBranch)
					if err != nil {
						return err
					}
				}

				// Create runner and handler
				runner, handler := NewPluckUI(ctx.Output, ctx.Logger, ctx.Interactive)
				if runner != nil {
					defer runner.Cleanup()
				}

				// Run pluck action
				return pluck.Action(ctx, pluck.Options{
					Source:      sourceBranch,
					Onto:        ontoBranch,
					SkipConfirm: yes,
				}, handler)
			})
		},
	}

	// Add flags
	cmd.Flags().StringVarP(&onto, "onto", "o", "", "Branch to pluck the source branch onto.")
	cmd.Flags().StringVar(&source, "source", "", "Branch to pluck (defaults to current branch).")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt.")

	_ = cmd.RegisterFlagCompletionFunc("onto", common.CompleteBranches)
	_ = cmd.RegisterFlagCompletionFunc("source", common.CompleteBranches)

	return cmd
}

// interactivePluckOntoSelection shows an interactive branch selector for choosing the "onto" branch
func interactivePluckOntoSelection(ctx *app.Context, sourceBranch string) (string, error) {
	eng := ctx.Engine

	// Get descendants of source to exclude them (can't pluck onto descendant)
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
	header := fmt.Sprintf("Select new parent for '%s' (children will be reparented to grandparent)", sourceBranch)
	selected, err := tui.PromptLogSelect(ctx.Context, ctx.Engine, ctx.GitHubClient, tui.LogOptions{
		Style:   "FULL",
		Exclude: excludedBranches,
		Logger:  ctx.Logger,
		Header:  header,
	})
	if err != nil {
		return "", err
	}

	return selected, nil
}
