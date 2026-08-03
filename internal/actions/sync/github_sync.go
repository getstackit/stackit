package sync

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/github"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/utils"
)

// GitHubSyncResult holds the results from GitHub PR info sync (network operation)
type GitHubSyncResult struct {
	BranchNames []string
	RepoOwner   string
	RepoName    string
	PRInfos     map[string]*github.PullRequestInfo
	mu          sync.Mutex
}

// syncGitHubPRInfo fetches PR info from GitHub (network operation only)
// This is designed to run in parallel with other network operations. The bounded
// remoteCtx governs every network call; the app context only supplies the
// navigator, logger, and git runner.
func syncGitHubPRInfo(remoteCtx context.Context, ctx *app.Context) (*GitHubSyncResult, error) {
	nav := ctx.Navigator()

	setupStart := time.Now()
	allBranches := nav.AllBranches()
	branchNames := allBranches.Names()

	repoOwner, repoName, err := nav.GetRepoInfo(remoteCtx)
	if err != nil {
		return nil, fmt.Errorf("failed to get repository info: %w", err)
	}
	ctx.Logger.Info("github sync setup completed durationMs=%v branchCount=%v", time.Since(setupStart).Milliseconds(), len(branchNames))

	result := &GitHubSyncResult{
		BranchNames: branchNames,
		RepoOwner:   repoOwner,
		RepoName:    repoName,
		PRInfos:     make(map[string]*github.PullRequestInfo),
	}

	if repoOwner == "" || repoName == "" {
		return result, nil
	}

	// Keep the recorded PR numbers so a branch deleted on GitHub can still be
	// resolved to its closed PR. A missing remote ref by itself is not enough to
	// delete local work; this only supplies GitHub's PR state to normal cleanup.
	knownPRNumbers := make(map[string]int)
	for _, branch := range allBranches {
		prInfo, err := branch.GetPrInfo()
		if err != nil || prInfo == nil || prInfo.Number() == nil || *prInfo.Number() <= 0 {
			continue
		}
		knownPRNumbers[branch.GetName()] = *prInfo.Number()
	}

	// Sync PR info from GitHub (this is already parallelized internally)
	syncPrStart := time.Now()
	if err := github.SyncPrInfoWithKnownPRNumbers(remoteCtx, ctx.Git(), branchNames, github.Repo{Owner: repoOwner, Name: repoName}, knownPRNumbers, func(name string, prInfo *github.PullRequestInfo) { //nolint:forbidigo // GitHub integration needs the git runner to run gh; not a domain bypass
		result.mu.Lock()
		result.PRInfos[name] = prInfo
		result.mu.Unlock()
	}); err != nil {
		return nil, fmt.Errorf("failed to sync PR info from GitHub: %w", err)
	}
	ctx.Logger.Info("sync pr info from github completed durationMs=%d prsUpdated=%d", time.Since(syncPrStart).Milliseconds(), len(result.PRInfos))

	return result, nil
}

// processGitHubSyncResult processes GitHub PR info after the network operation completes
// This must run after syncGitHubPRInfo completes
//
//nolint:unparam // error return is for future error handling
func processGitHubSyncResult(ctx *app.Context, result *GitHubSyncResult, dirtyAnchors dirtyAnchorSet, handler Handler) error {
	eng := ctx.PR()
	nav := ctx.Navigator()
	out := ctx.Output

	// Update local PR info from GitHub results, preserving lock reasons.
	// Collect into a map first so we can write all updates in one atomic batch.
	updates := make(map[string]*engine.PrInfo, len(result.PRInfos))
	for name, prInfo := range result.PRInfos {
		branch := nav.GetBranch(name)

		lockReason := engine.LockReasonNone
		if existing, err := branch.GetPrInfo(); err == nil && existing != nil {
			lockReason = existing.LockReason()
		}

		updates[name] = engine.NewPrInfo(
			&prInfo.Number,
			prInfo.Title,
			prInfo.Body,
			prInfo.State,
			prInfo.Base,
			prInfo.HTMLURL,
			prInfo.Draft,
		).WithLockReason(lockReason)
	}

	if err := eng.BatchUpsertPrInfo(ctx.Context, updates); err != nil {
		out.Debug("Failed to update PR info: %v", err)
	}
	prsUpdated := len(updates)

	// Emit completion event with count
	if prsUpdated > 0 {
		handler.EmitEvent(Event{
			Phase:   PhaseGitHub,
			Type:    EventCompleted,
			Message: fmt.Sprintf("Updated PR info for %d branches", prsUpdated),
		})
	} else {
		handler.EmitEvent(Event{
			Phase:   PhaseGitHub,
			Type:    EventCompleted,
			Message: "PR info up to date",
		})
	}

	// Update PR body footers only for branches flagged as needing updates
	if ctx.GitHub() != nil && result.RepoOwner != "" && result.RepoName != "" {
		flaggedBranches := ctx.Engine.GetBranchesNeedingPRBodyUpdate()
		if len(flaggedBranches) > 0 {
			updateMetaStart := time.Now()
			// Emit progress events for each branch being updated
			for _, branchName := range flaggedBranches {
				handler.EmitEvent(Event{
					Phase:   PhaseGitHub,
					Type:    EventProgress,
					Branch:  branchName,
					Message: fmt.Sprintf("Updating PR metadata for %s", branchName),
				})
			}
			actions.UpdateStackPRMetadata(ctx, flaggedBranches)
			ctx.Logger.Info("update stack pr metadata completed durationMs=%d branchCount=%d",
				time.Since(updateMetaStart).Milliseconds(), len(flaggedBranches))
		}
	}

	// Push local parent changes to GitHub PR bases
	// Local metadata is authoritative - if local parent differs from GitHub PR base, update GitHub
	parentsStart := time.Now()
	if ctx.GitHub() != nil && result.RepoOwner != "" && result.RepoName != "" {
		syncResult, err := PushParentsToGitHub(ctx, result, dirtyAnchors)
		ctx.Logger.Info("sync parents to github completed durationMs=%d", time.Since(parentsStart).Milliseconds())
		if err != nil {
			out.Debug("Failed to sync parents to GitHub: %v", err)
		} else if len(syncResult.BranchesUpdated) > 0 {
			out.Info("Updated PR base for %d branches to match local stack", len(syncResult.BranchesUpdated))
		}
	}

	return nil
}

// ParentsToGitHubResult contains the result of pushing local parents to GitHub
type ParentsToGitHubResult struct {
	BranchesUpdated []string // Branches whose PR base was updated on GitHub
}

// PushParentsToGitHub pushes local parent relationships to GitHub PR bases.
// Local metadata is authoritative - if local parent differs from GitHub PR base, update GitHub.
func PushParentsToGitHub(ctx *app.Context, result *GitHubSyncResult, dirtyAnchors dirtyAnchorSet) (*ParentsToGitHubResult, error) {
	eng := ctx.Engine
	out := ctx.Output
	gctx := ctx.Context
	githubClient := ctx.GitHub()

	allBranches := eng.AllBranches()

	type baseUpdate struct {
		branchName      string
		prNumber        int
		oldBase         string
		localParentName string
	}

	var pending []baseUpdate
	for _, branch := range allBranches {
		if branch.IsTrunk() {
			continue
		}

		// Skip branches in dirty stacks
		if dirtyAnchors.includes(ctx, branch.GetName()) {
			continue
		}

		// Get the PR info we just fetched from GitHub
		prInfo, ok := result.PRInfos[branch.GetName()]
		if !ok || prInfo == nil || prInfo.Number == 0 {
			// No PR for this branch
			continue
		}

		// Get local parent
		localParentName := githubParentName(eng, branch)

		if prInfo.Base == localParentName {
			continue
		}

		pending = append(pending, baseUpdate{
			branchName:      branch.GetName(),
			prNumber:        prInfo.Number,
			oldBase:         prInfo.Base,
			localParentName: localParentName,
		})
	}

	var (
		mu      sync.Mutex
		updated []string
	)
	utils.Run(pending, func(u baseUpdate) {
		out.Debug("PR for %s has base %s, but local parent is %s. Updating GitHub PR base...",
			u.branchName, u.oldBase, u.localParentName)

		updateOpts := github.UpdatePROptions{
			Base: &u.localParentName,
		}

		if _, err := githubClient.UpdatePullRequest(gctx, u.prNumber, updateOpts); err != nil {
			out.Debug("Failed to update PR base for %s: %v", u.branchName, err)
			return
		}

		out.Info("Updated PR base for %s: %s → %s",
			output.BranchName(u.branchName),
			output.Dim(u.oldBase),
			output.BranchName(u.localParentName))

		mu.Lock()
		updated = append(updated, u.branchName)
		mu.Unlock()
	})

	return &ParentsToGitHubResult{
		BranchesUpdated: updated,
	}, nil
}

// githubParentName skips hidden worktree anchors: they are local bookkeeping
// branches and can never be valid GitHub PR bases.
func githubParentName(nav engine.StackNavigator, branch engine.Branch) string {
	parent := branch.GetParent()
	visited := make(map[string]bool)
	for parent != nil {
		name := parent.GetName()
		if visited[name] {
			break
		}
		visited[name] = true
		if !parent.IsWorktreeAnchor() {
			return name
		}
		parent = parent.GetParent()
	}
	return nav.Trunk().GetName()
}
