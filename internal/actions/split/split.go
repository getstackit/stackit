// Package split provides functionality for splitting stacked branches.
package split

import (
	"fmt"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/config"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/errors"
)

// Style specifies the split mode
type Style string

// splitSuffix is appended to a branch name when generating a default split branch name.
const splitSuffix = "_split"

const (
	// StyleCommit splits by selecting commit points
	StyleCommit Style = "commit"
	// StyleHunk splits by interactively staging hunks
	StyleHunk Style = "hunk"
	// StyleFile splits by extracting specified files
	StyleFile Style = "file"
)

// Options contains options for the split command
type Options struct {
	Style         Style
	Direction     Direction
	Pathspecs     []string
	BranchPattern config.BranchPattern
	// AsSibling creates split branches as siblings on the same parent instead
	// of creating a linear chain. When true:
	// - StyleFile: Creates sibling branch, original branch unchanged
	// - StyleCommit: All split branches are siblings on the same parent
	// - StyleHunk: All split branches are siblings on the same parent
	AsSibling bool
	// Name specifies a custom name for the split branch (file split only).
	// If empty, auto-generates as "{original}_split".
	Name string
	// Message specifies the commit message for the split operation.
	// Only applies to StyleFile (other styles use existing commit messages).
	Message string
	// UseWizard enables the new wizard-based interactive flow.
	// When true, the wizard will guide through type/direction selection.
	UseWizard bool
	// HunkSelector specifies which hunk selection method to use ("tui" or "git").
	// Only applies to StyleHunk. When "git", uses git add -p instead of the TUI selector.
	HunkSelector string
	// PatchFile specifies a patch file for non-interactive hunk selection.
	// If set, hunks in the patch are staged directly without prompting.
	// Use "-" to read from stdin.
	PatchFile string
	// DryRun shows what would happen without executing the split.
	// When true, the split logic runs but no branches are created or modified.
	DryRun bool
}

// Result contains the result of a split operation
type Result struct {
	BranchNames  []string // From oldest to newest
	BranchPoints []int    // Commit indices (0 = HEAD, 1 = HEAD~1, etc.)
}

// Action performs the split operation
func Action(ctx *app.Context, opts Options, handler Handler) error {
	eng := ctx.Engine
	out := ctx.Output
	context := ctx.Context

	// Use null handler if none provided
	if handler == nil {
		handler = &NullHandler{}
	}
	defer handler.Cleanup()

	// If wizard mode is requested and handler supports interactive prompts, use wizard flow
	if opts.UseWizard {
		if interactiveHandler, ok := handler.(InteractiveHandler); ok && interactiveHandler.IsInteractive() {
			return RunWizard(ctx, interactiveHandler, WizardOptions{
				Style:        opts.Style,
				Direction:    opts.Direction,
				HunkSelector: opts.HunkSelector,
			})
		}
		// Fall back to standard flow if handler doesn't support interactive
	}

	// Get current branch
	currentBranch := eng.CurrentBranch()
	if currentBranch == nil {
		return errors.ErrNotOnBranch
	}

	if err := currentBranch.EnsureCanModify(); err != nil {
		return err
	}

	// Check for uncommitted tracked changes
	hasUnstaged, err := eng.HasUnstagedChanges(context)
	if err != nil {
		return fmt.Errorf("failed to check unstaged changes: %w", err)
	}
	if hasUnstaged {
		return fmt.Errorf("cannot split: you have uncommitted tracked changes")
	}

	// Ensure branch is tracked
	currentBranchObj := eng.GetBranch(currentBranch.GetName())
	if !currentBranchObj.IsTracked() {
		// Auto-track the branch
		parentName := currentBranch.GetParentOrTrunk()
		if err := eng.TrackBranch(context, currentBranch.GetName(), parentName); err != nil {
			return fmt.Errorf("failed to track branch: %w", err)
		}
	}

	// Determine style
	style := opts.Style
	if style == "" {
		// Check if there's more than one commit
		commits, err := currentBranch.GetAllCommits(engine.CommitFormatSHA)
		if err != nil {
			return fmt.Errorf("failed to get commits: %w", err)
		}

		if len(commits) > 1 {
			// Need interactive prompt to choose split type
			interactiveHandler, isInteractive := handler.(InteractiveHandler)
			if !isInteractive || !interactiveHandler.IsInteractive() {
				return fmt.Errorf("split style must be specified in non-interactive mode (use --by-commit, --by-hunk, or --by-file)")
			}

			// Build type choices based on commit count
			availableTypes := buildTypeChoices(true)
			selectedStyle, err := interactiveHandler.PromptSplitType(availableTypes)
			if err != nil {
				return err
			}
			style = selectedStyle
		} else {
			// Only one commit, default to hunk
			style = StyleHunk
		}
	}

	// Start handler with branch info and style
	handler.Start(currentBranch.GetName(), style)

	// Take snapshot before any modifications
	snapshotArgs := []string{string(style)}
	if style == StyleFile && len(opts.Pathspecs) > 0 {
		snapshotArgs = append(snapshotArgs, opts.Pathspecs...)
	}

	if err := eng.TakeSnapshot(engine.SnapshotOptions{
		Command: "split",
		Args:    snapshotArgs,
	}); err != nil {
		return fmt.Errorf("failed to take snapshot: %w", err)
	}

	// Perform the split
	var result *Result
	switch style {
	case StyleCommit:
		result, err = splitByCommit(ctx, currentBranch.GetName(), eng, out, opts.BranchPattern)
	case StyleHunk:
		// Use provided direction or default to below
		direction := opts.Direction
		if direction == "" {
			direction = DirectionBelow
		}

		// Hunk options for the split
		hunkOpts := hunkOptions{
			useGitAddP: opts.HunkSelector == "git",
			patchFile:  opts.PatchFile,
			name:       opts.Name,
			message:    opts.Message,
		}

		// If patch file is provided, we can run non-interactively
		if opts.PatchFile != "" {
			return splitByHunkWithHandler(ctx, *currentBranch, eng, out, nil, direction, hunkOpts)
		}

		// Interactive hunk split requires an interactive handler
		interactiveHandler, isInteractive := handler.(InteractiveHandler)
		if !isInteractive || !interactiveHandler.IsInteractive() {
			return fmt.Errorf("hunk split requires interactive mode")
		}
		return splitByHunkWithHandler(ctx, *currentBranch, eng, out, interactiveHandler, direction, hunkOpts)
	case StyleFile:
		pathspecs := opts.Pathspecs
		// If no pathspecs provided, prompt interactively
		if len(pathspecs) == 0 {
			pathspecs, err = promptForFiles(context, *currentBranch, eng, out, opts.AsSibling, opts.Direction)
			if err != nil {
				return err
			}
			if len(pathspecs) == 0 {
				return fmt.Errorf("no files selected")
			}
		}
		// splitByFile handles everything internally (creating branches, tracking, etc.)
		// and updates the parent relationship (unless AsSibling is true)
		fileResult, err := splitByFile(context, *currentBranch, pathspecs, eng, splitByFileOptions{
			AsSibling: opts.AsSibling,
			Direction: opts.Direction,
			Name:      opts.Name,
			Message:   opts.Message,
			DryRun:    opts.DryRun,
		})
		if err != nil {
			return err
		}

		// Dry-run mode: show preview and exit
		if opts.DryRun {
			out.Info("Dry Run - Split Preview")
			out.Info("========================")
			out.Info("Current branch: %s", currentBranch.GetName())
			switch {
			case opts.Direction == DirectionAbove:
				out.Info("New child branch: %s", fileResult.BranchNames[0])
				out.Info("Direction: above (extract to child)")
			case opts.AsSibling:
				out.Info("New sibling branch: %s", fileResult.BranchNames[0])
				out.Info("Direction: sibling (original unchanged)")
			default:
				out.Info("New parent branch: %s", fileResult.BranchNames[0])
				out.Info("Direction: below (extract to parent)")
			}
			out.Info("")
			out.Info("Files to extract:")
			for _, p := range pathspecs {
				out.Info("  %s", p)
			}
			out.Info("")
			out.Info("Run without --dry-run to execute.")
			return nil
		}

		// Restack upstack branches if any (not needed for --above since splitByFileAbove handles it)
		if opts.Direction != DirectionAbove {
			rng := engine.StackRange{
				RecursiveParents:  false,
				IncludeCurrent:    false,
				RecursiveChildren: true,
			}
			graph := eng.Graph(engine.SortStrategyAlphabetical)
			upstackBranches := graph.Range(*currentBranch, rng)
			if len(upstackBranches) > 0 {
				if err := actions.RestackBranches(ctx, upstackBranches); err != nil {
					return fmt.Errorf("failed to restack upstack branches: %w", err)
				}
			}
		}
		handler.Complete(ActionResult{
			OriginalBranch: currentBranch.GetName(),
			NewBranches:    fileResult.BranchNames,
			Style:          style,
		})
		return nil
	default:
		return fmt.Errorf("unknown split style: %s", style)
	}

	if err != nil {
		return err
	}

	// Get upstack branches (children)
	upstackRng := engine.StackRange{
		RecursiveParents:  false,
		IncludeCurrent:    false,
		RecursiveChildren: true,
	}
	upstackGraph := eng.Graph(engine.SortStrategyAlphabetical)
	upstackBranches := upstackGraph.Range(*currentBranch, upstackRng)

	// Apply the split
	if err := eng.ApplySplitToCommits(context, engine.ApplySplitOptions{
		BranchToSplit: currentBranch.GetName(),
		BranchNames:   result.BranchNames,
		BranchPoints:  result.BranchPoints,
		AsSibling:     opts.AsSibling,
	}); err != nil {
		// Restore to original branch to avoid leaving user in detached HEAD
		_ = eng.ForceCheckoutBranch(context, *currentBranch)
		return fmt.Errorf("failed to apply split: %w", err)
	}

	// Restack upstack branches
	if len(upstackBranches) > 0 {
		if err := actions.RestackBranches(ctx, upstackBranches); err != nil {
			return fmt.Errorf("failed to restack upstack branches: %w", err)
		}
	}

	handler.Complete(ActionResult{
		OriginalBranch: currentBranch.GetName(),
		NewBranches:    result.BranchNames,
		Style:          style,
	})
	return nil
}
