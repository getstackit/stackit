package combine

import (
	"errors"
	"fmt"

	"stackit.dev/stackit/internal/app"
)

// Action performs the multi-stack combine operation.
// It merges multiple independent stacks into a single consolidated branch,
// handling conflicts by skipping entire stacks that conflict.
func Action(ctx *app.Context, opts Options) (*Result, error) {
	eng := ctx.Engine

	// 1. Discover available stacks
	stacks, err := DiscoverStacks(eng)
	if err != nil {
		return nil, fmt.Errorf("failed to discover stacks: %w", err)
	}

	if len(stacks) == 0 {
		return nil, errors.New("no independent stacks found rooted at trunk")
	}

	// 2. Filter to selected stacks if provided
	if len(opts.SelectedStacks) > 0 {
		stacks = FilterStacks(stacks, opts.SelectedStacks)
		if len(stacks) == 0 {
			return nil, errors.New("none of the specified stacks were found")
		}
	}

	ctx.Output.Info("Combining %d stacks...", len(stacks))
	for _, stack := range stacks {
		ctx.Output.Info("  - %s (%d branches)", stack.RootBranch, len(stack.AllBranches))
	}

	if opts.DryRun {
		ctx.Output.Info("[dry-run] Would combine %d stacks", len(stacks))
		return &Result{
			IncludedStacks: stacks,
		}, nil
	}

	// 3. Execute in worktree to merge stacks
	executor := NewWorktreeExecutor(eng, ctx.Output)
	worktreeResult, err := executor.ExecuteInWorktree(ctx.Context, stacks)
	if err != nil {
		return nil, fmt.Errorf("failed to execute in worktree: %w", err)
	}
	defer worktreeResult.Cleanup()

	// 4. Check if any stacks merged successfully
	if len(worktreeResult.MergedStacks) == 0 {
		return nil, errors.New("all stacks have conflicts - nothing to combine")
	}

	// Report results so far
	ctx.Output.Success("Merged %d stacks successfully", len(worktreeResult.MergedStacks))
	for _, stack := range worktreeResult.MergedStacks {
		ctx.Output.Info("  + %s", stack.RootBranch)
	}

	if len(worktreeResult.ConflictStacks) > 0 {
		ctx.Output.Warn("Skipped %d conflicting stacks:", len(worktreeResult.ConflictStacks))
		for _, excluded := range worktreeResult.ConflictStacks {
			ctx.Output.Warn("  - %s (%s)", excluded.Stack.RootBranch, excluded.Reason)
		}
	}

	result := &Result{
		IncludedStacks: worktreeResult.MergedStacks,
		ExcludedStacks: worktreeResult.ConflictStacks,
	}

	// TODO Phase 3: Run CI validation and binary search if --skip-ci is not set
	// TODO Phase 4: Create consolidated PR

	return result, nil
}
