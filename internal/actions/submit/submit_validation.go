// Package submit provides functionality for submitting stacked branches as pull requests.
package submit

import (
	"github.com/getstackit/stackit/internal/git"

	"fmt"
	"sync"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/github"
	"github.com/getstackit/stackit/internal/output"
)

// ValidateBranchesToSubmit validates that branches are ready to submit
func ValidateBranchesToSubmit(ctx *app.Context, branches []string) error {
	pr := ctx.PR()
	nav := ctx.Navigator()

	// Sync PR info first
	repoOwner, repoName, err := ctx.Engine.GetRepoInfo(ctx.Context)
	if err != nil {
		return err
	}
	if repoOwner != "" && repoName != "" {
		// Collect updates from the callback (which may be called concurrently in the
		// REST fallback) then write all PR info in one atomic batch.
		remoteCtx, cancelRemote := ctx.RemoteOperationContext()
		var mu sync.Mutex
		updates := make(map[string]*engine.PrInfo)
		if err := github.SyncPrInfo(remoteCtx, ctx.Git(), branches, github.Repo{Owner: repoOwner, Name: repoName}, func(name string, prInfo *github.PullRequestInfo) { //nolint:forbidigo // GitHub integration needs the git runner to run gh; not a domain bypass
			branch := nav.GetBranch(name)

			lockReason := engine.LockReasonNone
			if existing, err := branch.GetPrInfo(); err == nil && existing != nil {
				lockReason = existing.LockReason()
			}

			mu.Lock()
			updates[name] = engine.NewPrInfo(
				&prInfo.Number,
				prInfo.Title,
				prInfo.Body,
				prInfo.State,
				prInfo.Base,
				prInfo.HTMLURL,
				prInfo.Draft,
			).WithLockReason(lockReason)
			mu.Unlock()
		}); err != nil {
			// Non-fatal, continue
			ctx.Output.Debug("Failed to sync PR info: %v", err)
		}
		cancelRemote()
		if err := pr.BatchUpsertPrInfo(ctx.Context, updates); err != nil {
			ctx.Output.Debug("Failed to update PR info: %v", err)
		}
	}

	// Validate base revisions
	if err := validateBaseRevisions(branches, ctx.Status(), ctx); err != nil {
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
	nav := ctx.Navigator()

	// Read remote status for every parent at most once, and only if a branch
	// actually needs it, so a stack whose parents are all trunk or in-list stays
	// offline. When needed, all parents are read in a single ls-remote instead
	// of one per branch.
	var (
		remoteStatuses engine.BranchRemoteStatuses
		remoteRead     bool
	)
	parentRemoteStatuses := func() engine.BranchRemoteStatuses {
		if !remoteRead {
			parentBranches := engine.Branches{}
			seen := make(map[string]bool)
			for _, branchName := range branches {
				parentName := resolveSubmitParentName(nav, eng.GetBranch(branchName))
				if seen[parentName] {
					continue
				}
				seen[parentName] = true
				parentBranches = parentBranches.Append(eng.GetBranch(parentName))
			}
			remoteCtx, cancelRemote := ctx.RemoteOperationContext()
			remoteStatuses = eng.ReadBranchRemoteStatuses(remoteCtx, parentBranches)
			cancelRemote()
			remoteRead = true
		}
		return remoteStatuses
	}

	// Resolve up-to-date status for every branch in one batched parent-revision
	// read instead of a per-branch IsBranchUpToDate() inside the loop (each of
	// which would shell a separate `git rev-parse` for the parent).
	branchObjs := make(engine.Branches, 0, len(branches))
	for _, branchName := range branches {
		branchObjs = append(branchObjs, eng.GetBranch(branchName))
	}
	statuses := eng.ReadBranchStatuses(branchObjs)

	for _, branchName := range branches {
		branch := eng.GetBranch(branchName)
		parentBranchName := resolveSubmitParentName(nav, branch)

		parentBranch := eng.GetBranch(parentBranchName)
		switch {
		case parentBranch.IsTrunk():
			if !statuses.IsUpToDate(branch) {
				ctx.Output.Warn("%s is behind trunk and may conflict on merge — run 'stackit sync' and 'stackit restack' to update it.",
					output.BranchName(branchName))
			}
		case validatedBranches[parentBranchName]:
			// Parent is in the submission list
			if !statuses.IsUpToDate(branch) {
				return fmt.Errorf("you are trying to submit at least one branch that has not been restacked on its parent. To resolve this, check out %s and run 'stackit restack'",
					output.BranchName(branchName))
			}
		default:
			// Parent is not in submission list
			if !parentRemoteStatuses().ForBranch(parentBranch).Matches() {
				return fmt.Errorf("you are trying to submit at least one branch whose base does not match its parent remotely, without including its parent. You may want to use 'stackit submit --stack' to ensure that the ancestors of %s are included in your submission",
					output.BranchName(branchName))
			}
		}

		validatedBranches[branchName] = true
	}

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
		if prInfo != nil && (prInfo.State() == git.PRStateMerged || prInfo.State() == git.PRStateClosed) {
			mergedOrClosedBranches = append(mergedOrClosedBranches, branchName)
		}
	}

	if len(mergedOrClosedBranches) == 0 {
		return nil
	}

	hasMultiple := len(mergedOrClosedBranches) > 1
	ctx.Output.Warn("PR%s for the following branch%s already been merged or closed:", actions.PluralSuffix("PR", hasMultiple), actions.PluralSuffix("branch", hasMultiple))
	for _, b := range mergedOrClosedBranches {
		ctx.Output.Warn("▸ %s", b)
	}
	ctx.Output.Tip("Run 'stackit sync' to delete merged/closed branches and rebase their children.")

	// For now, we'll allow creating new PRs (non-interactive mode)
	// In interactive mode, we would prompt here
	// TODO: Add interactive prompt when needed
	// Note: We used to clear PR info here, but mutation requires mutation interface.
	for _, branchName := range mergedOrClosedBranches {
		ctx.Output.Debug("Branch %s already has a merged/closed PR", branchName)
	}

	return nil
}
