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

// ValidateBranchesToSubmit validates that branches are ready to submit and
// returns the submittable subset. Branches that are not restacked on their
// in-submission parent (or whose base no longer matches an out-of-submission
// parent remotely) are pruned together with their descendants, with a warning
// per pruned subtree, so the rest of the stack still submits. An error is
// returned only when nothing in scope is submittable.
func ValidateBranchesToSubmit(ctx *app.Context, branches []string) ([]string, error) {
	pr := ctx.PR()
	nav := ctx.Navigator()

	// Sync PR info first
	repoOwner, repoName, err := ctx.Engine.GetRepoInfo(ctx.Context)
	if err != nil {
		return nil, err
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

	// Validate base revisions, pruning unsubmittable subtrees
	submittable, err := validateBaseRevisions(branches, ctx.Status(), ctx)
	if err != nil {
		return nil, err
	}

	// Validate no merged/closed branches
	if err := validateNoMergedOrClosedBranches(submittable, ctx.Status(), ctx); err != nil {
		return nil, err
	}

	return submittable, nil
}

// validateBaseRevisions ensures that for each branch:
// 1. Its parent is trunk, OR
// 2. We are submitting its parent before it and it does not need restacking, OR
// 3. Its base matches the existing head for its parent's PR
//
// A branch that fails 2 or 3 is pruned from the submission along with its
// descendants (a child cannot be submitted against a parent state that isn't
// being pushed), so the rest of the stack still submits. Returns the
// submittable subset in the original order; errors only when it is empty.
func validateBaseRevisions(branches []string, eng engine.BranchStatus, ctx *app.Context) ([]string, error) {
	validatedBranches := make(map[string]bool)
	prunedBranches := make(map[string]bool)
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

	kept := make([]string, 0, len(branches))
	for _, branchName := range branches {
		branch := eng.GetBranch(branchName)
		parentBranchName := resolveSubmitParentName(nav, branch)

		parentBranch := eng.GetBranch(parentBranchName)
		switch {
		case prunedBranches[parentBranchName]:
			// Parent was pruned from this submission; the child cannot be
			// pushed against a base that isn't being submitted.
			prunedBranches[branchName] = true
			ctx.Output.Warn("Skipping %s — its parent %s was skipped.",
				output.BranchName(branchName), output.BranchName(parentBranchName))
			continue
		case parentBranch.IsTrunk():
			if !statuses.IsUpToDate(branch) {
				ctx.Output.Warn("%s is behind trunk and may conflict on merge — run 'stackit sync' and 'stackit restack' to update it.",
					output.BranchName(branchName))
			}
		case validatedBranches[parentBranchName]:
			// Parent is in the submission list
			if !statuses.IsUpToDate(branch) {
				prunedBranches[branchName] = true
				ctx.Output.Warn("Skipping %s — it has not been restacked on its parent %s. Run 'stackit restack', then submit again.",
					output.BranchName(branchName), output.BranchName(parentBranchName))
				continue
			}
		default:
			// Parent is not in submission list
			if !parentRemoteStatuses().ForBranch(parentBranch).Matches() {
				prunedBranches[branchName] = true
				ctx.Output.Warn("Skipping %s — its base does not match its parent %s on the remote. Use 'stackit submit --stack' to include its ancestors.",
					output.BranchName(branchName), output.BranchName(parentBranchName))
				continue
			}
		}

		validatedBranches[branchName] = true
		kept = append(kept, branchName)
	}

	if len(kept) == 0 {
		return nil, fmt.Errorf("no submittable branches: every branch in scope needs a restack first. Run 'stackit restack', then submit again")
	}
	return kept, nil
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
