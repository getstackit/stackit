package split

import (
	"fmt"

	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/engine"
)

// WizardOptions configures the interactive split wizard
type WizardOptions struct {
	// Style is pre-selected split style (empty = prompt user)
	Style Style
	// Direction is pre-selected direction (empty = prompt user)
	Direction Direction
	// BranchName is a pre-selected branch name (empty = prompt or auto-generate)
	BranchName string
	// HunkSelector specifies which hunk selection method to use ("tui" or "git")
	HunkSelector string
}

// RunWizard executes the interactive split wizard.
// It guides the user through selecting split type, direction, and then
// executes the appropriate split operation.
func RunWizard(ctx *app.Context, handler InteractiveHandler, opts WizardOptions) error {
	eng := ctx.Engine
	out := ctx.Output

	out.Debug("split wizard: starting with opts=%+v", opts)

	// Get current branch
	currentBranch := eng.CurrentBranch()
	if currentBranch == nil {
		return fmt.Errorf("not on a branch")
	}

	if err := currentBranch.EnsureCanModify(); err != nil {
		return err
	}

	// Check for uncommitted tracked changes
	hasUnstaged, err := eng.HasUnstagedChanges(ctx.Context)
	if err != nil {
		return fmt.Errorf("failed to check unstaged changes: %w", err)
	}
	if hasUnstaged {
		return fmt.Errorf("cannot split: you have uncommitted tracked changes")
	}

	// Ensure branch is tracked
	currentBranchObj := eng.GetBranch(currentBranch.GetName())
	if !currentBranchObj.IsTracked() {
		parent := currentBranch.GetParent()
		parentName := ""
		if parent == nil {
			parentName = eng.Trunk().GetName()
		} else {
			parentName = parent.GetName()
		}
		if err := eng.TrackBranch(ctx.Context, currentBranch.GetName(), parentName); err != nil {
			return fmt.Errorf("failed to track branch: %w", err)
		}
	}

	// Get commit count to determine available options
	commits, err := currentBranch.GetAllCommits(engine.CommitFormatSHA)
	if err != nil {
		return fmt.Errorf("failed to get commits: %w", err)
	}
	hasMultipleCommits := len(commits) > 1

	// Step 1: Choose split type (if not pre-selected)
	style := opts.Style
	if style == "" {
		handler.OnStep(StepChoosingType, StatusStarted, "Choose split type")

		availableTypes := buildTypeChoices(hasMultipleCommits)
		selectedStyle, err := handler.PromptSplitType(availableTypes)
		if err != nil {
			return err
		}
		style = selectedStyle

		handler.OnStep(StepChoosingType, StatusCompleted, string(style))
		out.Debug("split wizard: user selected style: %s", style)
	}

	// Step 2: Choose direction (if not pre-selected)
	direction := opts.Direction
	if direction == "" {
		handler.OnStep(StepChoosingDirection, StatusStarted, "Choose direction")

		// Build context for direction prompt
		parentName := ""
		if parent := currentBranch.GetParent(); parent != nil {
			parentName = parent.GetName()
		} else {
			parentName = eng.Trunk().GetName()
		}

		// Get children of current branch
		graph := engine.BuildStackGraph(eng, engine.SortStrategyAlphabetical, nil)
		childNames := graph.Children(*currentBranch)

		dirCtx := DirectionContext{
			Engine:        eng,
			CurrentBranch: currentBranch.GetName(),
			ParentBranch:  parentName,
			Children:      childNames,
		}

		selectedDirection, err := handler.PromptDirection(dirCtx)
		if err != nil {
			return err
		}
		direction = selectedDirection

		handler.OnStep(StepChoosingDirection, StatusCompleted, string(direction))
		out.Debug("split wizard: user selected direction: %s", direction)
	}

	// Start handler with branch info and style
	handler.Start(currentBranch.GetName(), style)

	// Take snapshot before any modifications
	snapshotArgs := []string{string(style), string(direction)}
	if err := eng.TakeSnapshot(engine.SnapshotOptions{
		Command: "split",
		Args:    snapshotArgs,
	}); err != nil {
		return fmt.Errorf("failed to take snapshot: %w", err)
	}

	// Execute the appropriate split based on style
	switch style {
	case StyleHunk:
		hunkOpts := hunkOptions{
			useGitAddP: opts.HunkSelector == "git",
		}
		return splitByHunkWithHandler(ctx, *currentBranch, eng, out, handler, direction, hunkOpts)
	case StyleCommit, StyleFile:
		// For now, these don't support the new wizard flow with direction
		// Fall back to the existing implementation
		return fmt.Errorf("style %s with direction is not yet implemented in wizard mode", style)
	default:
		return fmt.Errorf("unknown split style: %s", style)
	}
}

// buildTypeChoices creates the list of available split type options
func buildTypeChoices(hasMultipleCommits bool) []TypeChoice {
	return []TypeChoice{
		{
			Style:       StyleHunk,
			Label:       "By hunk",
			Description: "Interactively select code changes to extract into new branches",
			Available:   true,
		},
		{
			Style:       StyleFile,
			Label:       "By file",
			Description: "Extract entire files to a new branch",
			Available:   true,
		},
		{
			Style:       StyleCommit,
			Label:       "By commit",
			Description: "Divide commit history into separate branches",
			Available:   hasMultipleCommits,
		},
	}
}
