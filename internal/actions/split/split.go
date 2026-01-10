// Package split provides functionality for splitting stacked branches.
package split

import (
	"fmt"
	"strings"

	"github.com/AlecAivazis/survey/v2"

	"stackit.dev/stackit/internal/actions"
	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/config"
	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/utils"
)

// Style specifies the split mode
type Style string

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
	Pathspecs     []string
	BranchPattern config.BranchPattern
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

	// Get current branch
	currentBranch := eng.CurrentBranch()
	if currentBranch == nil {
		return fmt.Errorf("not on a branch")
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
		parent := currentBranch.GetParent()
		parentName := ""
		if parent == nil {
			// Try to find parent from git
			parentName = eng.Trunk().GetName()
		} else {
			parentName = parent.GetName()
		}
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
			if !utils.IsInteractive() {
				return fmt.Errorf("split style must be specified in non-interactive mode")
			}
			// Prompt for style
			var styleStr string
			prompt := &survey.Select{
				Message: fmt.Sprintf("How would you like to split %s?", currentBranch.GetName()),
				Options: []string{"By commit - slice up the history of this branch", "By hunk - split into new single-commit branches", "Cancel"},
			}
			if err := survey.AskOne(prompt, &styleStr); err != nil {
				return fmt.Errorf("canceled")
			}

			switch {
			case strings.Contains(styleStr, "Cancel"):
				return fmt.Errorf("canceled")
			case strings.Contains(styleStr, "commit"):
				style = StyleCommit
			case strings.Contains(styleStr, "hunk"):
				style = StyleHunk
			}
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
		result, err = splitByHunk(context, *currentBranch, eng, out)
	case StyleFile:
		pathspecs := opts.Pathspecs
		// If no pathspecs provided, prompt interactively
		if len(pathspecs) == 0 {
			pathspecs, err = promptForFiles(context, *currentBranch, eng, out)
			if err != nil {
				return err
			}
			if len(pathspecs) == 0 {
				return fmt.Errorf("no files selected")
			}
		}
		// splitByFile handles everything internally (creating branches, tracking, etc.)
		// and updates the parent relationship, so we just need to restack upstack branches
		_, err = splitByFile(context, *currentBranch, pathspecs, eng)
		if err != nil {
			return err
		}
		// Restack upstack branches if any
		rng := engine.StackRange{
			RecursiveParents:  false,
			IncludeCurrent:    false,
			RecursiveChildren: true,
		}
		graph := engine.BuildStackGraph(eng, engine.SortStrategyAlphabetical, nil)
		upstackBranches := graph.Range(*currentBranch, rng)
		if len(upstackBranches) > 0 {
			if err := actions.RestackBranches(ctx, upstackBranches); err != nil {
				return fmt.Errorf("failed to restack upstack branches: %w", err)
			}
		}
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
	upstackGraph := engine.BuildStackGraph(eng, engine.SortStrategyAlphabetical, nil)
	upstackBranches := upstackGraph.Range(*currentBranch, upstackRng)

	// Apply the split
	if err := eng.ApplySplitToCommits(context, engine.ApplySplitOptions{
		BranchToSplit: currentBranch.GetName(),
		BranchNames:   result.BranchNames,
		BranchPoints:  result.BranchPoints,
	}); err != nil {
		return fmt.Errorf("failed to apply split: %w", err)
	}

	// Restack upstack branches
	if len(upstackBranches) > 0 {
		if err := actions.RestackBranches(ctx, upstackBranches); err != nil {
			return fmt.Errorf("failed to restack upstack branches: %w", err)
		}
	}

	return nil
}
