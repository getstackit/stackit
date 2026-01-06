package combine

import (
	"errors"
	"fmt"

	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/config"
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

	// 5. Run CI validation if not skipped
	if !opts.SkipCI {
		cfg, err := config.LoadConfig(ctx.RepoRoot)
		if err != nil {
			return nil, fmt.Errorf("failed to load config: %w", err)
		}
		validator := NewCIValidator(cfg, ctx.Output)
		if !validator.IsConfigured() {
			ctx.Output.Warn("CI validation skipped (no combine.ciCommand configured)")
		} else {
			// Run CI on merged stacks
			err := validator.Validate(ctx.Context, worktreeResult.WorktreePath)
			if err != nil {
				// CI failed - try binary search to find largest working set
				ctx.Output.Warn("CI validation failed, searching for working subset...")

				searchResult, searchErr := FindLargestWorkingSet(
					ctx.Context,
					validator,
					executor,
					worktreeResult.WorktreeEngine,
					worktreeResult.WorktreePath,
					worktreeResult.MergedStacks,
				)
				if searchErr != nil {
					return nil, fmt.Errorf("binary search failed: %w", searchErr)
				}

				if len(searchResult.WorkingStacks) == 0 {
					return nil, errors.New("no combination of stacks passes CI")
				}

				// Update result with binary search findings
				result.IncludedStacks = searchResult.WorkingStacks
				result.ExcludedStacks = append(result.ExcludedStacks, searchResult.FailedStacks...)

				ctx.Output.Success("Found %d stacks that pass CI together", len(result.IncludedStacks))
			}
		}
	}

	// 6. Create consolidated PR
	prCreator := NewPRCreator(ctx, worktreeResult.WorktreeEngine, worktreeResult.WorktreePath)
	branchName := GenerateBranchName()

	ctx.Output.Info("Creating combined branch: %s", branchName)
	if err := prCreator.CreateAndPushBranch(ctx.Context, branchName); err != nil {
		return nil, fmt.Errorf("failed to create and push branch: %w", err)
	}

	ctx.Output.Info("Creating pull request...")
	pr, err := prCreator.CreatePR(ctx.Context, branchName, result.IncludedStacks, result.ExcludedStacks)
	if err != nil {
		return nil, fmt.Errorf("failed to create PR: %w", err)
	}

	result.PRNumber = pr.Number
	result.PRURL = pr.HTMLURL
	result.BranchName = branchName

	ctx.Output.Success("Created PR #%d: %s", pr.Number, pr.HTMLURL)

	// 7. Optionally wait for CI and auto-merge
	if opts.Wait {
		ctx.Output.Info("Waiting for CI to pass...")
		if err := prCreator.WaitAndMerge(ctx.Context, branchName, pr); err != nil {
			return nil, fmt.Errorf("failed to wait and merge: %w", err)
		}
		ctx.Output.Success("PR merged successfully!")
	}

	return result, nil
}
