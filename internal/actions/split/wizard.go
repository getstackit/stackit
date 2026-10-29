package split

import (
	"context"
	"fmt"

	"github.com/getstackit/stackit/internal/actions"
	handlerBase "github.com/getstackit/stackit/internal/actions/handler"
	"github.com/getstackit/stackit/internal/config"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/errors"
)

// WizardOptions configures the interactive split wizard
type WizardOptions struct {
	// Style is pre-selected split style (empty = prompt user)
	Style Style
	// Direction is pre-selected direction (empty = prompt user)
	Direction Direction
	// BranchName is a pre-selected branch name (empty = prompt or auto-generate)
	BranchName string
	// HunkSelector specifies which hunk selection method to use
	HunkSelector HunkSelector
	// BranchPattern is the resolved branch.pattern used to suggest a default
	// branch name (empty = fall back to "<current>_split").
	BranchPattern config.BranchPattern
}

// RunWizard executes the interactive split wizard.
// It guides the user through selecting split type, direction, and then
// executes the appropriate split operation.
func RunWizard(ctx context.Context, deps Deps, handler InteractiveHandler, opts WizardOptions) error {
	eng, out := deps.Engine, deps.Out

	out.Debug("split wizard: starting with opts=%+v", opts)

	// Get current branch
	currentBranch := eng.CurrentBranch(ctx)
	if currentBranch == nil {
		return errors.ErrNotOnBranch
	}

	if err := currentBranch.EnsureCanModify(); err != nil {
		return err
	}
	// The wizard is the default `stackit split` path, so Action returns here
	// before reaching its own ownership guard. Splitting rewrites the branch's
	// commits, which is only safe in the worktree that owns it — guard before
	// the snapshot below, so a refusal is a no-op.
	if err := actions.EnsureCanModify(ctx, deps.Engine, deps.Here, *currentBranch); err != nil {
		return err
	}

	// Check for uncommitted tracked changes
	hasUnstaged, err := eng.HasUnstagedChanges(ctx)
	if err != nil {
		return fmt.Errorf("failed to check unstaged changes: %w", err)
	}
	if hasUnstaged {
		return errors.New("cannot split: you have uncommitted tracked changes")
	}

	// Ensure branch is tracked
	currentBranchObj := eng.GetBranch(currentBranch.Name())
	if !currentBranchObj.IsTracked() {
		parentName := currentBranch.ParentOrTrunk()
		if err := eng.TrackBranch(ctx, currentBranch.Name(), parentName); err != nil {
			return fmt.Errorf("failed to track branch: %w", err)
		}
	}

	// Get commit count to determine available options
	commits, err := currentBranch.CommitIDs(ctx)
	if err != nil {
		return fmt.Errorf("failed to get commits: %w", err)
	}
	hasMultipleCommits := len(commits) > 1

	// Step 1 & 2: Choose split type and direction (with back navigation support)
	style := opts.Style
	direction := opts.Direction
	availableTypes := buildTypeChoices(hasMultipleCommits)

	// Loop to allow going back from direction to type selection
	for {
		// Step 1: Choose split type (if not pre-selected)
		if style == "" {
			handler.OnStep(StepChoosingType, handlerBase.StatusStarted, "Choose split type")

			selectedStyle, err := handler.PromptSplitType(ctx, availableTypes)
			if err != nil {
				return err
			}
			style = selectedStyle

			handler.OnStep(StepChoosingType, handlerBase.StatusCompleted, string(style))
			out.Debug("split wizard: user selected style: %s", style)
		}

		// Step 2: Choose direction (if not pre-selected)
		if direction == "" {
			handler.OnStep(StepChoosingDirection, handlerBase.StatusStarted, "Choose direction")

			// Build context for direction prompt
			parentName := ""
			if parent := currentBranch.Parent(); parent != nil {
				parentName = parent.Name()
			} else {
				parentName = eng.Trunk().Name()
			}

			// Get children of current branch
			graph := eng.Graph(ctx, engine.SortStrategyAlphabetical)
			childNames := graph.Children(*currentBranch)

			dirCtx := DirectionContext{
				Engine:        eng,
				CurrentBranch: currentBranch.Name(),
				ParentBranch:  parentName,
				Children:      childNames,
			}

			selectedDirection, err := handler.PromptDirection(ctx, dirCtx)
			if errors.Is(err, errors.ErrBack) {
				// User wants to go back to type selection
				style = ""
				out.Debug("split wizard: user pressed back, returning to type selection")
				continue
			}
			if err != nil {
				return err
			}
			direction = selectedDirection

			handler.OnStep(StepChoosingDirection, handlerBase.StatusCompleted, string(direction))
			out.Debug("split wizard: user selected direction: %s", direction)
		}

		// Both selections made, break out of the loop
		break
	}

	// Start handler with branch info and style
	handler.Start(currentBranch.Name(), style)

	// Take snapshot before any modifications
	snapshotArgs := []string{string(style), string(direction)}
	if err := eng.TakeSnapshot(ctx, engine.SnapshotOptions{
		Command: "split",
		// split stages and commits whatever is in the working tree as it
		// rebuilds the branch (git add -A), untracked files included, so
		// uncommitted changes are its input too.
		Capture: engine.WorktreeCaptureUntracked,
		Args:    snapshotArgs,
	}); err != nil {
		return fmt.Errorf("failed to take snapshot: %w", err)
	}

	// Execute the appropriate split based on style
	switch style {
	case StyleHunk:
		hunkOpts := hunkOptions{
			useGitAddP: opts.HunkSelector == HunkSelectorGit,
		}
		return splitByHunkWithHandler(ctx, deps, *currentBranch, eng, out, handler, direction, hunkOpts)

	case StyleFile:
		// Prompt for files to extract
		pathspecs, err := promptForFiles(ctx, deps.Prompts, *currentBranch, eng, out, false, direction)
		if err != nil {
			return err
		}
		if len(pathspecs) == 0 {
			return errors.New("no files selected")
		}

		// Get the original commit message from the first commit on this branch
		// GetAllCommits returns newest to oldest, so the first commit is the last element
		var originalCommitMessage string
		commitMessagesData, err := currentBranch.AllCommits(ctx)
		commitMessages := commitMessagesData.Messages()
		if err == nil && len(commitMessages) > 0 {
			// Use the first (oldest) commit's message
			originalCommitMessage = commitMessages[len(commitMessages)-1]
		}

		// Prompt for commit message with context (shows files being extracted)
		handler.OnStep(StepCommitMessage, handlerBase.StatusStarted, "Enter commit message")
		commitMsgCtx := CommitMessageContext{
			Files:                 pathspecs,
			Direction:             direction,
			CurrentBranch:         currentBranch.Name(),
			OriginalCommitMessage: originalCommitMessage,
		}
		commitMessage, err := handler.PromptCommitMessageWithContext(commitMsgCtx)
		if err != nil {
			return err
		}
		handler.OnStep(StepCommitMessage, handlerBase.StatusCompleted, "Commit message set")

		// Generate default branch name from commit message using the branch pattern
		var defaultBranchName string
		if opts.BranchPattern != "" {
			defaultBranchName, err = opts.BranchPattern.GetBranchName(ctx, deps.Engine, commitMessage, "")
		}
		if opts.BranchPattern == "" || err != nil {
			// Fallback to simpler default if pattern fails
			defaultBranchName = currentBranch.Name() + splitSuffix
		}

		// Prompt for branch name
		handler.OnStep(StepBranchName, handlerBase.StatusStarted, "Enter branch name")
		branchName, err := handler.PromptBranchName(defaultBranchName, []string{}, eng.BranchNames(), currentBranch.Name())
		if err != nil {
			return err
		}
		handler.OnStep(StepBranchName, handlerBase.StatusCompleted, branchName)

		// Execute the file split with the provided name and message
		fileResult, err := splitByFile(ctx, *currentBranch, pathspecs, eng, splitByFileOptions{
			AsSibling: false,
			Direction: direction,
			Name:      branchName,
			Message:   commitMessage,
		})
		if err != nil {
			return err
		}

		// Restack upstack branches if needed (not needed for --above since splitByFileAbove handles it)
		if direction != DirectionAbove {
			rng := engine.StackRange{
				RecursiveParents:  false,
				IncludeCurrent:    false,
				RecursiveChildren: true,
			}
			graph := eng.Graph(ctx, engine.SortStrategyAlphabetical)
			upstackBranches := graph.Range(*currentBranch, rng)
			if len(upstackBranches) > 0 {
				if err := deps.Restack.Restack(ctx, upstackBranches); err != nil {
					return fmt.Errorf("failed to restack upstack branches: %w", err)
				}
			}
		}

		handler.Complete(ActionResult{
			OriginalBranch: currentBranch.Name(),
			NewBranches:    fileResult.BranchNames,
			Style:          style,
		})
		return nil

	case StyleCommit:
		// Commit split doesn't use direction in the same way
		return fmt.Errorf("style %s is not yet implemented in wizard mode", style)

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
