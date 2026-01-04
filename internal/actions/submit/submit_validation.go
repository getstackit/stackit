// Package submit provides functionality for submitting stacked branches as pull requests.
package submit

import (
	"context"
	"fmt"

	"stackit.dev/stackit/internal/actions"
	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/github"
	"stackit.dev/stackit/internal/tui/style"
)

// ValidateBranchesToSubmit validates that branches are ready to submit
func ValidateBranchesToSubmit(ctx *app.Context, branches []string) error {
	pr := ctx.PR()
	nav := ctx.Navigator()

	// Sync PR info first
	repoOwner, repoName, err := ctx.Git().GetRepoInfo(ctx.Context)
	if err != nil {
		return err
	}
	if repoOwner != "" && repoName != "" {
		if err := github.SyncPrInfo(ctx.Context, ctx.Git(), branches, repoOwner, repoName, func(name string, prInfo *github.PullRequestInfo) {
			branch := nav.GetBranch(name)

			// Preserve existing locked status
			lockReason := engine.LockReasonNone
			if existing, err := branch.GetPrInfo(); err == nil && existing != nil {
				lockReason = existing.LockReason()
			}

			_ = pr.UpsertPrInfo(branch, engine.NewPrInfo(
				&prInfo.Number,
				prInfo.Title,
				prInfo.Body,
				prInfo.State,
				prInfo.Base,
				prInfo.HTMLURL,
				prInfo.Draft,
			).WithLockReason(lockReason))
		}); err != nil {
			// Non-fatal, continue
			ctx.Splog.Debug("Failed to sync PR info: %v", err)
		}
	}

	// Validate base revisions
	if err := validateBaseRevisions(branches, ctx.Status(), ctx); err != nil {
		return err
	}

	// Validate no empty branches
	if err := validateNoEmptyBranches(ctx.Context, branches, ctx.Navigator(), ctx); err != nil {
		return err
	}

	// Validate no merged/closed branches
	if err := validateNoMergedOrClosedBranches(branches, ctx.Status(), ctx); err != nil {
		return err
	}

	return nil
}

// validateBaseRevisions ensures that for each branch:
// 1. Its parent is trunk, OR
// 2. We are submitting its parent before it and it does not need restacking, OR
// 3. Its base matches the existing head for its parent's PR
func validateBaseRevisions(branches []string, eng engine.BranchStatus, ctx *app.Context) error {
	validatedBranches := make(map[string]bool)

	for _, branchName := range branches {
		branch := eng.GetBranch(branchName)
		parentBranchName := branch.GetParentPrecondition()

		parentBranch := eng.GetBranch(parentBranchName)
		switch {
		case parentBranch.IsTrunk():
			if !branch.IsBranchUpToDate() {
				ctx.Splog.Info("Note that %s has fallen behind trunk. You may encounter conflicts if you attempt to merge it.",
					style.ColorBranchName(branchName, false))
			}
		case validatedBranches[parentBranchName]:
			// Parent is in the submission list
			if !branch.IsBranchUpToDate() {
				return fmt.Errorf("you are trying to submit at least one branch that has not been restacked on its parent. To resolve this, check out %s and run 'stackit restack'",
					style.ColorBranchName(branchName, false))
			}
		default:
			// Parent is not in submission list
			status, err := eng.GetBranchRemoteStatus(parentBranch)
			if err != nil {
				return fmt.Errorf("failed to check if parent branch matches remote: %w", err)
			}
			if !status.Matches() {
				return fmt.Errorf("you are trying to submit at least one branch whose base does not match its parent remotely, without including its parent. You may want to use 'stackit submit --stack' to ensure that the ancestors of %s are included in your submission",
					style.ColorBranchName(branchName, false))
			}
		}

		validatedBranches[branchName] = true
	}

	return nil
}

// validateNoEmptyBranches checks for empty branches and prompts user if found
func validateNoEmptyBranches(ctx context.Context, branches []string, nav engine.StackNavigator, runtimeCtx *app.Context) error {
	emptyBranches := []string{}
	for _, branchName := range branches {
		isEmpty, err := nav.IsBranchEmpty(ctx, branchName)
		if err != nil {
			continue
		}
		if isEmpty {
			emptyBranches = append(emptyBranches, branchName)
		}
	}

	if len(emptyBranches) == 0 {
		return nil
	}

	hasMultiple := len(emptyBranches) > 1
	runtimeCtx.Splog.Warn("The following branch%s have no changes:", actions.PluralSuffix(hasMultiple))
	for _, b := range emptyBranches {
		runtimeCtx.Splog.Warn("▸ %s", b)
	}
	runtimeCtx.Splog.Warn("Are you sure you want to submit %s?", actions.PluralIt(hasMultiple))

	// For now, we'll allow empty branches (non-interactive mode)
	// In interactive mode, we would prompt here
	// TODO: Add interactive prompt when needed

	return nil
}

// validateNoMergedOrClosedBranches checks for merged/closed PRs and prompts user if found
func validateNoMergedOrClosedBranches(branches []string, eng engine.BranchStatus, ctx *app.Context) error {
	mergedOrClosedBranches := []string{}
	for _, branchName := range branches {
		branch := eng.GetBranch(branchName)
		prInfo, err := eng.GetPrInfo(branch)
		if err != nil {
			continue
		}
		if prInfo != nil && (prInfo.State() == "MERGED" || prInfo.State() == "CLOSED") {
			mergedOrClosedBranches = append(mergedOrClosedBranches, branchName)
		}
	}

	if len(mergedOrClosedBranches) == 0 {
		return nil
	}

	hasMultiple := len(mergedOrClosedBranches) > 1
	ctx.Splog.Tip("You can use 'stackit sync' to find and delete all merged/closed branches automatically and rebase their children.")
	ctx.Splog.Warn("PR%s for the following branch%s already been merged or closed:", actions.PluralSuffix(hasMultiple), actions.PluralSuffix(hasMultiple))
	for _, b := range mergedOrClosedBranches {
		ctx.Splog.Warn("▸ %s", b)
	}

	// For now, we'll allow creating new PRs (non-interactive mode)
	// In interactive mode, we would prompt here
	// TODO: Add interactive prompt when needed
	// Note: We used to clear PR info here, but mutation requires mutation interface.
	for _, branchName := range mergedOrClosedBranches {
		ctx.Splog.Debug("Branch %s already has a merged/closed PR", branchName)
	}

	return nil
}
