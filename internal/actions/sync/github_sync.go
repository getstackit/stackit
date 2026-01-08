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
func processGitHubSyncResult(ctx *app.Context, result *GitHubSyncResult, branchesToRestack *[]string, handler Handler) error {
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

		_ = eng.UpsertPrInfo(branch, engine.NewPrInfo(
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

	// Update PR body footers if needed
	if ctx.GitHubClient != nil && result.RepoOwner != "" && result.RepoName != "" {
		updateMetaStart := time.Now()
		actions.UpdateStackPRMetadata(ctx, result.BranchNames, result.RepoOwner, result.RepoName)
		ctx.Logger.Info("update stack pr metadata completed durationMs=%d", time.Since(updateMetaStart).Milliseconds())
	}

	// Synchronize local parents with GitHub PR base branches
	parentsStart := time.Now()
	syncResult, err := ParentsFromGitHubBase(ctx)
	ctx.Logger.Info("sync parents from github base completed durationMs=%d", time.Since(parentsStart).Milliseconds())
	if err != nil {
		out.Debug("Failed to sync parents from GitHub: %v", err)
	} else if len(syncResult.BranchesReparented) > 0 {
		graph := engine.BuildStackGraph(ctx.Engine, engine.SortStrategyAlphabetical, nil)
		// Add reparented branches to restack list
		for _, branchName := range syncResult.BranchesReparented {
			*branchesToRestack = append(*branchesToRestack, branchName)
			// Also add descendants
			branch := nav.GetBranch(branchName)
			upstack := graph.Range(branch, engine.StackRange{
				RecursiveChildren: true,
			})
			for _, b := range upstack {
				*branchesToRestack = append(*branchesToRestack, b.GetName())
			}
		}
	}

	return nil
}

// ParentsFromGitHubBase synchronizes local parents with GitHub PR base branches
func ParentsFromGitHubBase(ctx *app.Context) (*ParentsResult, error) {
	eng := ctx.Engine // Using Engine here because it needs multiple interfaces (Status, Navigator, Differ, Tracking)
	out := ctx.Output
	gctx := ctx.Context

	allBranches := eng.AllBranches()
	reparented := []string{}

	// Map of all local branches for quick lookup
	localBranches := make(map[string]bool)
	for _, b := range allBranches {
		localBranches[b.GetName()] = true
	}
	localBranches[eng.Trunk().GetName()] = true

	for _, branch := range allBranches {
		if branch.IsTrunk() {
			continue
		}

		prInfo, err := branch.GetPrInfo()
		if err != nil || prInfo == nil || prInfo.Base() == "" {
			continue
		}

		currentParent := branch.GetParent()
		currentParentName := ""
		if currentParent == nil {
			currentParentName = eng.Trunk().GetName()
		} else {
			currentParentName = currentParent.GetName()
		}

		githubBase := prInfo.Base()

		// If GitHub base is different from local parent, and GitHub base is a valid local branch
		if githubBase != currentParentName && localBranches[githubBase] {
			// Before reparenting to match GitHub, check if the GitHub base is an
			// ancestor of our current local parent.
			if currentParentName != eng.Trunk().GetName() {
				isAncestor, err := eng.IsAncestor(githubBase, currentParentName)
				if err == nil && isAncestor {
					// If GitHub base is an ancestor, it's a "downgrade" in specificity.
					// We only skip reparenting if the branch is EMPTY relative to its current parent.
					// This handles the "submit" bug in diamond structures.
					isEmpty, err := eng.IsBranchEmpty(gctx, branch.GetName())
					if err == nil && isEmpty {
						out.Debug("GitHub PR for %s has base %s, which is an ancestor of local parent %s. "+
							"Branch is empty relative to its parent, so keeping the more specific local parent.",
							branch.GetName(), githubBase, currentParentName)
						continue
					}
				}
			}

			out.Info("GitHub PR for %s has base %s, but local parent is %s. Updating local parent...",
				style.ColorBranchName(branch.GetName(), false),
				style.ColorBranchName(githubBase, false),
				style.ColorBranchName(currentParentName, false))

			if err := eng.SetParent(gctx, branch, eng.GetBranch(githubBase)); err != nil {
				out.Debug("Failed to update parent for %s: %v", branch.GetName(), err)
				continue
			}

			reparented = append(reparented, branch.GetName())
		}
	}

	return &ParentsResult{
		BranchesReparented: reparented,
	}, nil
}
