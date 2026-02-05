package sync

import (
	"fmt"
	"sync"
	"time"

	"stackit.dev/stackit/internal/actions"
	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/github"
	"stackit.dev/stackit/internal/tui/style"
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
// This is designed to run in parallel with other network operations
func syncGitHubPRInfo(ctx *app.Context) (*GitHubSyncResult, error) {
	nav := ctx.Navigator()
	gctx := ctx.Context

	setupStart := time.Now()
	allBranches := nav.AllBranches()
	branchNames := make([]string, len(allBranches))
	for i, b := range allBranches {
		branchNames[i] = b.GetName()
	}

	repoOwner, repoName, err := nav.GetRepoInfo(gctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get repository info: %w", err)
	}
	ctx.Logger.Info("github sync setup completed", "durationMs", time.Since(setupStart).Milliseconds(), "branchCount", len(branchNames))

	result := &GitHubSyncResult{
		BranchNames: branchNames,
		RepoOwner:   repoOwner,
		RepoName:    repoName,
		PRInfos:     make(map[string]*github.PullRequestInfo),
	}

	if repoOwner == "" || repoName == "" {
		return result, nil
	}

	// Sync PR info from GitHub (this is already parallelized internally)
	syncPrStart := time.Now()
	if err := github.SyncPrInfo(gctx, ctx.Git(), branchNames, repoOwner, repoName, func(name string, prInfo *github.PullRequestInfo) {
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
func processGitHubSyncResult(ctx *app.Context, result *GitHubSyncResult, dirtyAnchors map[string]bool, handler Handler) error {
	eng := ctx.PR()
	nav := ctx.Navigator()
	out := ctx.Output

	prsUpdated := 0

	// Update local PR info from GitHub results
	for name, prInfo := range result.PRInfos {
		branch := nav.GetBranch(name)

		// Try to preserve existing locked status
		lockReason := engine.LockReasonNone
		if existing, err := branch.GetPrInfo(); err == nil && existing != nil {
			lockReason = existing.LockReason()
		}

		_ = eng.UpsertPrInfo(ctx.Context, branch, engine.NewPrInfo(
			&prInfo.Number,
			prInfo.Title,
			prInfo.Body,
			prInfo.State,
			prInfo.Base,
			prInfo.HTMLURL,
			prInfo.Draft,
		).WithLockReason(lockReason))
		prsUpdated++
	}

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
	if ctx.GitHubClient != nil && result.RepoOwner != "" && result.RepoName != "" {
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
			actions.UpdateStackPRMetadata(ctx, flaggedBranches, result.RepoOwner, result.RepoName)
			ctx.Logger.Info("update stack pr metadata completed durationMs=%d branchCount=%d",
				time.Since(updateMetaStart).Milliseconds(), len(flaggedBranches))
		}
	}

	// Push local parent changes to GitHub PR bases
	// Local metadata is authoritative - if local parent differs from GitHub PR base, update GitHub
	parentsStart := time.Now()
	if ctx.GitHubClient != nil && result.RepoOwner != "" && result.RepoName != "" {
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
func PushParentsToGitHub(ctx *app.Context, result *GitHubSyncResult, dirtyAnchors map[string]bool) (*ParentsToGitHubResult, error) {
	eng := ctx.Engine
	out := ctx.Output
	gctx := ctx.Context
	githubClient := ctx.GitHubClient

	allBranches := eng.AllBranches()
	updated := []string{}

	for _, branch := range allBranches {
		if branch.IsTrunk() {
			continue
		}

		// Skip branches in dirty stacks
		if isInDirtyStack(ctx, branch.GetName(), dirtyAnchors) {
			continue
		}

		// Get the PR info we just fetched from GitHub
		prInfo, ok := result.PRInfos[branch.GetName()]
		if !ok || prInfo == nil || prInfo.Number == 0 {
			// No PR for this branch
			continue
		}

		githubBase := prInfo.Base

		// Get local parent
		currentParent := branch.GetParent()
		localParentName := ""
		if currentParent == nil {
			localParentName = eng.Trunk().GetName()
		} else {
			localParentName = currentParent.GetName()
		}

		// If local parent differs from GitHub base, update GitHub to match local
		if githubBase != localParentName {
			out.Debug("PR for %s has base %s, but local parent is %s. Updating GitHub PR base...",
				branch.GetName(), githubBase, localParentName)

			updateOpts := github.UpdatePROptions{
				Base: &localParentName,
			}

			if _, err := githubClient.UpdatePullRequest(gctx, result.RepoOwner, result.RepoName, prInfo.Number, updateOpts); err != nil {
				out.Debug("Failed to update PR base for %s: %v", branch.GetName(), err)
				continue
			}

			out.Info("Updated PR base for %s: %s → %s",
				style.ColorBranchName(branch.GetName(), false),
				style.ColorDim(githubBase),
				style.ColorBranchName(localParentName, false))

			updated = append(updated, branch.GetName())
		}
	}

	return &ParentsToGitHubResult{
		BranchesUpdated: updated,
	}, nil
}
