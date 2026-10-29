package sync

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/getstackit/stackit/internal/workpool"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/github"
	"github.com/getstackit/stackit/internal/output"
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
// remoteCtx governs every network call; deps only supply the navigator, logger,
// and git runner.
func syncGitHubPRInfo(remoteCtx context.Context, deps Deps) (*GitHubSyncResult, error) {
	nav := deps.Engine

	setupStart := time.Now()
	allBranches := nav.AllBranches()
	branchNames := allBranches.Names()

	repo, err := nav.GetRepoInfo(remoteCtx)
	if err != nil {
		return nil, fmt.Errorf("failed to get repository info: %w", err)
	}
	deps.Log.Info("github sync setup completed durationMs=%v branchCount=%v", time.Since(setupStart).Milliseconds(), len(branchNames))

	result := &GitHubSyncResult{
		BranchNames: branchNames,
		RepoOwner:   repo.Owner,
		RepoName:    repo.Name,
		PRInfos:     make(map[string]*github.PullRequestInfo),
	}

	if !repo.IsHosted() {
		return result, nil
	}

	// Keep the recorded PR numbers so a branch deleted on GitHub can still be
	// resolved to its closed PR. A missing remote ref by itself is not enough to
	// delete local work; this only supplies GitHub's PR state to normal cleanup.
	knownPRNumbers := make(map[string]git.PRNumber)
	for _, branch := range allBranches {
		prInfo, err := branch.PrInfo()
		if err != nil || prInfo == nil || prInfo.Number() == nil || *prInfo.Number() <= 0 {
			continue
		}
		knownPRNumbers[branch.Name()] = *prInfo.Number()
	}

	// Sync PR info from GitHub (this is already parallelized internally)
	syncPrStart := time.Now()
	if err := github.SyncPrInfoWithKnownPRNumbers(remoteCtx, deps.GHRunner, branchNames, github.Repo{Owner: repo.Owner, Name: repo.Name}, knownPRNumbers, func(name string, prInfo *github.PullRequestInfo) {
		result.mu.Lock()
		result.PRInfos[name] = prInfo
		result.mu.Unlock()
	}); err != nil {
		return nil, fmt.Errorf("failed to sync PR info from GitHub: %w", err)
	}
	deps.Log.Info("sync pr info from github completed durationMs=%d prsUpdated=%d", time.Since(syncPrStart).Milliseconds(), len(result.PRInfos))

	return result, nil
}

// processGitHubSyncResult processes GitHub PR info after the network operation completes
// This must run after syncGitHubPRInfo completes
//
//nolint:unparam // error return is for future error handling
func processGitHubSyncResult(ctx context.Context, deps Deps, result *GitHubSyncResult, dirtyAnchors dirtyAnchorSet, handler Handler) error {
	eng := deps.Engine
	out := deps.Out

	// Update local PR info from GitHub results, preserving lock reasons.
	// Collect into a map first so we can write all updates in one atomic batch.
	updates := make(map[string]*engine.PrInfo, len(result.PRInfos))
	for name, prInfo := range result.PRInfos {
		branch := eng.GetBranch(name)

		lockReason := engine.LockReasonNone
		if existing, err := branch.PrInfo(); err == nil && existing != nil {
			lockReason = existing.LockReason()
		}

		updates[name] = engine.NewPrInfo(engine.PrInfoFields{
			Number:  &prInfo.Number,
			Title:   prInfo.Title,
			Body:    prInfo.Body,
			State:   prInfo.State,
			Base:    prInfo.Base,
			URL:     prInfo.HTMLURL,
			IsDraft: prInfo.Draft,
		}).WithLockReason(lockReason)
	}

	if err := eng.BatchUpsertPrInfo(ctx, updates); err != nil {
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
	if deps.GitHub() != nil && result.RepoOwner != "" && result.RepoName != "" {
		flaggedBranches := eng.BranchesNeedingPRBodyUpdate()
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
			deps.PRSync.UpdateStack(ctx, flaggedBranches)
			deps.Log.Info("update stack pr metadata completed durationMs=%d branchCount=%d",
				time.Since(updateMetaStart).Milliseconds(), len(flaggedBranches))
		}
	}

	// Push local parent changes to GitHub PR bases
	// Local metadata is authoritative - if local parent differs from GitHub PR base, update GitHub
	parentsStart := time.Now()
	if deps.GitHub() != nil && result.RepoOwner != "" && result.RepoName != "" {
		syncResult, err := PushParentsToGitHub(ctx, deps, result, dirtyAnchors)
		deps.Log.Info("sync parents to github completed durationMs=%d", time.Since(parentsStart).Milliseconds())
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
func PushParentsToGitHub(ctx context.Context, deps Deps, result *GitHubSyncResult, dirtyAnchors dirtyAnchorSet) (*ParentsToGitHubResult, error) {
	eng := deps.Engine
	out := deps.Out
	githubClient := deps.GitHub()

	allBranches := eng.AllBranches()

	type baseUpdate struct {
		branchName      string
		prNumber        git.PRNumber
		oldBase         string
		localParentName string
	}

	var pending []baseUpdate
	for _, branch := range allBranches {
		if branch.IsTrunk() {
			continue
		}

		// Skip branches in dirty stacks
		if dirtyAnchors.includes(eng, branch.Name()) {
			continue
		}

		// Get the PR info we just fetched from GitHub
		prInfo, ok := result.PRInfos[branch.Name()]
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
			branchName:      branch.Name(),
			prNumber:        prInfo.Number,
			oldBase:         prInfo.Base,
			localParentName: localParentName,
		})
	}

	var (
		mu      sync.Mutex
		updated []string
	)
	workpool.Run(pending, func(u baseUpdate) {
		out.Debug("PR for %s has base %s, but local parent is %s. Updating GitHub PR base...",
			u.branchName, u.oldBase, u.localParentName)

		updateOpts := github.UpdatePROptions{
			Base: &u.localParentName,
		}

		if _, err := githubClient.UpdatePullRequest(ctx, u.prNumber, updateOpts); err != nil {
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
	parent := branch.Parent()
	visited := make(map[string]bool)
	for parent != nil {
		name := parent.Name()
		if visited[name] {
			break
		}
		visited[name] = true
		if !parent.IsWorktreeAnchor() {
			return name
		}
		parent = parent.Parent()
	}
	return nav.Trunk().Name()
}
