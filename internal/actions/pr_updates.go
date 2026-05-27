package actions

import (
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/config"
	"github.com/getstackit/stackit/internal/github"
	"github.com/getstackit/stackit/internal/pr"
	"github.com/getstackit/stackit/internal/utils"
)

// UpdateStackPRMetadata updates PR titles and body footers for a list of branches
func UpdateStackPRMetadata(ctx *app.Context, branches []string, repoOwner, repoName string) {
	// Update PRs in parallel using a worker pool
	if len(branches) == 0 {
		return
	}

	utils.Run(branches, func(name string) {
		UpdateBranchPRMetadata(ctx, name, repoOwner, repoName)
	})
}

// UpdateBranchPRMetadata updates PR title and body footer for a single branch
func UpdateBranchPRMetadata(ctx *app.Context, name string, repoOwner, repoName string) {
	branch := ctx.Engine.GetBranch(name)
	prInfo, err := branch.GetPrInfo()
	if err != nil || prInfo == nil || prInfo.Number() == nil {
		ctx.Output.Debug("Skipping PR metadata update for %s: no PR info available", name)
		return
	}

	prNumber := *prInfo.Number()

	// 1. Fetch latest PR state from GitHub (Option 1)
	latestPR, err := ctx.GitHub().GetPullRequest(ctx.Context, repoOwner, repoName, prNumber)
	if err != nil {
		ctx.Output.Debug("Failed to fetch PR #%d for %s: %v", prNumber, name, err)
		return
	}

	// 2. Calculate updates
	scope := ctx.Engine.GetScope(branch)
	currentTitle := latestPR.Title
	currentBody := latestPR.Body

	updatedTitle := scope.ApplyToTitle(currentTitle)

	// Build navigation options from config
	navOpts := pr.DefaultNavigationOptions()
	if ctx.Config != nil {
		navOpts.When = ctx.Config.NavigationWhen()
		navOpts.Marker = ctx.Config.NavigationMarker()
		navOpts.Location = ctx.Config.NavigationLocation()
		navOpts.ShowMerged = ctx.Config.NavigationShowMerged()
	}

	// 1. Always handle lock section independently of navigation settings
	// This ensures lock status is shown even when navigation is hidden (single-branch stack with when=multiple)
	lockSection := pr.CreateLockSection(name, ctx.Engine)
	updatedBody := pr.UpdatePRBodyLockSection(currentBody, lockSection)

	// 2. Handle navigation footer based on settings
	// The footer now includes both the description and navigation tree in a combined section
	if navOpts.Location == config.NavigationLocationBody {
		footer := pr.CreatePRBodyFooterWithOptions(name, ctx.Engine, navOpts)
		updatedBody = pr.UpdatePRBodyFooter(updatedBody, footer)
	} else {
		// For comment/none location, strip any existing footer from body
		updatedBody = pr.StripFooter(updatedBody)
	}

	// 3. Apply updates if needed (Option 2)
	// Don't update body if:
	// - It would become empty when adding navigation (preserve existing body instead)
	// - But DO allow empty when stripping footer (switching to comment/none location)
	// Allow footer to be added even when body is empty (user expects stack information)
	shouldUpdateBody := updatedBody != currentBody
	if shouldUpdateBody && updatedBody == "" && navOpts.Location == config.NavigationLocationBody {
		// Don't clear the body when location is "body" - preserve existing content
		shouldUpdateBody = false
	}
	if updatedTitle != currentTitle || shouldUpdateBody {
		updateOpts := github.UpdatePROptions{}
		if updatedTitle != currentTitle {
			updateOpts.Title = &updatedTitle
		}
		if shouldUpdateBody {
			updateOpts.Body = &updatedBody
		}

		ctx.Output.Debug("Updating PR #%d for %s: titleChanged=%v, bodyChanged=%v", prNumber, name, updatedTitle != currentTitle, shouldUpdateBody)
		warnings, err := ctx.GitHub().UpdatePullRequest(ctx.Context, repoOwner, repoName, prNumber, updateOpts)
		if err != nil {
			ctx.Output.Debug("Failed to update PR #%d for %s: %v", prNumber, name, err)
			return
		}
		for _, w := range warnings {
			ctx.Output.Debug("PR #%d update warning: %s", prNumber, w)
		}
	} else {
		ctx.Output.Debug("PR #%d for %s already up to date", prNumber, name)
	}

	// Successfully updated (or already up to date), clear the PR body update flag and update local engine state
	_ = ctx.Engine.ClearNeedsPRBodyUpdate(name)
	_ = ctx.Engine.UpsertPrInfo(ctx.Context, branch, prInfo.WithTitleAndBody(updatedTitle, updatedBody).WithLockReason(branch.GetLockReason()))

	// Handle navigation comment based on location setting
	switch navOpts.Location {
	case config.NavigationLocationBody, config.NavigationLocationNone:
		// Delete navigation comment only if we have a cached ID (indicates we previously used comment mode)
		// This avoids unnecessary API calls when the user has always used body/none mode
		if commentID, _ := ctx.Engine.GetNavigationCommentID(branch); commentID != 0 {
			deleteNavigationComment(ctx, name, prNumber, repoOwner, repoName)
		}
	case config.NavigationLocationComment:
		// Create/update navigation comment
		updateNavigationComment(ctx, name, prNumber, repoOwner, repoName, navOpts)
	}
}

// deleteNavigationComment removes any existing navigation comment from a PR.
// Uses cached comment ID when available to avoid API search.
func deleteNavigationComment(ctx *app.Context, branchName string, prNumber int, repoOwner, repoName string) {
	branch := ctx.Engine.GetBranch(branchName)

	// Try cached comment ID first
	commentID, err := ctx.Engine.GetNavigationCommentID(branch)
	if err == nil && commentID != 0 {
		if err := ctx.GitHub().DeletePRComment(ctx.Context, repoOwner, repoName, commentID); err == nil {
			_ = ctx.Engine.ClearNavigationCommentID(branch)
			ctx.Output.Debug("Deleted navigation comment %d on PR #%d", commentID, prNumber)
			return
		}
		// If delete failed (comment already deleted externally?), clear cache and search
		_ = ctx.Engine.ClearNavigationCommentID(branch)
	}

	// Fall back to search for existing comment
	comments, err := ctx.GitHub().ListPRComments(ctx.Context, repoOwner, repoName, prNumber)
	if err != nil {
		ctx.Output.Debug("Failed to list comments on PR #%d: %v", prNumber, err)
		return
	}

	for _, c := range comments {
		if pr.IsStackitComment(c.Body) {
			if err := ctx.GitHub().DeletePRComment(ctx.Context, repoOwner, repoName, c.ID); err == nil {
				ctx.Output.Debug("Deleted navigation comment %d on PR #%d", c.ID, prNumber)
			}
			break
		}
	}
}

// updateNavigationComment manages the navigation comment on a PR.
// Creates, updates, or deletes the comment as needed based on navigation options.
// Uses cached comment ID when available to avoid API search.
func updateNavigationComment(ctx *app.Context, branchName string, prNumber int, repoOwner, repoName string, navOpts pr.NavigationOptions) {
	commentBody := pr.CreateNavigationComment(branchName, ctx.Engine, navOpts)
	branch := ctx.Engine.GetBranch(branchName)

	// If navigation should be hidden, delete existing comment
	if commentBody == "" {
		deleteNavigationComment(ctx, branchName, prNumber, repoOwner, repoName)
		return
	}

	// Try cached comment ID first
	commentID, _ := ctx.Engine.GetNavigationCommentID(branch)
	if commentID != 0 {
		// Try to update existing comment
		if err := ctx.GitHub().UpdatePRComment(ctx.Context, repoOwner, repoName, commentID, commentBody); err == nil {
			ctx.Output.Debug("Updated navigation comment %d on PR #%d", commentID, prNumber)
			return
		}
		// If update failed (comment deleted externally?), clear cache and fall through
		_ = ctx.Engine.ClearNavigationCommentID(branch)
	}

	// Search for existing comment (cache miss or stale)
	comments, err := ctx.GitHub().ListPRComments(ctx.Context, repoOwner, repoName, prNumber)
	if err != nil {
		ctx.Output.Debug("Failed to list comments on PR #%d: %v", prNumber, err)
		return
	}

	for _, c := range comments {
		if pr.IsStackitComment(c.Body) {
			// Found existing - update it and cache ID
			if err := ctx.GitHub().UpdatePRComment(ctx.Context, repoOwner, repoName, c.ID, commentBody); err == nil {
				_ = ctx.Engine.SetNavigationCommentID(branch, c.ID)
				ctx.Output.Debug("Updated navigation comment %d on PR #%d", c.ID, prNumber)
			}
			return
		}
	}

	// No existing comment - create new one and cache ID
	newID, err := ctx.GitHub().CreatePRComment(ctx.Context, repoOwner, repoName, prNumber, commentBody)
	if err == nil {
		_ = ctx.Engine.SetNavigationCommentID(branch, newID)
		ctx.Output.Debug("Created navigation comment %d on PR #%d", newID, prNumber)
	} else {
		ctx.Output.Debug("Failed to create navigation comment on PR #%d: %v", prNumber, err)
	}
}

// PushMetadataAndSyncPRs pushes metadata for the given branches to remote and updates their PRs on GitHub
func PushMetadataAndSyncPRs(ctx *app.Context, branchNames []string) error {
	if len(branchNames) == 0 {
		return nil
	}

	eng := ctx.Engine
	out := ctx.Output

	// Update LastModifiedBy for all branches (parallel with config caching)
	if err := eng.BatchSetLastModifiedBy(branchNames); err != nil {
		out.Debug("Failed to update metadata: %v", err)
	}

	// Check if remote sync is enabled; if not, run compatibility test first
	if !eng.IsRemoteSyncEnabled() {
		if err := eng.TestRemoteMetadataCompatibility(ctx.Context); err != nil {
			out.Debug("Remote metadata sync not supported: %v", err)
			return nil // Non-fatal
		}
		eng.SetRemoteSyncEnabled(true)
		// Configure refspec so future git fetch commands also fetch metadata
		if err := eng.ConfigureRemoteMetadataSync(ctx.Context); err != nil {
			out.Debug("Failed to configure metadata refspec: %v", err)
		}
	}

	// Push metadata refs
	if err := eng.PushMetadataForBranches(ctx.Context, branchNames); err != nil {
		out.Debug("Failed to push metadata refs: %v", err)
		return err
	}

	// If GitHub client is available, update PRs to trigger CI checks (and update footers/titles)
	if ctx.GitHub() != nil {
		owner, repo := ctx.GitHub().GetOwnerRepo()
		if owner != "" && repo != "" {
			UpdateStackPRMetadata(ctx, branchNames, owner, repo)
		}
	}

	return nil
}
