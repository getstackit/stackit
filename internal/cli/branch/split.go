// Package branch provides CLI commands for managing branches in a stack.
package branch

import (
	"fmt"

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
		asSibling         bool
		above             bool
		below             bool
		name              string
		message           string
		patchFile         string
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
split without options will launch an interactive wizard.

Direction options for --by-hunk:
  --below (default): New branch inserted between current and parent (downstack)
  --above: New branch inserted as child of current (upstack)

Non-interactive mode (--patch):
  --patch <file>: Read hunks from a patch file instead of prompting interactively.
                  Use "-" to read from stdin. Implies --by-hunk --below unless --above is specified.

By default, --by-file creates a new PARENT branch, making the current branch
a child of the split branch. Use --as-sibling to create an independent branch
on the same parent instead (leaving the current branch unchanged).

Examples:
  stackit split                                        # Interactive wizard
  stackit split --by-hunk                              # Skip type selection
  stackit split --by-hunk --below                      # Skip type and direction
  stackit split --by-hunk --above                      # Split upstack (child)
  stackit split --patch extract.patch -n parent -m "Extract to parent"  # Non-interactive (below)
  stackit split --patch extract.patch --above -n child -m "Extract to child"  # Non-interactive (above)
  stackit split --by-file path/to/file.go             # Extract to parent branch
  stackit split --by-file path/to/file.go --as-sibling # Extract to sibling branch
  stackit split --by-commit --as-sibling              # Split commits as siblings`,
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
				case patchFile != "":
					// --patch implies --by-hunk
					style = split.StyleHunk
				}

				// Determine direction
				var direction split.Direction
				switch {
				case above && below:
					return fmt.Errorf("cannot specify both --above and --below")
				case above:
					direction = split.DirectionAbove
				case below:
					direction = split.DirectionBelow
				case patchFile != "":
					// --patch defaults to --below if no direction specified
					direction = split.DirectionBelow
				}
				// If direction is empty, wizard will prompt (for hunk mode)

				// Validate flag combinations
				// --patch can only be used with --by-hunk
				if patchFile != "" && style != split.StyleHunk {
					return fmt.Errorf("--patch can only be used with --by-hunk")
				}
				// --name and --message require --by-file or --by-hunk with --patch
				if name != "" && style != split.StyleFile && patchFile == "" {
					return fmt.Errorf("--name can only be used with --by-file or --by-hunk --patch")
				}
				if message != "" && style != split.StyleFile && patchFile == "" {
					return fmt.Errorf("--message can only be used with --by-file or --by-hunk --patch")
				}
				// --above/--below only make sense with --by-hunk
				if (above || below) && style != "" && style != split.StyleHunk {
					return fmt.Errorf("--above/--below can only be used with --by-hunk")
				}

				// Load config for branch pattern and hunk selector
				cfg, _ := config.LoadConfig(ctx.RepoRoot)
				branchPattern := cfg.GetBranchPattern()
				hunkSelector := cfg.SplitHunkSelector()

				// Create runner and handler
				runner, handler := NewSplitUI(ctx.Output, ctx.Logger)
				if runner != nil {
					defer runner.Cleanup()
				}

				// Determine if we should use wizard mode
				// Use wizard when: no style specified, or style is hunk with no direction
				// Never use wizard when --patch is provided (non-interactive mode)
				useWizard := (style == "" || (style == split.StyleHunk && direction == "")) && patchFile == ""

				// Run split action
				return split.Action(ctx, split.Options{
					Style:         style,
					Direction:     direction,
					Pathspecs:     byFile,
					BranchPattern: branchPattern,
					AsSibling:     asSibling,
					Name:          name,
					Message:       message,
					UseWizard:     useWizard,
					HunkSelector:  hunkSelector,
					PatchFile:     patchFile,
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

	// Additional options
	cmd.Flags().BoolVar(&asSibling, "as-sibling", false, "Create split branches as siblings instead of a chain")
	cmd.Flags().StringVarP(&name, "name", "n", "", "Name for the new split branch (default: auto-generated)")
	cmd.Flags().StringVarP(&message, "message", "m", "", "Commit message for extraction (only with --by-file)")

	// Direction options (for hunk mode)
	cmd.Flags().BoolVar(&above, "above", false, "Insert new branch above current (as child, upstack)")
	cmd.Flags().BoolVar(&below, "below", false, "Insert new branch below current (as parent, downstack)")

	// Non-interactive hunk mode
	cmd.Flags().StringVarP(&patchFile, "patch", "p", "", "Patch file specifying hunks to split (implies --by-hunk, defaults to --below, use \"-\" for stdin)")

	// Add alternative long form names (these will be checked in RunE via cmd.Flags().Changed)
	// Note: We can't bind the same variable twice, so we check for these flags manually
	_ = cmd.Flags().Bool("commit", false, "Alias for --by-commit")
	_ = cmd.Flags().Bool("hunk", false, "Alias for --by-hunk")
	_ = cmd.Flags().StringSlice("file", nil, "Alias for --by-file")

	return cmd
}
