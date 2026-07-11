package merge

import (
	"context"
	"fmt"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/worktree"
)

// MultiStackWorktreeResult contains the result of merging stacks in a worktree
type MultiStackWorktreeResult struct {
	MergedStacks   MultiStacks          // Stacks that were successfully merged
	ConflictStacks []MultiStackExcluded // Stacks that conflicted
	WorktreePath   string               // Path to the worktree
	WorktreeEngine engine.Engine        // Engine for the worktree
	Cleanup        func()               // Function to clean up the worktree
}

// MultiStackWorktreeExecutor handles merging stacks in a worktree
type MultiStackWorktreeExecutor struct {
	eng      engine.Engine
	output   output.Output
	executor *worktree.Executor
}

// NewMultiStackWorktreeExecutor creates a new worktree executor for multi-stack merge
func NewMultiStackWorktreeExecutor(eng engine.Engine, out output.Output) *MultiStackWorktreeExecutor {
	return &MultiStackWorktreeExecutor{
		eng:      eng,
		output:   out,
		executor: worktree.NewExecutor(eng, out),
	}
}

// ExecuteInWorktree creates a worktree at trunk and attempts to merge all stacks.
// It first tries a global octopus merge (all branches from all stacks in one commit).
// If that fails due to conflicts, it falls back to per-stack merging to identify
// which stacks conflict.
func (w *MultiStackWorktreeExecutor) ExecuteInWorktree(ctx context.Context, stacks []MultiStackInfo) (*MultiStackWorktreeResult, error) {
	// Create worktree session at trunk with pull
	session, err := w.executor.CreateSession(ctx, worktree.CreateSessionOptions{
		NamePattern: "stackit-multistack-*",
		PullTrunk:   true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create worktree session: %w", err)
	}

	result := &MultiStackWorktreeResult{
		MergedStacks:   make(MultiStacks, 0),
		ConflictStacks: make([]MultiStackExcluded, 0),
		WorktreePath:   session.Path,
		WorktreeEngine: session.Engine,
		Cleanup:        session.Close,
	}

	// Try global octopus merge first (all branches from all stacks in one commit)
	if err := w.tryGlobalOctopusMerge(ctx, session.Engine, stacks); err == nil {
		w.output.Debug("Global octopus merge succeeded for %d stacks", len(stacks))
		result.MergedStacks = stacks
		return result, nil
	}

	// Global octopus failed, fall back to per-stack merging to identify conflicts
	w.output.Debug("Global octopus merge failed, falling back to per-stack merging")

	// Reset to trunk before trying per-stack
	trunk := w.eng.Trunk()
	if err := session.Engine.ResetHard(ctx, trunk.GetName()); err != nil {
		session.Close()
		return nil, fmt.Errorf("failed to reset after global merge failure: %w", err)
	}

	// Try to merge each stack individually
	for _, stack := range stacks {
		baseline, err := session.GetCurrentRevision(ctx)
		if err != nil {
			session.Close()
			return nil, fmt.Errorf("failed to capture worktree state: %w", err)
		}

		if err := w.tryMergeStack(ctx, session.Engine, stack); err != nil {
			w.output.Debug("Stack %s conflicts: %v", stack.RootBranch, err)
			if resetErr := session.ResetToRef(ctx, baseline); resetErr != nil {
				w.output.Debug("Failed to reset worktree after conflict: %v", resetErr)
			}
			result.ConflictStacks = append(result.ConflictStacks, MultiStackExcluded{
				Stack:  stack,
				Reason: excludeReasonConflict,
			})
		} else {
			w.output.Debug("Stack %s merged successfully", stack.RootBranch)
			result.MergedStacks = append(result.MergedStacks, stack)
		}
	}

	return result, nil
}

// tryGlobalOctopusMerge attempts to merge all branches from all stacks in a single octopus merge.
// This creates one merge commit with all branches as parents.
func (w *MultiStackWorktreeExecutor) tryGlobalOctopusMerge(ctx context.Context, eng engine.Engine, stacks []MultiStackInfo) error {
	// Collect all branches from all stacks
	totalBranches := 0
	for _, stack := range stacks {
		totalBranches += len(stack.AllBranches)
	}
	allBranches := make([]string, 0, totalBranches)
	for _, stack := range stacks {
		allBranches = append(allBranches, stack.AllBranches...)
	}

	if len(allBranches) == 0 {
		return nil
	}

	// Build commit message
	msg := fmt.Sprintf("Merge %d stacks (%d branches)", len(stacks), len(allBranches))

	// Perform octopus merge (single merge commit with multiple parents)
	err := eng.MergeMultiple(ctx, allBranches, engine.MergeOptions{
		NoFF:    true,
		NoEdit:  true,
		Message: msg,
	})
	if err != nil {
		// Abort the merge if it's in progress
		if eng.IsMergeInProgress(ctx) {
			if abortErr := eng.MergeAbort(ctx); abortErr != nil {
				w.output.Debug("Failed to abort merge: %v", abortErr)
			}
		}
		return fmt.Errorf("global octopus merge failed: %w", err)
	}

	return nil
}

// tryMergeStack attempts to merge all branches of a stack via octopus merge.
// Returns error if any branch conflicts (entire stack is skipped on conflict).
func (w *MultiStackWorktreeExecutor) tryMergeStack(ctx context.Context, eng engine.Engine, stack MultiStackInfo) error {
	// Create a merge commit message
	msg := fmt.Sprintf("Merge stack %s for multi-stack (%d branches)", stack.RootBranch, len(stack.AllBranches))

	// Perform octopus merge (single merge commit with multiple parents)
	err := eng.MergeMultiple(ctx, stack.AllBranches, engine.MergeOptions{
		NoFF:    true,
		NoEdit:  true,
		Message: msg,
	})
	if err != nil {
		// Abort the merge if it's in progress
		if eng.IsMergeInProgress(ctx) {
			if abortErr := eng.MergeAbort(ctx); abortErr != nil {
				w.output.Debug("Failed to abort merge: %v", abortErr)
			}
		}
		return fmt.Errorf("conflict in stack %s: %w", stack.RootBranch, err)
	}
	return nil
}

// ResetToTrunk resets the worktree to trunk, discarding all merges.
// This is used by binary search to try different combinations.
func (w *MultiStackWorktreeExecutor) ResetToTrunk(ctx context.Context, eng engine.Engine) error {
	trunk := w.eng.Trunk()
	return eng.ResetHard(ctx, trunk.GetName())
}
