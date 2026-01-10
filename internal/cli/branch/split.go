// Package branch provides CLI commands for managing branches in a stack.
package branch

import (
	"github.com/spf13/cobra"

	"stackit.dev/stackit/internal/actions/split"
	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/cli/common"
	"stackit.dev/stackit/internal/config"
)

// NewSplitCmd creates the split command
func NewSplitCmd() *cobra.Command {
	var (
		byCommit          bool
		byHunk            bool
		byFile            []string
		byFileInteractive bool
	)

	cmd := &cobra.Command{
		Use:     "split",
		Aliases: []string{"sp"},
		Short:   "Split the current branch into multiple branches",
		Long: `Split the current branch into multiple branches.

Has three forms: split --by-commit, split --by-hunk, and split --by-file.
split --by-commit slices up the commit history, allowing you to select split points.
split --by-hunk interactively stages changes to create new single-commit branches.
split --by-file <files> extracts specified files into a new parent branch.
split -F (--by-file-interactive) shows an interactive file selector.
split without options will prompt for a splitting strategy.`,
		SilenceUsage: true,
		// Disable default help flag to allow -h for --by-hunk
		DisableFlagParsing: false,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				// Determine split style - check all flag variants
				var style split.Style
				switch {
				case byCommit || cmd.Flags().Changed("commit"):
					style = split.StyleCommit
				case byHunk || cmd.Flags().Changed("hunk"):
					style = split.StyleHunk
				case byFileInteractive || len(byFile) > 0 || cmd.Flags().Changed("file"):
					// -F triggers interactive file selection
					// --by-file with pathspecs uses those files directly
					if cmd.Flags().Changed("file") {
						filePaths, _ := cmd.Flags().GetStringSlice("file")
						byFile = filePaths
					}
					style = split.StyleFile
				}
				// If style is empty, SplitAction will prompt

				// Load config for branch pattern
				cfg, _ := config.LoadConfig(ctx.RepoRoot)
				branchPattern := cfg.GetBranchPattern()

				// Create runner and handler
				runner, handler := NewSplitUI(ctx.Output, ctx.Logger)
				if runner != nil {
					defer runner.Cleanup()
				}

				// Run split action
				return split.Action(ctx, split.Options{
					Style:         style,
					Pathspecs:     byFile,
					BranchPattern: branchPattern,
				}, handler)
			})
		},
	}

	// Disable the default help flag shorthand to allow -h for --by-hunk
	cmd.Flags().BoolP("help", "", false, "help for split")

	// Define flags - cobra allows multiple long forms but only one shorthand per variable
	cmd.Flags().BoolVarP(&byCommit, "by-commit", "c", false, "Split by commit - slice up the history of this branch")
	cmd.Flags().BoolVarP(&byHunk, "by-hunk", "h", false, "Split by hunk - split into new single-commit branches")
	cmd.Flags().StringSliceVarP(&byFile, "by-file", "f", nil, "Split by file - extracts specified files to a new parent branch")
	cmd.Flags().BoolVarP(&byFileInteractive, "by-file-interactive", "F", false, "Split by file (interactive) - select files to extract")

	// Add alternative long form names (these will be checked in RunE via cmd.Flags().Changed)
	// Note: We can't bind the same variable twice, so we check for these flags manually
	_ = cmd.Flags().Bool("commit", false, "Alias for --by-commit")
	_ = cmd.Flags().Bool("hunk", false, "Alias for --by-hunk")
	_ = cmd.Flags().StringSlice("file", nil, "Alias for --by-file")

	return cmd
}
