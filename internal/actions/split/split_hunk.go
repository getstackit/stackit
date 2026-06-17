package split

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/getstackit/stackit/internal/actions"
	handlerBase "github.com/getstackit/stackit/internal/actions/handler"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	sterrors "github.com/getstackit/stackit/internal/errors"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/output"
)

// hunkOptions contains options for hunk-based splitting
type hunkOptions struct {
	useGitAddP bool   // Use git add -p instead of TUI selector
	patchFile  string // Path to patch file for non-interactive mode (use "-" for stdin)
	name       string // Branch name for the new branch
	message    string // Commit message for the new branch
}

// splitByHunkEngine is a minimal interface needed for splitting by hunk
type splitByHunkEngine interface {
	engine.BranchReader
	engine.BranchWriter
	engine.PRManager
	engine.StackRewriter
}

// splitByHunkWithHandler splits a branch by interactively staging hunks using an InteractiveHandler.
// This is the new wizard-based implementation that supports direction selection.
//
// For DirectionBelow (downstack) - current behavior:
//  1. Detach HEAD and soft reset to parent's tip
//  2. All changes become unstaged
//  3. User stages hunks for new branch
//  4. New branch created between parent and current
//
// For DirectionAbove (upstack) - new behavior:
//  1. Stay on current branch commit
//  2. User stages hunks to EXTRACT (these will be removed from current)
//  3. Create child branch with staged changes
//  4. Remove staged changes from current branch
//  5. Existing children of current become children of new branch
//
// When opts.useGitAddP is true, uses git add -p instead of the TUI hunk selector.
// When opts.patchFile is set, uses the patch file instead of interactive selection.
func splitByHunkWithHandler(ctx *app.Context, branchToSplit engine.Branch, eng splitByHunkEngine, splog output.Output, handler InteractiveHandler, direction Direction, opts hunkOptions) error {
	if direction == DirectionAbove {
		return splitByHunkAbove(ctx, branchToSplit, eng, splog, handler, opts)
	}

	// Non-interactive patch mode for --below direction
	if opts.patchFile != "" {
		return splitByHunkBelowWithPatch(ctx, branchToSplit, eng, splog, opts)
	}

	gitCtx := ctx.Context

	// Detach and reset branch changes
	if err := eng.DetachAndResetBranchChanges(gitCtx, branchToSplit.GetName()); err != nil {
		return fmt.Errorf("failed to detach and reset: %w", err)
	}

	// On any error, restore the original branch to avoid leaving the user in detached HEAD.
	success := false
	defer func() {
		if !success {
			_ = eng.ForceCheckoutBranch(gitCtx, branchToSplit)
		}
	}()

	branchNames := []string{}

	// Get default commit message
	commitMessages, err := branchToSplit.GetAllCommits(engine.CommitFormatMessage)
	if err != nil {
		return fmt.Errorf("failed to get commit messages: %w", err)
	}
	defaultCommitMessage := strings.Join(commitMessages, "\n\n")

	// Show instructions
	splog.Info("Splitting %s into multiple single-commit branches.", output.Branch(branchToSplit.GetName(), true))
	branch := eng.GetBranch(branchToSplit.GetName())
	prInfo, _ := branch.GetPrInfo()
	if prInfo != nil && prInfo.Number() != nil {
		splog.Info("If any of the new branches keeps the name %s, it will be linked to PR #%d.",
			output.Branch(branchToSplit.GetName(), true), *prInfo.Number())
	}
	splog.Info("")
	splog.Info("For each branch you'd like to create:")
	splog.Info("1. Follow the prompts to stage the changes that you'd like to include.")
	splog.Info("2. Enter a commit message.")
	splog.Info("3. Pick a branch name.")
	splog.Info("The command will continue until all changes have been added to a new branch.")
	splog.Info("")

	// Get existing branch names for validation
	existingBranchNames := eng.BranchNames()

	// Loop while there are unstaged changes
	for {
		hasUnstaged, err := eng.HasUnstagedChanges(gitCtx)
		if err != nil {
			return fmt.Errorf("failed to check unstaged changes: %w", err)
		}
		if !hasUnstaged {
			break
		}

		// Show remaining changes via handler
		handler.OnStep(StepStagingHunks, handlerBase.StatusStarted, fmt.Sprintf("Stage changes for branch %d", len(branchNames)+1))

		unstagedDiff, err := eng.GetUnstagedDiff(gitCtx)
		if err != nil {
			return fmt.Errorf("failed to get unstaged diff: %w", err)
		}

		// Parse the diff into hunks
		hunks, err := git.ParseDiffOutput(unstagedDiff)
		if err != nil {
			return fmt.Errorf("failed to parse diff: %w", err)
		}

		// Also get hunks for untracked (new) files
		untrackedHunks, err := eng.GetUntrackedFileHunks(gitCtx)
		if err != nil {
			return fmt.Errorf("failed to get untracked file hunks: %w", err)
		}
		hunks = append(hunks, untrackedHunks...)

		if len(hunks) == 0 {
			// No hunks to stage - break out of loop
			break
		}

		// Stage hunks using either git add -p or the TUI selector
		if opts.useGitAddP {
			// Use git's built-in interactive staging
			if err := eng.StagePatch(gitCtx); err != nil {
				return sterrors.ErrCanceled
			}
		} else {
			// Use the TUI hunk selector
			selectedHunks, err := handler.PromptSelectHunks(hunks)
			if err != nil {
				return sterrors.ErrCanceled
			}

			// Stage the selected hunks
			if len(selectedHunks) > 0 {
				if err := eng.StageHunks(gitCtx, selectedHunks); err != nil {
					return fmt.Errorf("failed to stage hunks: %w", err)
				}
			}
		}

		// Check if anything was staged
		hasStaged, err := eng.HasStagedChanges(gitCtx)
		if err != nil {
			return fmt.Errorf("failed to check staged changes: %w", err)
		}
		if !hasStaged {
			// Nothing was staged - ask user if they want to continue or cancel via handler
			continueAgain, err := handler.PromptContinueOrCancel()
			if err != nil {
				return err
			}
			if !continueAgain {
				return sterrors.ErrCanceled
			}
			continue
		}

		handler.OnStep(StepStagingHunks, handlerBase.StatusCompleted, "Changes staged")

		// Prompt for branch name BEFORE creating commit (so we can validate first)
		handler.OnStep(StepBranchName, handlerBase.StatusStarted, "Enter branch name")

		defaultName := generateDefaultBranchName(branchToSplit.GetName(), branchNames)
		branchName, err := handler.PromptBranchName(defaultName, branchNames, existingBranchNames, branchToSplit.GetName())
		if err != nil {
			return err
		}

		handler.OnStep(StepBranchName, handlerBase.StatusCompleted, branchName)

		// Prompt for commit message via handler
		handler.OnStep(StepCommitMessage, handlerBase.StatusStarted, "Enter commit message")

		editMessage, err := handler.PromptEditCommitMessage()
		if err != nil {
			return err
		}

		commitMessage := defaultCommitMessage
		if editMessage {
			commitMessage, err = handler.PromptCommitMessage(defaultCommitMessage)
			if err != nil {
				return err
			}
		}

		handler.OnStep(StepCommitMessage, handlerBase.StatusCompleted, "Commit message set")

		// Create commit (after all validation passed)
		if err := eng.CommitWithOptions(gitCtx, git.CommitOptions{
			Message:  commitMessage,
			NoVerify: true, // Split hunk commits are internal, hooks usually shouldn't run
		}); err != nil {
			return fmt.Errorf("failed to create commit: %w", err)
		}

		// Track the new branch name so it's not reused (sessionNames passed to PromptBranchName)
		branchNames = append(branchNames, branchName)
		handler.OnBranchCreated(branchName)
	}

	// Update branchToSplit to point to the last commit we created
	if err := eng.UpdateBranchRef(gitCtx, branchToSplit.GetName(), "HEAD"); err != nil {
		return fmt.Errorf("failed to update branch reference: %w", err)
	}

	// Apply the split
	result := &Result{
		BranchNames:  branchNames,
		BranchPoints: makeRange(len(branchNames)),
	}

	// Get upstack branches (children)
	upstackRng := engine.StackRange{
		RecursiveParents:  false,
		IncludeCurrent:    false,
		RecursiveChildren: true,
	}
	upstackGraph := eng.Graph(engine.SortStrategyAlphabetical)
	upstackBranches := upstackGraph.Range(branchToSplit, upstackRng)

	// Apply the split to commits
	if err := eng.ApplySplitToCommits(gitCtx, engine.ApplySplitOptions{
		BranchToSplit: branchToSplit.GetName(),
		BranchNames:   result.BranchNames,
		BranchPoints:  result.BranchPoints,
		AsSibling:     false,
	}); err != nil {
		return fmt.Errorf("failed to apply split: %w", err)
	}

	// Restack upstack branches
	if len(upstackBranches) > 0 {
		if err := actions.RestackBranches(ctx, upstackBranches); err != nil {
			return fmt.Errorf("failed to restack upstack branches: %w", err)
		}
	}

	handler.Complete(ActionResult{
		OriginalBranch: branchToSplit.GetName(),
		NewBranches:    result.BranchNames,
		Style:          StyleHunk,
	})

	success = true
	return nil
}

// generateDefaultBranchName generates a default branch name for a new split branch.
// It returns "{originalName}_split", or "{originalName}_split_N" if that's already taken.
func generateDefaultBranchName(originalName string, existingNames []string) string {
	// First try the simple suffix
	candidate := originalName + "_split"
	if !slices.Contains(existingNames, candidate) {
		return candidate
	}

	// If that's taken, try numbered suffixes
	for suffix := 2; suffix <= 1000; suffix++ {
		candidate = fmt.Sprintf("%s_split_%d", originalName, suffix)
		if !slices.Contains(existingNames, candidate) {
			return candidate
		}
	}

	// Fallback (should never happen in practice)
	return fmt.Sprintf("%s_split_%d", originalName, len(existingNames)+1)
}

// splitByHunkBelowWithPatch splits a branch by creating a new parent branch with hunks from a patch file.
// This is the non-interactive version for --below direction.
//
// Algorithm:
//  1. Get the original parent of branchToSplit
//  2. Detach HEAD and soft reset to parent's tip (all changes unstaged)
//  3. Read and parse hunks from patch file
//  4. Stage hunks from patch (these go to new parent branch)
//  5. Stash staged changes
//  6. Stage and commit remaining changes (these stay on branchToSplit)
//  7. Update branchToSplit ref
//  8. Reset to original parent, pop stash, commit (new parent content)
//  9. Create new parent branch at HEAD
//  10. Track new parent branch with grandparent as its parent
//  11. Update branchToSplit to have new parent as its parent
//  12. Restack branchToSplit onto new parent
func splitByHunkBelowWithPatch(ctx *app.Context, branchToSplit engine.Branch, eng splitByHunkEngine, splog output.Output, opts hunkOptions) error {
	gitCtx := ctx.Context

	// Get the original parent before we modify anything
	originalParent := branchToSplit.GetParent()
	if originalParent == nil {
		return fmt.Errorf("cannot split branch %s: it has no parent", branchToSplit.GetName())
	}

	// Get default commit message
	commitMessages, err := branchToSplit.GetAllCommits(engine.CommitFormatMessage)
	if err != nil {
		return fmt.Errorf("failed to get commit messages: %w", err)
	}
	defaultCommitMessage := strings.Join(commitMessages, "\n\n")

	// Get existing branch names for validation
	branchSet := eng.BranchNames()

	// Determine new parent branch name
	newParentName := opts.name
	if newParentName == "" {
		newParentName = generateDefaultBranchName(branchToSplit.GetName(), branchSet.Names())
	}
	// Validate branch name doesn't already exist
	if branchSet.Contains(newParentName) {
		return fmt.Errorf("branch %q already exists", newParentName)
	}

	// Determine commit message for new parent
	newParentMessage := opts.message
	if newParentMessage == "" {
		newParentMessage = defaultCommitMessage
	}

	// Detach and reset branch changes (all changes become unstaged)
	if err := eng.DetachAndResetBranchChanges(gitCtx, branchToSplit.GetName()); err != nil {
		return fmt.Errorf("failed to detach and reset: %w", err)
	}

	// On any error, restore the original branch to avoid leaving the user in detached HEAD.
	success := false
	defer func() {
		if !success {
			_ = eng.ForceCheckoutBranch(gitCtx, branchToSplit)
		}
	}()

	// Read and parse patch file
	patchContent, err := readPatchFile(opts.patchFile)
	if err != nil {
		return fmt.Errorf("failed to read patch file %q: %w", opts.patchFile, err)
	}

	patchHunks, err := git.ParseDiffOutput(patchContent)
	if err != nil {
		return fmt.Errorf("failed to parse patch file %q: %w", opts.patchFile, err)
	}

	if len(patchHunks) == 0 {
		return fmt.Errorf("patch file %q contains no hunks", opts.patchFile)
	}

	// Stage the hunks from the patch (these will go to the new parent branch)
	if err := eng.StageHunks(gitCtx, patchHunks); err != nil {
		return fmt.Errorf("failed to stage hunks from patch file %q: %w", opts.patchFile, err)
	}

	// Check if anything was staged
	hasStaged, err := eng.HasStagedChanges(gitCtx)
	if err != nil {
		return fmt.Errorf("failed to check staged changes: %w", err)
	}
	if !hasStaged {
		return fmt.Errorf("no changes staged from patch file %q", opts.patchFile)
	}

	// Check if there are unstaged changes or untracked files (to keep on branchToSplit)
	hasUnstaged, err := eng.HasUnstagedChanges(gitCtx)
	if err != nil {
		return fmt.Errorf("failed to check unstaged changes: %w", err)
	}
	hasUntracked, err := eng.HasUntrackedFiles(gitCtx)
	if err != nil {
		return fmt.Errorf("failed to check untracked files: %w", err)
	}
	if !hasUnstaged && !hasUntracked {
		return fmt.Errorf("all changes were staged from patch - nothing would remain on %s", branchToSplit.GetName())
	}

	// Stash the staged changes (these will become the new parent branch content)
	stashName := fmt.Sprintf("stackit-split-below-parent-%d", time.Now().UnixNano())
	_, err = eng.StashPushStaged(gitCtx, stashName)
	if err != nil {
		return fmt.Errorf("failed to stash staged changes: %w", err)
	}

	// Track stash state for cleanup
	stashPopped := false
	cleanupStash := func() {
		if !stashPopped {
			_ = eng.StashPop(gitCtx)
			stashPopped = true
		}
	}

	// Stage and commit remaining changes - these stay on branchToSplit
	if err := eng.StageAll(gitCtx); err != nil {
		cleanupStash()
		return fmt.Errorf("failed to stage remaining changes: %w", err)
	}

	if err := eng.CommitWithOptions(gitCtx, git.CommitOptions{
		Message:  defaultCommitMessage,
		NoVerify: true,
	}); err != nil {
		cleanupStash()
		return fmt.Errorf("failed to commit remaining changes: %w", err)
	}

	// Update branchToSplit to point to this commit (contains remaining changes)
	if err := eng.UpdateBranchRef(gitCtx, branchToSplit.GetName(), "HEAD"); err != nil {
		cleanupStash()
		return fmt.Errorf("failed to update branch reference: %w", err)
	}

	// Reset to original parent to create the new parent branch
	if err := eng.ResetHard(gitCtx, originalParent.GetName()); err != nil {
		cleanupStash()
		return fmt.Errorf("failed to reset to original parent: %w", err)
	}

	// Pop the stash to get the parent branch changes
	if err := eng.StashPop(gitCtx); err != nil {
		return fmt.Errorf("failed to pop stash: %w. Recovery: run 'git stash pop' to restore changes", err)
	}
	stashPopped = true

	// Stage and commit - this becomes the NEW PARENT branch content
	if err := eng.StageAll(gitCtx); err != nil {
		return fmt.Errorf("failed to stage parent branch changes: %w", err)
	}

	if err := eng.CommitWithOptions(gitCtx, git.CommitOptions{
		Message:  newParentMessage,
		NoVerify: true,
	}); err != nil {
		return fmt.Errorf("failed to commit parent branch changes: %w", err)
	}

	// Create the new parent branch at HEAD (contains patch hunks only)
	if err := eng.CreateBranch(gitCtx, newParentName, "HEAD"); err != nil {
		return fmt.Errorf("failed to create parent branch: %w", err)
	}

	// Track the new parent branch with originalParent as its parent
	newParentBranch := eng.GetBranch(newParentName)
	if err := eng.TrackBranch(gitCtx, newParentName, originalParent.GetName()); err != nil {
		return fmt.Errorf("failed to track parent branch: %w", err)
	}

	// Update branchToSplit to have newParentBranch as its parent
	if err := eng.SetParent(gitCtx, branchToSplit, newParentBranch, engine.DivergenceRecompute); err != nil {
		return fmt.Errorf("failed to update parent of %s: %w", branchToSplit.GetName(), err)
	}

	// Restack branchToSplit onto the new parent
	if err := actions.RestackBranches(ctx, engine.BranchesOf(branchToSplit)); err != nil {
		return fmt.Errorf("failed to restack %s: %w", branchToSplit.GetName(), err)
	}

	// Checkout branchToSplit (we end up on the original branch)
	if err := eng.CheckoutBranch(gitCtx, branchToSplit); err != nil {
		return fmt.Errorf("failed to checkout original branch: %w", err)
	}

	splog.Info("Created branch %s as parent of %s", output.Branch(newParentName, true), output.Branch(branchToSplit.GetName(), true))

	success = true
	return nil
}

// splitByHunkAbove splits a branch by creating a new child branch with extracted changes.
//
// Algorithm:
//  1. Detach HEAD and soft reset to parent's tip (all changes unstaged)
//  2. User stages hunks to EXTRACT (for new child branch)
//  3. Stash only the staged changes (preserving unstaged "keep" changes)
//  4. Commit unstaged changes → this becomes new current branch content
//  5. Update current branch ref
//  6. Create child branch from current
//  7. Pop stash and commit → child branch content
//  8. Re-parent existing children to the new child
//
// When opts.useGitAddP is true, uses git add -p instead of the TUI hunk selector.
// When opts.patchFile is set, uses the patch file instead of interactive selection.
func splitByHunkAbove(ctx *app.Context, branchToSplit engine.Branch, eng splitByHunkEngine, splog output.Output, handler InteractiveHandler, opts hunkOptions) error {
	gitCtx := ctx.Context

	// Get existing children before we modify anything
	graph := eng.Graph(engine.SortStrategyAlphabetical)
	existingChildren := graph.Children(branchToSplit)

	// Detach and reset branch changes (all changes become unstaged)
	if err := eng.DetachAndResetBranchChanges(gitCtx, branchToSplit.GetName()); err != nil {
		return fmt.Errorf("failed to detach and reset: %w", err)
	}

	// On any error, restore the original branch to avoid leaving the user in detached HEAD.
	success := false
	defer func() {
		if !success {
			_ = eng.ForceCheckoutBranch(gitCtx, branchToSplit)
		}
	}()

	// Get default commit message
	commitMessages, err := branchToSplit.GetAllCommits(engine.CommitFormatMessage)
	if err != nil {
		return fmt.Errorf("failed to get commit messages: %w", err)
	}
	defaultCommitMessage := strings.Join(commitMessages, "\n\n")

	// Get existing branch names for validation
	existingBranches := eng.BranchNames()

	// Show instructions (only for interactive mode)
	if handler != nil {
		splog.Info("Splitting %s - extracting changes to a new child branch.", output.Branch(branchToSplit.GetName(), true))
		splog.Info("")
		splog.Info("Stage the changes you want to EXTRACT to the new child branch.")
		splog.Info("The remaining changes will stay on %s.", output.Branch(branchToSplit.GetName(), true))
		splog.Info("")
		handler.OnStep(StepStagingHunks, handlerBase.StatusStarted, "Stage changes to extract")
	}

	unstagedDiff, err := eng.GetUnstagedDiff(gitCtx)
	if err != nil {
		return fmt.Errorf("failed to get unstaged diff: %w", err)
	}

	// Parse the diff into hunks
	hunks, err := git.ParseDiffOutput(unstagedDiff)
	if err != nil {
		return fmt.Errorf("failed to parse diff: %w", err)
	}

	// Also get hunks for untracked (new) files
	untrackedHunks, err := eng.GetUntrackedFileHunks(gitCtx)
	if err != nil {
		return fmt.Errorf("failed to get untracked file hunks: %w", err)
	}
	hunks = append(hunks, untrackedHunks...)

	if len(hunks) == 0 {
		return fmt.Errorf("no changes to extract")
	}

	// Stage hunks using: patch file, git add -p, or the TUI selector
	switch {
	case opts.patchFile != "":
		// Non-interactive mode: read hunks from patch file
		patchContent, err := readPatchFile(opts.patchFile)
		if err != nil {
			return fmt.Errorf("failed to read patch file %q: %w", opts.patchFile, err)
		}

		patchHunks, err := git.ParseDiffOutput(patchContent)
		if err != nil {
			return fmt.Errorf("failed to parse patch file %q: %w", opts.patchFile, err)
		}

		if len(patchHunks) == 0 {
			return fmt.Errorf("patch file %q contains no hunks", opts.patchFile)
		}

		// Stage the hunks from the patch
		if err := eng.StageHunks(gitCtx, patchHunks); err != nil {
			return fmt.Errorf("failed to stage hunks from patch file %q: %w", opts.patchFile, err)
		}

	case opts.useGitAddP:
		// Use git's built-in interactive staging
		if err := eng.StagePatch(gitCtx); err != nil {
			return sterrors.ErrCanceled
		}

	default:
		// Use the TUI hunk selector (user selects what to EXTRACT)
		selectedHunks, err := handler.PromptSelectHunks(hunks)
		if err != nil {
			return sterrors.ErrCanceled
		}

		// Stage the selected hunks
		if len(selectedHunks) > 0 {
			if err := eng.StageHunks(gitCtx, selectedHunks); err != nil {
				return fmt.Errorf("failed to stage hunks: %w", err)
			}
		}
	}

	// Check if anything was staged
	hasStaged, err := eng.HasStagedChanges(gitCtx)
	if err != nil {
		return fmt.Errorf("failed to check staged changes: %w", err)
	}
	if !hasStaged {
		return fmt.Errorf("no changes staged to extract")
	}

	// Check if there are unstaged changes or untracked files (to keep on current)
	hasUnstaged, err := eng.HasUnstagedChanges(gitCtx)
	if err != nil {
		return fmt.Errorf("failed to check unstaged changes: %w", err)
	}
	hasUntracked, err := eng.HasUntrackedFiles(gitCtx)
	if err != nil {
		return fmt.Errorf("failed to check untracked files: %w", err)
	}
	if !hasUnstaged && !hasUntracked {
		return fmt.Errorf("all changes were staged - nothing would remain on %s", branchToSplit.GetName())
	}

	if handler != nil {
		handler.OnStep(StepStagingHunks, handlerBase.StatusCompleted, "Changes staged for extraction")
	}

	// Determine child branch name and commit message
	var childBranchName string
	var childCommitMessage string

	if opts.patchFile != "" {
		// Non-interactive mode: use provided name/message or generate defaults
		childBranchName = opts.name
		if childBranchName == "" {
			childBranchName = generateDefaultBranchName(branchToSplit.GetName(), existingBranches.Names())
		}
		// Validate branch name doesn't already exist
		if existingBranches.Contains(childBranchName) {
			return fmt.Errorf("branch %q already exists", childBranchName)
		}

		childCommitMessage = opts.message
		if childCommitMessage == "" {
			childCommitMessage = defaultCommitMessage
		}
	} else {
		// Interactive mode: prompt for branch name and commit message
		handler.OnStep(StepBranchName, handlerBase.StatusStarted, "Enter child branch name")

		defaultName := generateDefaultBranchName(branchToSplit.GetName(), existingBranches.Names())
		var err error
		childBranchName, err = handler.PromptBranchName(defaultName, []string{}, existingBranches, branchToSplit.GetName())
		if err != nil {
			return err
		}

		handler.OnStep(StepBranchName, handlerBase.StatusCompleted, childBranchName)

		// Prompt for commit message for the child branch
		handler.OnStep(StepCommitMessage, handlerBase.StatusStarted, "Enter commit message for extracted changes")

		editMessage, err := handler.PromptEditCommitMessage()
		if err != nil {
			return err
		}

		childCommitMessage = defaultCommitMessage
		if editMessage {
			childCommitMessage, err = handler.PromptCommitMessage(defaultCommitMessage)
			if err != nil {
				return err
			}
		}

		handler.OnStep(StepCommitMessage, handlerBase.StatusCompleted, "Commit message set")
	}

	// Stash only the staged changes (what we want to extract to child)
	// Use a unique stash name with timestamp to prevent collision with previous operations
	stashName := fmt.Sprintf("stackit-split-above-extract-%d", time.Now().UnixNano())
	_, err = eng.StashPushStaged(gitCtx, stashName)
	if err != nil {
		return fmt.Errorf("failed to stash staged changes: %w", err)
	}

	// Track stash state for cleanup - once stash is pushed, we need to pop it on any error
	stashPopped := false
	cleanupStash := func() {
		if !stashPopped {
			_ = eng.StashPop(gitCtx)
			stashPopped = true
		}
	}

	// Now only the "keep" changes remain unstaged
	// Stage and commit them as the new current branch content
	if err := eng.StageAll(gitCtx); err != nil {
		cleanupStash()
		return fmt.Errorf("failed to stage remaining changes: %w", err)
	}

	if err := eng.CommitWithOptions(gitCtx, git.CommitOptions{
		Message:  defaultCommitMessage,
		NoVerify: true,
	}); err != nil {
		cleanupStash()
		return fmt.Errorf("failed to commit remaining changes: %w", err)
	}

	// Update the original branch ref to point to this new commit (the "keep" content)
	if err := eng.UpdateBranchRef(gitCtx, branchToSplit.GetName(), "HEAD"); err != nil {
		cleanupStash()
		return fmt.Errorf("failed to update branch reference: %w", err)
	}

	// Checkout the updated branch
	if err := eng.CheckoutBranch(gitCtx, branchToSplit); err != nil {
		cleanupStash()
		return fmt.Errorf("failed to checkout branch: %w", err)
	}

	// Create the child branch at the current position
	if err := eng.CreateBranch(gitCtx, childBranchName, "HEAD"); err != nil {
		cleanupStash()
		return fmt.Errorf("failed to create child branch: %w", err)
	}

	// Checkout the child branch
	childBranch := eng.GetBranch(childBranchName)
	if err := eng.CheckoutBranch(gitCtx, childBranch); err != nil {
		cleanupStash()
		return fmt.Errorf("failed to checkout child branch: %w", err)
	}

	// Pop the stash to get the extract changes back
	if err := eng.StashPop(gitCtx); err != nil {
		// Stash pop failed - this leaves the repo in a partially completed state.
		// The original branch has been updated and the child branch exists but has no commit.
		// Provide guidance on how to recover.
		return fmt.Errorf("failed to pop stash: %w. Recovery: run 'git stash pop' to restore changes, then 'git add -A && git commit' on branch %s", err, childBranchName)
	}
	stashPopped = true

	// Stage and commit the extracted changes on child branch
	if err := eng.StageAll(gitCtx); err != nil {
		// Changes are in working directory but not staged.
		// User can recover by staging and committing manually.
		return fmt.Errorf("failed to stage extracted changes: %w. Recovery: run 'git add -A && git commit' to complete", err)
	}

	if err := eng.CommitWithOptions(gitCtx, git.CommitOptions{
		Message:  childCommitMessage,
		NoVerify: true,
	}); err != nil {
		// Changes are staged but commit failed.
		// User can recover by committing manually.
		return fmt.Errorf("failed to commit extracted changes: %w. Recovery: run 'git commit' to complete", err)
	}

	// Track the child branch with parent = branchToSplit
	if err := eng.TrackBranch(gitCtx, childBranchName, branchToSplit.GetName()); err != nil {
		return fmt.Errorf("failed to track child branch: %w", err)
	}

	if handler != nil {
		handler.OnBranchCreated(childBranchName)
	}

	// Re-parent existing children to the new child branch, preserving divergence
	// points so children don't carry the split-out changes.
	if err := eng.ReparentBranches(gitCtx, existingChildren, childBranch); err != nil {
		return fmt.Errorf("failed to reparent children: %w", err)
	}

	// Restack the children that were reparented
	if len(existingChildren) > 0 {
		childBranches := engine.BranchesFromNames(eng, existingChildren)
		if err := actions.RestackBranches(ctx, childBranches); err != nil {
			return fmt.Errorf("failed to restack children: %w", err)
		}
	}

	if handler != nil {
		handler.Complete(ActionResult{
			OriginalBranch: branchToSplit.GetName(),
			NewBranches:    []string{childBranchName},
			Style:          StyleHunk,
		})
	} else {
		// Non-interactive mode (patch file): print completion message
		splog.Info("Created branch %s as child of %s", output.Branch(childBranchName, true), output.Branch(branchToSplit.GetName(), true))
	}

	success = true
	return nil
}

// readPatchFile reads patch content from a file path or stdin.
// If path is "-", reads from stdin.
func readPatchFile(path string) (string, error) {
	if path == "-" {
		// Check if stdin is a terminal - if so, nothing is being piped
		fi, err := os.Stdin.Stat()
		if err != nil {
			return "", fmt.Errorf("failed to stat stdin: %w", err)
		}
		if (fi.Mode() & os.ModeCharDevice) != 0 {
			return "", fmt.Errorf("stdin is a terminal; pipe a patch file or use a file path instead of \"-\"")
		}

		content, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("failed to read from stdin: %w", err)
		}
		return string(content), nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(content), nil
}
