package combine

import (
	"context"
	"fmt"

	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/output"
)

// WorktreeResult contains the result of merging stacks in a worktree
type WorktreeResult struct {
	MergedStacks   []StackInfo     // Stacks that were successfully merged
	ConflictStacks []ExcludedStack // Stacks that conflicted
	WorktreePath   string          // Path to the worktree
	WorktreeEngine engine.Engine   // Engine for the worktree
	Cleanup        func()          // Function to clean up the worktree
}

// WorktreeExecutor handles merging stacks in a worktree
type WorktreeExecutor struct {
	eng    engine.Engine
	output output.Output
}

// NewWorktreeExecutor creates a new worktree executor
func NewWorktreeExecutor(eng engine.Engine, out output.Output) *WorktreeExecutor {
	return &WorktreeExecutor{
		eng:    eng,
		output: out,
	}
}

// ExecuteInWorktree creates a worktree at trunk and attempts to merge all stacks.
// For each stack, it merges all branches in order. If any branch in a stack
// conflicts, the entire stack is skipped.
func (w *WorktreeExecutor) ExecuteInWorktree(ctx context.Context, stacks []StackInfo) (*WorktreeResult, error) {
	trunk := w.eng.Trunk()

	// Create temporary worktree at trunk
	worktreePath, cleanup, err := w.eng.CreateTemporaryWorktree(ctx, trunk.GetName(), "stackit-combine-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create worktree: %w", err)
	}

	w.output.Debug("Created worktree at %s", worktreePath)

	// Create engine for worktree
	worktreeEng, err := engine.NewEngine(engine.Options{
		RepoRoot:          worktreePath,
		Trunk:             trunk.GetName(),
		MaxUndoStackDepth: 0, // No undo needed for combine
	})
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("failed to initialize worktree engine: %w", err)
	}

	result := &WorktreeResult{
		MergedStacks:   make([]StackInfo, 0),
		ConflictStacks: make([]ExcludedStack, 0),
		WorktreePath:   worktreePath,
		WorktreeEngine: worktreeEng,
		Cleanup:        cleanup,
	}

	// Try to merge each stack
	for _, stack := range stacks {
		err := w.tryMergeStack(ctx, worktreeEng, stack)
		if err != nil {
			w.output.Debug("Stack %s conflicts: %v", stack.RootBranch, err)
			result.ConflictStacks = append(result.ConflictStacks, ExcludedStack{
				Stack:  stack,
				Reason: "conflict",
			})
		} else {
			w.output.Debug("Stack %s merged successfully", stack.RootBranch)
			result.MergedStacks = append(result.MergedStacks, stack)
		}
	}

	return result, nil
}

// tryMergeStack attempts to merge all branches of a stack in order.
// Returns error if ANY branch conflicts (entire stack is skipped on conflict).
func (w *WorktreeExecutor) tryMergeStack(ctx context.Context, eng engine.Engine, stack StackInfo) error {
	for _, branchName := range stack.AllBranches {
		// Create a merge commit message
		msg := fmt.Sprintf("Merge %s for combine", branchName)

		err := eng.Merge(ctx, branchName, engine.MergeOptions{
			NoFF:    true,
			NoEdit:  true,
			Message: msg,
		})
		if err != nil {
			// Abort the merge if it's in progress
			git := eng.Git()
			if git.IsMergeInProgress(ctx) {
				if abortErr := git.MergeAbort(ctx); abortErr != nil {
					w.output.Debug("Failed to abort merge: %v", abortErr)
				}
			}
			return fmt.Errorf("conflict in branch %s: %w", branchName, err)
		}
	}
	return nil
}

// ResetToTrunk resets the worktree to trunk, discarding all merges.
// This is used by binary search to try different combinations.
func (w *WorktreeExecutor) ResetToTrunk(ctx context.Context, eng engine.Engine) error {
	trunk := w.eng.Trunk()
	return eng.ResetHard(ctx, trunk.GetName())
}
