package sync

import (
	"fmt"

	"stackit.dev/stackit/internal/actions"
	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/github"
	"stackit.dev/stackit/internal/tui/style"
)

// syncGitHubInfo synchronizes PR information from GitHub and updates local parents
func syncGitHubInfo(ctx *app.Context, branchesToRestack *[]string, handler Handler, _ *Summary) error {
	eng := ctx.PR()
	nav := ctx.Navigator()
	splog := ctx.Splog
	gctx := ctx.Context

	// Sync PR info
	allBranches := nav.AllBranches()
	branchNames := make([]string, len(allBranches))
	for i, b := range allBranches {
		branchNames[i] = b.GetName()
	}

	repoOwner, repoName, err := nav.GetRepoInfo(gctx)
	if err != nil {
		return fmt.Errorf("failed to get repository info: %w", err)
	}
	if repoOwner != "" && repoName != "" {
		prsUpdated := 0
		if err := github.SyncPrInfo(gctx, ctx.Git(), branchNames, repoOwner, repoName, func(name string, prInfo *github.PullRequestInfo) {
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
		}); err != nil {
			// GitHub failure aborts sync (per spec)
			return fmt.Errorf("failed to sync PR info from GitHub: %w", err)
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
		if ctx.GitHubClient != nil {
			actions.UpdateStackPRMetadata(ctx, branchNames, repoOwner, repoName)
		}
	}

	// Synchronize local parents with GitHub PR base branches
	// This can happen even if we couldn't sync with GitHub just now,
	// using the metadata already stored in the engine.
	syncResult, err := ParentsFromGitHubBase(ctx)
	if err != nil {
		splog.Debug("Failed to sync parents from GitHub: %v", err)
	} else if len(syncResult.BranchesReparented) > 0 {
		// Add reparented branches to restack list
		for _, branchName := range syncResult.BranchesReparented {
			*branchesToRestack = append(*branchesToRestack, branchName)
			// Also add descendants
			branch := nav.GetBranch(branchName)
			upstack := branch.GetRelativeStackUpstack()
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
	splog := ctx.Splog
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
						splog.Debug("GitHub PR for %s has base %s, which is an ancestor of local parent %s. "+
							"Branch is empty relative to its parent, so keeping the more specific local parent.",
							branch.GetName(), githubBase, currentParentName)
						continue
					}
				}
			}

			splog.Info("GitHub PR for %s has base %s, but local parent is %s. Updating local parent...",
				style.ColorBranchName(branch.GetName(), false),
				style.ColorBranchName(githubBase, false),
				style.ColorBranchName(currentParentName, false))

			if err := eng.SetParent(gctx, branch, eng.GetBranch(githubBase)); err != nil {
				splog.Debug("Failed to update parent for %s: %v", branch.GetName(), err)
				continue
			}

			reparented = append(reparented, branch.GetName())
		}
	}

	return &ParentsResult{
		BranchesReparented: reparented,
	}, nil
}
