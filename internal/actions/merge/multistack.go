package merge

import (
	"errors"
	"fmt"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/config"
	"github.com/getstackit/stackit/internal/engine"
)

// ExecuteMultiStack performs the multi-stack merge operation.
// It merges multiple independent stacks into a single consolidated branch,
// handling conflicts by skipping entire stacks that conflict.
func ExecuteMultiStack(ctx *app.Context, opts MultiStackOptions) (*MultiStackResult, error) {
	eng := ctx.Engine
	out := ctx.Output

	out.Debug("multistack: starting with opts=%+v", opts)

	// 1. Discover available stacks
	out.Debug("multistack: discovering available stacks")
	stacks, err := DiscoverStacks(eng)
	if err != nil {
		out.Debug("multistack: failed to discover stacks: %v", err)
		return nil, fmt.Errorf("failed to discover stacks: %w", err)
	}
	out.Debug("multistack: discovered %d stacks", len(stacks))

	if len(stacks) == 0 {
		out.Debug("multistack: no stacks found")
		return nil, errors.New("no independent stacks found rooted at trunk")
	}

	// 2. Filter to selected stacks if provided
	if len(opts.SelectedStacks) > 0 {
		out.Debug("multistack: filtering to selected stacks: %v", opts.SelectedStacks)
		stacks = FilterStacks(stacks, opts.SelectedStacks)
		if len(stacks) == 0 {
			out.Debug("multistack: no matching stacks after filter")
			return nil, errors.New("none of the specified stacks were found")
		}
		out.Debug("multistack: %d stacks after filtering", len(stacks))
	}

	out.Info("Combining %d stacks...", len(stacks))
	for _, stack := range stacks {
		out.Info("  - %s (%d branches)", stack.RootBranch, len(stack.AllBranches))
	}

	// 2.5. Validate that all branches match their remote
	// This is critical: if branches differ from remote, the octopus merge uses local SHAs
	// but PRs track remote SHAs, so GitHub won't auto-close individual PRs
	out.Debug("multistack: validating branches match remote")
	if err := validateBranchesMatchRemote(eng, stacks, out); err != nil {
		out.Debug("multistack: branch validation failed: %v", err)
		return nil, err
	}

	// 3. Execute in worktree to merge stacks
	out.Debug("multistack: creating worktree executor")
	executor := NewMultiStackWorktreeExecutor(eng, out)
	out.Debug("multistack: executing in worktree")
	worktreeResult, err := executor.ExecuteInWorktree(ctx.Context, stacks)
	if err != nil {
		out.Debug("multistack: worktree execution failed: %v", err)
		return nil, fmt.Errorf("failed to execute in worktree: %w", err)
	}
	defer worktreeResult.Cleanup()
	out.Debug("multistack: worktree execution complete, merged=%d, conflicts=%d",
		len(worktreeResult.MergedStacks), len(worktreeResult.ConflictStacks))

	// 4. Check if any stacks merged successfully
	if len(worktreeResult.MergedStacks) == 0 {
		out.Debug("multistack: no stacks merged successfully")
		return nil, errors.New("all stacks have conflicts - nothing to combine")
	}

	// Report results so far
	out.Success("Merged %d stacks successfully", len(worktreeResult.MergedStacks))
	for _, stack := range worktreeResult.MergedStacks {
		out.Info("  + %s", stack.RootBranch)
	}

	if len(worktreeResult.ConflictStacks) > 0 {
		out.Warn("Skipped %d conflicting stacks:", len(worktreeResult.ConflictStacks))
		for _, excluded := range worktreeResult.ConflictStacks {
			out.Warn("  - %s (%s)", excluded.Stack.RootBranch, excluded.Reason)
		}
	}

	result := &MultiStackResult{
		IncludedStacks: worktreeResult.MergedStacks,
		ExcludedStacks: worktreeResult.ConflictStacks,
	}

	// 4.5. Generate branch name early and lock individual PRs before CI validation
	// This indicates the PRs are part of a consolidation in progress
	branchName := GenerateMultiStackBranchName()
	out.Debug("multistack: locking individual PRs before CI validation")
	if err := lockAndNotifyMultiStackPRs(ctx, eng, result.IncludedStacks, branchName); err != nil {
		out.Warn("Failed to lock individual PRs: %v", err)
	}

	// 5. Run CI validation if not skipped
	if !opts.SkipLocalCI {
		out.Debug("multistack: running CI validation")
		cfg, err := config.LoadConfig(ctx.RepoRoot)
		if err != nil {
			out.Debug("multistack: failed to load config: %v", err)
			return nil, fmt.Errorf("failed to load config: %w", err)
		}
		validator := NewLocalCIValidator(cfg, out)
		if !validator.IsConfigured() {
			out.Debug("multistack: CI not configured, skipping")
			out.Warn("CI validation skipped (no ci.command configured)")
		} else {
			// Run CI on merged stacks
			out.Debug("multistack: running CI validation")
			err := validator.Validate(ctx.Context, worktreeResult.WorktreePath)
			if err != nil {
				out.Debug("multistack: CI validation failed: %v", err)
				// CI failed - try binary search to find largest working set
				out.Warn("CI validation failed, searching for working subset...")

				searchResult, searchErr := FindLargestWorkingSet(
					ctx.Context,
					validator,
					executor,
					worktreeResult.WorktreeEngine,
					worktreeResult.WorktreePath,
					worktreeResult.MergedStacks,
				)
				if searchErr != nil {
					out.Debug("multistack: binary search failed: %v", searchErr)
					return nil, fmt.Errorf("binary search failed: %w", searchErr)
				}

				if len(searchResult.WorkingStacks) == 0 {
					out.Debug("multistack: no combination of stacks passes CI")
					return nil, errors.New("no combination of stacks passes CI")
				}

				// Update result with binary search findings
				result.IncludedStacks = searchResult.WorkingStacks
				result.ExcludedStacks = append(result.ExcludedStacks, searchResult.FailedStacks...)

				out.Success("Found %d stacks that pass CI together", len(result.IncludedStacks))
			} else {
				out.Debug("multistack: CI validation passed")
			}
		}
	} else {
		out.Debug("multistack: CI validation skipped (--skip-local-ci)")
	}

	// 6. Create consolidated PR
	out.Debug("multistack: creating consolidated PR")
	prCreator := NewMultiStackPRCreator(ctx, worktreeResult.WorktreeEngine, worktreeResult.WorktreePath)

	out.Info("Creating combined branch: %s", branchName)
	out.Debug("multistack: creating and pushing branch")
	if err := prCreator.CreateAndPushBranch(ctx.Context, branchName); err != nil {
		out.Debug("multistack: failed to create and push branch: %v", err)
		return nil, fmt.Errorf("failed to create and push branch: %w", err)
	}

	out.Info("Creating pull request...")
	out.Debug("multistack: creating PR with %d included stacks, %d excluded stacks",
		len(result.IncludedStacks), len(result.ExcludedStacks))
	pr, err := prCreator.CreatePR(ctx.Context, branchName, result.IncludedStacks, result.ExcludedStacks)
	if err != nil {
		out.Debug("multistack: failed to create PR: %v", err)
		return nil, fmt.Errorf("failed to create PR: %w", err)
	}

	result.PRNumber = pr.Number
	result.PRURL = pr.HTMLURL
	result.BranchName = branchName

	out.Success("Created PR #%d: %s", pr.Number, pr.HTMLURL)
	out.Debug("multistack: PR created successfully")
	metadata := prCreator.BuildStackMetadata(result.IncludedStacks)
	trailers := metadata.ToTrailers()

	// 7. Optionally wait for CI and auto-merge
	if opts.Wait {
		out.Info("Waiting for CI to pass...")
		out.Debug("multistack: waiting for CI and auto-merge")
		if err := prCreator.WaitAndMerge(ctx.Context, branchName, pr, trailers); err != nil {
			out.Debug("multistack: wait and merge failed: %v", err)
			return nil, fmt.Errorf("failed to wait and merge: %w", err)
		}
		out.Success("PR merged successfully!")
		out.Debug("multistack: merge complete")

		// 8. Clean up individual PRs from all included stacks
		out.Debug("multistack: cleaning up individual PRs")
		var branchNames []string
		for _, stack := range result.IncludedStacks {
			branchNames = append(branchNames, stack.AllBranches...)
		}

		userName, _ := eng.Git().GetUserName(ctx.Context)
		cleaner := NewPRCleaner(ctx, eng, PRCleanupConfig{
			Source:                CleanupSourceMultiStack,
			ConsolidationPRNumber: pr.Number,
			UserName:              userName,
		})

		cleanupResult := cleaner.CleanupBranches(ctx.Context, branchNames)
		cleaner.LogResult(cleanupResult)
	} else {
		out.Debug("multistack: enabling auto-merge for fire-and-forget")
		if err := prCreator.EnableAutoMerge(ctx.Context, pr, trailers); err != nil {
			out.Warn("Could not enable automerge: %v", err)
			out.Tip("Enable automerge manually on the PR: %s", pr.HTMLURL)
		} else {
			out.Success("Automerge enabled on PR #%d", pr.Number)
		}
	}

	out.Debug("multistack: completed successfully")
	return result, nil
}

// validateBranchesMatchRemote checks that all branches in the stacks match their remote.
// This is critical for shipping: if local differs from remote, the octopus merge
// will use local SHAs but PRs track remote SHAs, so GitHub won't auto-close them.
func validateBranchesMatchRemote(eng engine.Engine, stacks []MultiStackInfo, out interface{ Warn(string, ...any) }) error {
	var mismatchedBranches []string

	for _, stack := range stacks {
		for _, branchName := range stack.AllBranches {
			branch := eng.GetBranch(branchName)
			status, err := eng.GetBranchRemoteStatus(branch)
			if err != nil {
				continue // Skip branches we can't check
			}

			if !status.Matches() {
				mismatchedBranches = append(mismatchedBranches, branchName)
			}
		}
	}

	if len(mismatchedBranches) > 0 {
		for _, branch := range mismatchedBranches {
			out.Warn("Branch %s differs from remote", branch)
		}
		return fmt.Errorf("cannot ship: %d branch(es) differ from remote - push changes before shipping", len(mismatchedBranches))
	}

	return nil
}

// lockAndNotifyMultiStackPRs locks individual PRs and updates them with consolidation info.
// This mirrors the behavior in consolidate.go's lockAndNotifyIndividualPRs.
func lockAndNotifyMultiStackPRs(ctx *app.Context, eng engine.Engine, includedStacks []MultiStackInfo, consolidationBranch string) error {
	out := ctx.Output
	out.Info("🔒 Locking individual PRs and updating status...")

	branchesToLock := []engine.Branch{}
	branchNames := []string{}

	for _, stack := range includedStacks {
		for _, branchName := range stack.AllBranches {
			branch := eng.GetBranch(branchName)
			if !branch.IsLocked() {
				branchesToLock = append(branchesToLock, branch)
			}
			branchNames = append(branchNames, branchName)
		}
	}

	if len(branchesToLock) > 0 {
		if _, err := eng.SetLocked(ctx, branchesToLock, engine.LockReasonConsolidating); err != nil {
			return fmt.Errorf("failed to lock branches: %w", err)
		}
	}

	// Update PR info with consolidation branch
	for _, b := range branchesToLock {
		prInfo, _ := b.GetPrInfo()
		if prInfo != nil {
			if err := eng.UpsertPrInfo(ctx.Context, b, prInfo.WithMergeBranch(consolidationBranch)); err != nil {
				out.Debug("Failed to upsert PR info for %s: %v", b.GetName(), err)
			}
		}
	}

	// Sync PRs with updated metadata
	if err := actions.PushMetadataAndSyncPRs(ctx, branchNames); err != nil {
		out.Warn("Failed to sync individual PRs: %v", err)
	}

	return nil
}
