package split

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/getstackit/stackit/internal/actions"
	handlerBase "github.com/getstackit/stackit/internal/actions/handler"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/utils"
)

// splitByFileEngine is a minimal interface needed for splitting by file
type splitByFileEngine interface {
	engine.BranchReader
	engine.BranchWriter
	engine.StackRewriter
	splitStashEngine
}

// splitByFileOptions contains options for the splitByFile operation
type splitByFileOptions struct {
	// AsSibling creates the split branch as a sibling instead of a parent.
	// When true, the extracted files go to a new branch on the same parent,
	// and the original branch is unchanged (files are NOT removed).
	AsSibling bool
	// Direction specifies whether to create the split branch above (child) or below (parent).
	// Only applies when AsSibling is false. Default (empty) is below (parent).
	Direction Direction
	// Name specifies a custom name for the split branch.
	// If empty, defaults to "{original}_split".
	Name string
	// Message specifies a custom commit message for the extraction.
	// If empty, auto-generates: "Extract {files} from {branch}"
	Message string
	// DryRun shows what would happen without executing the split.
	DryRun bool
}

// splitByFile splits a branch by extracting CHANGES to specified files to a new branch.
// Unlike the legacy behavior, this extracts only the diff hunks for the specified files,
// not the complete file contents. This is the correct semantic for "split by file".
//
// Default behavior (AsSibling=false):
// Creates a new PARENT branch containing the changes to the extracted files.
// The original branch becomes a child of the split branch.
// Algorithm:
//  1. Get the diff between parent and branch, parse into hunks
//  2. Filter hunks to only those for specified files
//  3. Detach HEAD and soft reset to parent (all changes become unstaged)
//  4. Stage the filtered hunks (changes go to new parent branch)
//  5. Stash staged changes
//  6. Commit remaining changes (stay on original branch)
//  7. Pop stash, commit on new parent branch
//  8. Update parent relationship
//
// Sibling mode (AsSibling=true):
// Creates a new SIBLING branch containing the changes to the extracted files.
// The original branch is unchanged (changes are NOT removed).
//
// Stack ID preservation:
// All new branches created by split inherit the original branch's stack ID,
// ensuring they remain part of the same logical stack.
func splitByFile(ctx context.Context, branchToSplit engine.Branch, pathspecs []string, eng splitByFileEngine, opts splitByFileOptions) (_ *Result, err error) {
	// Capture original stack ID to preserve on new branches
	originalStackID := eng.GetStackID(branchToSplit)
	// Get parent branch
	parentBranchName := branchToSplit.GetParentOrTrunk()
	parentBranch := eng.GetBranch(parentBranchName)

	// Generate new branch name
	newBranchName := opts.Name
	if newBranchName == "" {
		newBranchName = branchToSplit.GetName() + splitSuffix
	}
	allBranches := eng.AllBranches()
	branchNames := allBranches.Names()
	// Ensure unique name (only if we're auto-generating)
	if opts.Name == "" {
		for slices.Contains(branchNames, newBranchName) {
			newBranchName += splitSuffix
		}
	} else if slices.Contains(branchNames, newBranchName) {
		return nil, fmt.Errorf("branch %s already exists", newBranchName)
	}

	// Get the diff between parent and branchToSplit (raw output for parsing)
	diffOutput, err := eng.GetDiffBetween(ctx, git.RevRange{Base: parentBranchName, Head: branchToSplit.GetName()})
	if err != nil {
		return nil, fmt.Errorf("failed to get diff: %w", err)
	}

	// Parse diff into hunks
	allHunks, err := git.ParseDiffOutput(diffOutput)
	if err != nil {
		return nil, fmt.Errorf("failed to parse diff: %w", err)
	}

	// Filter hunks to only those for specified files
	filteredHunks := filterHunksByFiles(allHunks, pathspecs)
	if len(filteredHunks) == 0 {
		return nil, fmt.Errorf("no changes found for files: %s", strings.Join(pathspecs, ", "))
	}

	// Check for binary files and reject them
	var binaryFiles []string
	for _, h := range filteredHunks {
		if h.Binary {
			binaryFiles = append(binaryFiles, h.File)
		}
	}
	if len(binaryFiles) > 0 {
		return nil, fmt.Errorf("cannot split binary files by hunk: %s. Exclude these files from the split, or use git commands directly to handle binary files",
			strings.Join(binaryFiles, ", "))
	}

	// Get commit message
	commitMessagesData, err := branchToSplit.GetAllCommits()
	commitMessages := commitMessagesData.Messages()
	if err != nil {
		return nil, fmt.Errorf("failed to get commit messages: %w", err)
	}
	defaultCommitMessage := strings.Join(commitMessages, "\n\n")
	if defaultCommitMessage == "" {
		defaultCommitMessage = fmt.Sprintf("Split from %s", branchToSplit.GetName())
	}

	commitMessage := opts.Message
	if commitMessage == "" {
		commitMessage = defaultCommitMessage
	}

	// For sibling mode, use simpler approach - create branch at parent and apply hunks directly
	if opts.AsSibling {
		return splitByFileSibling(ctx, branchToSplit, parentBranch, newBranchName, filteredHunks, commitMessage, eng, originalStackID, opts.DryRun)
	}

	// For above mode (upstack), extract to child branch
	if opts.Direction == DirectionAbove {
		return splitByFileAbove(ctx, branchToSplit, newBranchName, filteredHunks, defaultCommitMessage, commitMessage, eng, originalStackID, opts.DryRun)
	}

	// Dry-run mode: show what would happen without executing
	if opts.DryRun {
		return &Result{
			BranchNames:  []string{newBranchName},
			BranchPoints: []int{0},
		}, nil
	}

	// Default mode: extract to parent branch (below)

	// Detach and reset branch changes (all changes become unstaged).
	// On any error, the guard restores the original branch (and its ref, once
	// moved) to avoid leaving the user in detached HEAD.
	guard, err := actions.DetachForRewrite(ctx, eng, branchToSplit)
	if err != nil {
		return nil, err
	}
	defer guard.RestoreUnlessReleased(ctx, &err)

	// Stage the filtered hunks (these will go to the new parent branch)
	if err := eng.StageHunks(ctx, filteredHunks); err != nil {
		return nil, fmt.Errorf("failed to stage hunks: %w", err)
	}

	// Check if anything was staged
	status, err := eng.GetWorkingTreeStatus(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to read working tree status: %w", err)
	}
	if !status.Staged {
		return nil, fmt.Errorf("no changes staged for files: %s", strings.Join(pathspecs, ", "))
	}

	// Check if there are remaining changes (to keep on branchToSplit)

	if !status.HasUnstagedChanges() {
		return nil, fmt.Errorf("all changes were selected - nothing would remain on %s", branchToSplit.GetName())
	}

	// Stash the staged changes (these will become the new parent branch content)
	stashName := fmt.Sprintf("stackit-split-file-parent-%d", time.Now().UnixNano())
	_, err = eng.StashPushStaged(ctx, stashName)
	if err != nil {
		return nil, fmt.Errorf("failed to stash staged changes: %w", err)
	}
	stashRef, err := splitStashRef(ctx, eng, stashName)
	if err != nil {
		return nil, err
	}

	// Track stash state for cleanup on restore.
	// Note: the StashPop error is intentionally ignored to avoid masking the
	// original error. If StashPop fails, the stash remains in 'git stash list'
	// for manual recovery.
	stashPopped := false
	guard.OnRestore(func() {
		if !stashPopped {
			_ = eng.StashPopRef(ctx, stashRef)
			stashPopped = true
		}
	})

	// Stage and commit remaining changes - these stay on branchToSplit
	if err := eng.StageAll(ctx); err != nil {
		return nil, fmt.Errorf("failed to stage remaining changes: %w", err)
	}

	if err := eng.Commit(ctx, git.CommitOptions{
		Message:  defaultCommitMessage,
		NoVerify: true,
	}); err != nil {
		return nil, fmt.Errorf("failed to commit remaining changes: %w", err)
	}

	// Save original SHA before modifying branch ref (for recovery)
	originalSHA, err := eng.GetCommitSHA(branchToSplit.GetName(), 0)
	if err != nil {
		return nil, fmt.Errorf("failed to get original commit SHA: %w", err)
	}

	// Update branchToSplit to point to this commit (contains remaining changes)
	if err := eng.UpdateBranchRef(ctx, branchToSplit.GetName(), "HEAD"); err != nil {
		return nil, fmt.Errorf("failed to update branch reference: %w", err)
	}

	// From this point, recovery must restore the original branch ref
	guard.RestoreRefTo(originalSHA)

	// Reset to original parent to create the new parent branch
	if err := eng.ResetHard(ctx, parentBranchName); err != nil {
		return nil, fmt.Errorf("failed to reset to original parent: %w", err)
	}

	// Pop the stash to get the parent branch changes
	if err := eng.StashPopRef(ctx, stashRef); err != nil {
		// Leave the conflicted state in place for manual recovery.
		guard.Release()
		return nil, fmt.Errorf("failed to pop stash: %w. Recovery: check 'git stash list' for pending stash, resolve any conflicts manually", err)
	}
	stashPopped = true

	// Stage and commit - this becomes the NEW PARENT branch content
	if err := eng.StageAll(ctx); err != nil {
		// Changes are in the working tree after stash pop, not staged
		return nil, fmt.Errorf("failed to stage parent branch changes: %w; changes are in working tree", err)
	}

	if err := eng.Commit(ctx, git.CommitOptions{
		Message:  commitMessage,
		NoVerify: true,
	}); err != nil {
		return nil, fmt.Errorf("failed to commit parent branch changes: %w", err)
	}

	// Create the new parent branch at HEAD
	if err := eng.CreateBranch(ctx, newBranchName, "HEAD"); err != nil {
		return nil, fmt.Errorf("failed to create parent branch: %w", err)
	}

	// Track the new parent branch with originalParent as its parent
	newBranch := eng.GetBranch(newBranchName)
	if err := eng.TrackBranch(ctx, newBranchName, parentBranchName); err != nil {
		return nil, fmt.Errorf("failed to track parent branch: %w", err)
	}

	// Preserve stack ID from original branch
	// TrackBranch may generate a new ID if parent is trunk, but split branches
	// should stay in the same stack as the branch being split
	if originalStackID != "" {
		if err := eng.SetStackID(ctx, engine.BranchesOf(newBranch), originalStackID); err != nil {
			return nil, fmt.Errorf("failed to preserve stack ID: %w", err)
		}
	}

	// Update branchToSplit to have newBranch as its parent
	if err := eng.SetParent(ctx, branchToSplit, newBranch, engine.DivergenceRecompute); err != nil {
		return nil, fmt.Errorf("failed to update parent of %s: %w", branchToSplit.GetName(), err)
	}

	// The split is recorded; do not roll it back if the final checkout fails.
	guard.Release()

	// Checkout branchToSplit (we end up on the original branch)
	if err := eng.CheckoutBranch(ctx, branchToSplit); err != nil {
		return nil, fmt.Errorf("failed to checkout original branch: %w", err)
	}

	return &Result{
		BranchNames:  []string{newBranchName},
		BranchPoints: []int{0},
	}, nil
}

// splitByFileAbove extracts specified file changes to a new CHILD branch.
// The original branch keeps the remaining changes, and the new branch becomes a child.
// Existing children of the original branch are reparented to the new child.
//
// Algorithm:
//  1. Capture existing children before modifications
//  2. Detach HEAD and soft reset to parent (all changes unstaged)
//  3. Stage the filtered hunks (changes go to new child branch)
//  4. Stash staged changes
//  5. Commit remaining changes (stay on original branch)
//  6. Update original branch ref
//  7. Checkout original branch
//  8. Create child branch from current
//  9. Pop stash and commit (extracted files)
//  10. Track child branch with original as parent
//  11. Reparent existing children to new child
func splitByFileAbove(ctx context.Context, branchToSplit engine.Branch, newBranchName string, hunks []git.Hunk, defaultCommitMessage string, childCommitMessage string, eng splitByFileEngine, originalStackID string, dryRun bool) (_ *Result, err error) {
	// Dry-run mode: show what would happen without executing
	if dryRun {
		return &Result{
			BranchNames:  []string{newBranchName},
			BranchPoints: []int{0},
		}, nil
	}

	// Get existing children before we modify anything
	graph := eng.Graph(engine.SortStrategyAlphabetical)
	existingChildren := graph.Children(branchToSplit)

	// Detach and reset branch changes (all changes become unstaged).
	// On any error, the guard restores the original branch (and its ref, once
	// moved) to avoid leaving the user in detached HEAD.
	guard, err := actions.DetachForRewrite(ctx, eng, branchToSplit)
	if err != nil {
		return nil, err
	}
	defer guard.RestoreUnlessReleased(ctx, &err)

	// Stage the filtered hunks (these will go to the new child branch)
	if err := eng.StageHunks(ctx, hunks); err != nil {
		return nil, fmt.Errorf("failed to stage hunks: %w", err)
	}

	// Check if anything was staged
	status, err := eng.GetWorkingTreeStatus(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to read working tree status: %w", err)
	}
	if !status.Staged {
		return nil, fmt.Errorf("no changes staged for extraction")
	}

	// Check if there are remaining changes (to keep on branchToSplit)

	if !status.HasUnstagedChanges() {
		return nil, fmt.Errorf("all changes were selected - nothing would remain on %s", branchToSplit.GetName())
	}

	// Stash only the staged changes (what we want to extract to child)
	stashName := fmt.Sprintf("stackit-split-file-above-%d", time.Now().UnixNano())
	_, err = eng.StashPushStaged(ctx, stashName)
	if err != nil {
		return nil, fmt.Errorf("failed to stash staged changes: %w", err)
	}
	stashRef, err := splitStashRef(ctx, eng, stashName)
	if err != nil {
		return nil, err
	}

	// Track stash state for cleanup on restore.
	// Note: the StashPop error is intentionally ignored to avoid masking the
	// original error. If StashPop fails, the stash remains in 'git stash list'
	// for manual recovery.
	stashPopped := false
	guard.OnRestore(func() {
		if !stashPopped {
			_ = eng.StashPopRef(ctx, stashRef)
			stashPopped = true
		}
	})

	// Stage and commit remaining changes - these stay on branchToSplit
	if err := eng.StageAll(ctx); err != nil {
		return nil, fmt.Errorf("failed to stage remaining changes: %w", err)
	}

	if err := eng.Commit(ctx, git.CommitOptions{
		Message:  defaultCommitMessage,
		NoVerify: true,
	}); err != nil {
		return nil, fmt.Errorf("failed to commit remaining changes: %w", err)
	}

	// Save original SHA before modifying branch ref (for recovery)
	originalSHA, err := eng.GetCommitSHA(branchToSplit.GetName(), 0)
	if err != nil {
		return nil, fmt.Errorf("failed to get original commit SHA: %w", err)
	}

	// Update branchToSplit to point to this commit (the "keep" content)
	if err := eng.UpdateBranchRef(ctx, branchToSplit.GetName(), "HEAD"); err != nil {
		return nil, fmt.Errorf("failed to update branch reference: %w", err)
	}

	// From this point, recovery must restore the original branch ref
	guard.RestoreRefTo(originalSHA)

	// Checkout the updated branch
	if err := eng.CheckoutBranch(ctx, branchToSplit); err != nil {
		return nil, fmt.Errorf("failed to checkout branch: %w", err)
	}

	// Create the child branch at the current position
	if err := eng.CreateBranch(ctx, newBranchName, "HEAD"); err != nil {
		return nil, fmt.Errorf("failed to create child branch: %w", err)
	}

	// Delete the partially created child branch on restore
	cleanupChildBranch := func() {
		_ = eng.DeleteBranch(ctx, eng.GetBranch(newBranchName))
	}
	guard.OnRestore(cleanupChildBranch)

	// Checkout the child branch
	childBranch := eng.GetBranch(newBranchName)
	if err := eng.CheckoutBranch(ctx, childBranch); err != nil {
		return nil, fmt.Errorf("failed to checkout child branch: %w", err)
	}

	// Pop the stash to get the extract changes back
	if err := eng.StashPopRef(ctx, stashRef); err != nil {
		// Leave the stash for manual recovery rather than rolling back.
		guard.Release()
		cleanupChildBranch()
		_ = eng.CheckoutBranch(ctx, branchToSplit)
		return nil, fmt.Errorf("failed to pop stash: %w. Recovery: check 'git stash list' for pending stash, resolve any conflicts manually", err)
	}

	// The extracted changes are now in the child's working tree; from here,
	// failures leave that state in place with recovery instructions.
	guard.Release()

	// Stage and commit the extracted changes on child branch
	if err := eng.StageAll(ctx); err != nil {
		return nil, fmt.Errorf("failed to stage extracted changes: %w. Recovery: run 'git add -A && git commit' to complete", err)
	}

	if err := eng.Commit(ctx, git.CommitOptions{
		Message:  childCommitMessage,
		NoVerify: true,
	}); err != nil {
		return nil, fmt.Errorf("failed to commit extracted changes: %w. Recovery: run 'git commit' to complete", err)
	}

	// Track the child branch with parent = branchToSplit
	if err := eng.TrackBranch(ctx, newBranchName, branchToSplit.GetName()); err != nil {
		return nil, fmt.Errorf("failed to track child branch: %w", err)
	}

	// Preserve stack ID from original branch
	if originalStackID != "" {
		if err := eng.SetStackID(ctx, engine.BranchesOf(childBranch), originalStackID); err != nil {
			return nil, fmt.Errorf("failed to preserve stack ID: %w", err)
		}
	}

	// Re-parent existing children to the new child branch, preserving divergence
	// points so children don't carry the split-out changes.
	if err := eng.ReparentBranchesToParents(ctx, engine.MovesTo(existingChildren, childBranch.GetName()), engine.ReparentOpts{}); err != nil {
		return nil, fmt.Errorf("failed to reparent children: %w", err)
	}

	return &Result{
		BranchNames:  []string{newBranchName},
		BranchPoints: []int{0},
	}, nil
}

// splitByFileSibling creates a sibling branch with the specified file changes.
// The original branch is unchanged.
func splitByFileSibling(ctx context.Context, branchToSplit engine.Branch, parentBranch engine.Branch, newBranchName string, hunks []git.Hunk, commitMessage string, eng splitByFileEngine, originalStackID string, dryRun bool) (*Result, error) {
	// Dry-run mode: show what would happen without executing
	if dryRun {
		return &Result{
			BranchNames:  []string{newBranchName},
			BranchPoints: []int{0},
		}, nil
	}
	// First checkout the parent branch so the new branch starts from there
	if err := eng.CheckoutBranch(ctx, parentBranch); err != nil {
		return nil, fmt.Errorf("failed to checkout parent branch: %w", err)
	}

	// Create new branch from parent
	newBranch := eng.GetBranch(newBranchName)
	if err := eng.CreateAndCheckoutBranch(ctx, newBranch); err != nil {
		_ = eng.CheckoutBranch(ctx, branchToSplit)
		return nil, fmt.Errorf("failed to create branch: %w", err)
	}

	// On any subsequent failure, delete the partially-created branch and restore the original.
	var succeeded bool
	defer func() {
		if !succeeded {
			_ = eng.DeleteBranch(ctx, newBranch)
			_ = eng.CheckoutBranch(ctx, branchToSplit)
		}
	}()

	// Stage the hunks directly
	if err := eng.StageHunks(ctx, hunks); err != nil {
		return nil, fmt.Errorf("failed to stage hunks: %w", err)
	}

	// Check if anything was staged
	hasStaged, err := eng.HasStagedChanges(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to check staged changes: %w", err)
	}
	if !hasStaged {
		return nil, fmt.Errorf("no changes staged")
	}

	// Commit
	if err := eng.Commit(ctx, git.CommitOptions{
		Message:  commitMessage,
		NoVerify: true,
	}); err != nil {
		return nil, fmt.Errorf("failed to commit: %w", err)
	}

	// Track the new branch
	if err := eng.TrackBranch(ctx, newBranchName, parentBranch.GetName()); err != nil {
		return nil, fmt.Errorf("failed to track branch: %w", err)
	}

	// Preserve stack ID from original branch
	if originalStackID != "" {
		if err := eng.SetStackID(ctx, engine.BranchesOf(newBranch), originalStackID); err != nil {
			return nil, fmt.Errorf("failed to preserve stack ID: %w", err)
		}
	}

	succeeded = true
	if err := eng.CheckoutBranch(ctx, branchToSplit); err != nil {
		return nil, fmt.Errorf("failed to checkout original branch: %w", err)
	}

	return &Result{
		BranchNames:  []string{newBranchName},
		BranchPoints: []int{0},
	}, nil
}

// filterHunksByFiles filters hunks to only those affecting the specified files.
func filterHunksByFiles(hunks []git.Hunk, files []string) []git.Hunk {
	// Create a map for O(1) lookup
	fileSet := make(map[string]bool)
	for _, f := range files {
		fileSet[f] = true
	}

	var filtered []git.Hunk
	for _, h := range hunks {
		if fileSet[h.File] {
			filtered = append(filtered, h)
		}
	}
	return filtered
}

// promptForFiles shows an interactive file selector for split --by-file
func promptForFiles(ctx context.Context, prompter handlerBase.MultiSelector, branchToSplit engine.Branch, eng splitByFileEngine, splog output.Output, asSibling bool, direction Direction) ([]string, error) {
	if !utils.IsInteractive() {
		return nil, fmt.Errorf("file selection must be specified via pathspecs in non-interactive mode")
	}
	// Get the parent branch to compare against
	parentBranchName := branchToSplit.GetParentOrTrunk()

	// Get merge base between branch and parent
	mergeBase, err := eng.GetMergeBase(ctx, branchToSplit.GetName(), parentBranchName)
	if err != nil {
		return nil, fmt.Errorf("failed to get merge base: %w", err)
	}

	// Get list of changed files
	changedFiles, err := eng.GetChangedFiles(ctx, git.RevRange{Base: mergeBase, Head: branchToSplit.GetName()})
	if err != nil {
		return nil, fmt.Errorf("failed to get changed files: %w", err)
	}

	if len(changedFiles) == 0 {
		return nil, fmt.Errorf("no files changed in branch %s", branchToSplit.GetName())
	}

	if len(changedFiles) == 1 {
		return nil, fmt.Errorf("only one file changed in branch - use --by-hunk to split by individual changes")
	}

	// Show instructions based on mode
	splog.Info("Splitting %s by file.", output.CurrentBranch(branchToSplit.GetName()))
	switch {
	case asSibling:
		splog.Info("Select the files to extract to a new sibling branch.")
		splog.Info("The original branch will remain unchanged.")
	case direction == DirectionAbove:
		splog.Info("Select the files to extract to a new child branch.")
		splog.Info("The remaining files will stay on %s.", output.CurrentBranch(branchToSplit.GetName()))
	default:
		splog.Info("Select the files to extract to a new parent branch.")
		splog.Info("The remaining files will stay on %s.", output.CurrentBranch(branchToSplit.GetName()))
	}
	splog.Info("")

	// Prompt for file selection
	selectedFiles, err := prompter.MultiSelect("Select files to extract:", changedFiles)
	if err != nil {
		return nil, err
	}

	// Validate that not all files were selected (only in default mode where files are removed)
	if !asSibling && len(selectedFiles) == len(changedFiles) {
		return nil, fmt.Errorf("cannot extract all files - at least one must remain on the original branch; use --as-sibling to extract without modifying the original branch")
	}

	return selectedFiles, nil
}
