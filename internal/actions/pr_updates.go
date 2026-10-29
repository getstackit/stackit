package actions

import (
	"context"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/config"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/github"
	"github.com/getstackit/stackit/internal/pr"
	"github.com/getstackit/stackit/internal/workpool"
)

// PRSyncEngine lists the engine methods PRSync calls. pr.ComposeBody and
// pr.CreateNavigationComment read the stack through engine.BranchReader.
type PRSyncEngine interface {
	engine.BranchReader
	MetadataPushEngine
	UpsertPrInfo(ctx context.Context, branch engine.Branch, prInfo *engine.PrInfo) error
	ClearNeedsPRBodyUpdate(ctx context.Context, branchName string) error
	GetNavigationCommentID(branch engine.Branch) (int64, error)
	SetNavigationCommentID(ctx context.Context, branch engine.Branch, commentID int64) error
	ClearNavigationCommentID(ctx context.Context, branch engine.Branch) error
}

// PRSync pushes branch metadata and refreshes the matching PRs on GitHub:
// titles (scope prefix), body footers, and navigation comments.
type PRSync struct {
	Engine PRSyncEngine
	// GitHub returns the client, or nil when GitHub is unavailable. It is a
	// function so the client is only built when a PR is actually touched.
	GitHub   func() github.Client
	GHRunner github.GitCommandRunner
	Nav      pr.NavigationOptions
	Log      DebugLogger
}

// PRSyncFrom builds a PRSync from the command's runtime context.
func PRSyncFrom(ctx *app.Context) PRSync {
	return PRSync{Engine: ctx.Engine, GitHub: ctx.GitHub, GHRunner: ctx.GHRunner, Nav: PRNavigationOptions(ctx), Log: ctx.Output}
}

// UpdateStackPRMetadata is the *app.Context form of PRSync.UpdateStack.
func UpdateStackPRMetadata(ctx *app.Context, branches []string) {
	PRSyncFrom(ctx).UpdateStack(ctx.Context, branches)
}

// FetchPRContentForBranches is the *app.Context form of PRSync.FetchContent.
func FetchPRContentForBranches(ctx *app.Context, branches []string) map[git.PRNumber]github.PRContent {
	return PRSyncFrom(ctx).FetchContent(ctx.Context, branches)
}

// UpdateBranchPRMetadataWithContent is the *app.Context form of PRSync.UpdateBranch.
func UpdateBranchPRMetadataWithContent(ctx *app.Context, name string, current map[git.PRNumber]github.PRContent) {
	PRSyncFrom(ctx).UpdateBranch(ctx.Context, name, current)
}

// PushMetadataAndSyncPRs is the *app.Context form of PRSync.PushAndSync.
func PushMetadataAndSyncPRs(ctx *app.Context, branchNames []string) error {
	return PRSyncFrom(ctx).PushAndSync(ctx.Context, branchNames)
}

// UpdateStack updates PR titles and body footers for a list of branches
func (s PRSync) UpdateStack(ctx context.Context, branches []string) {
	if len(branches) == 0 {
		return
	}

	// Fetch every PR's current title/body in one GraphQL query instead of one
	// GetPullRequest per branch, then update each in parallel.
	current := s.FetchContent(ctx, branches)

	workpool.Run(branches, func(name string) {
		s.UpdateBranch(ctx, name, current)
	})
}

// FetchContent fetches the current title and body for the PRs of
// the given branches in a single GraphQL query, keyed by PR number. Branches
// without a known PR number are skipped. On failure it returns an empty map; the
// per-branch update then falls back to a direct GetPullRequest.
func (s PRSync) FetchContent(ctx context.Context, branches []string) map[git.PRNumber]github.PRContent {
	prNumbers := make([]git.PRNumber, 0, len(branches))
	for _, name := range branches {
		prInfo, err := s.Engine.GetBranch(name).PrInfo()
		if err != nil || prInfo == nil || prInfo.Number() == nil {
			continue
		}
		prNumbers = append(prNumbers, *prInfo.Number())
	}
	if len(prNumbers) == 0 {
		return map[git.PRNumber]github.PRContent{}
	}

	content, err := github.BatchGetPRContentGraphQL(ctx, s.GHRunner, s.GitHub().Repo(), prNumbers)
	if err != nil {
		s.Log.Debug("Failed to batch-fetch PR content: %v", err)
		return map[git.PRNumber]github.PRContent{}
	}
	return content
}

// PRNavigationOptions resolves the stack navigation settings from config,
// falling back to defaults when no config is loaded.
func PRNavigationOptions(ctx *app.Context) pr.NavigationOptions {
	navOpts := pr.DefaultNavigationOptions()
	if ctx.Config != nil {
		navOpts.When = ctx.Config.NavigationWhen()
		navOpts.Marker = ctx.Config.NavigationMarker()
		navOpts.Location = ctx.Config.NavigationLocation()
		navOpts.ShowMerged = ctx.Config.NavigationShowMerged()
	}
	return navOpts
}

// UpdateBranch updates PR title and body footer for a single
// branch, using PR content pre-fetched in bulk by FetchContent. If
// the branch's PR is not present in current (a batch miss or fetch failure), it
// falls back to a direct GetPullRequest so the freshness guarantee is preserved.
func (s PRSync) UpdateBranch(ctx context.Context, name string, current map[git.PRNumber]github.PRContent) {
	branch := s.Engine.GetBranch(name)
	prInfo, err := branch.PrInfo()
	if err != nil || prInfo == nil || prInfo.Number() == nil {
		s.Log.Debug("Skipping PR metadata update for %s: no PR info available", name)
		return
	}

	prNumber := *prInfo.Number()

	// Use the bulk-fetched current state, falling back to a single GET on a miss.
	content, ok := current[prNumber]
	if !ok {
		latestPR, err := s.GitHub().GetPullRequest(ctx, prNumber)
		if err != nil {
			s.Log.Debug("Failed to fetch PR #%d for %s: %v", prNumber, name, err)
			return
		}
		content = github.PRContent{Title: latestPR.Title, Body: latestPR.Body}
	}

	// Calculate updates
	scope := s.Engine.GetScope(branch)
	currentTitle := content.Title
	currentBody := content.Body

	updatedTitle := scope.ApplyToTitle(currentTitle)

	navOpts := s.Nav
	updatedBody := pr.ComposeBody(ctx, currentBody, name, s.Engine, navOpts)

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

		s.Log.Debug("Updating PR #%d for %s: titleChanged=%v, bodyChanged=%v", prNumber, name, updatedTitle != currentTitle, shouldUpdateBody)
		warnings, err := s.GitHub().UpdatePullRequest(ctx, prNumber, updateOpts)
		if err != nil {
			s.Log.Debug("Failed to update PR #%d for %s: %v", prNumber, name, err)
			return
		}
		for _, w := range warnings {
			s.Log.Debug("PR #%d update warning: %s", prNumber, w)
		}
	} else {
		s.Log.Debug("PR #%d for %s already up to date", prNumber, name)
	}

	// Successfully updated (or already up to date), clear the PR body update flag and update local engine state
	if err := s.Engine.ClearNeedsPRBodyUpdate(ctx, name); err != nil {
		s.Log.Debug("Failed to clear PR body update flag for %s: %v", name, err)
	}
	if err := s.Engine.UpsertPrInfo(ctx, branch, prInfo.WithTitleAndBody(updatedTitle, updatedBody).WithLockReason(branch.LockReason())); err != nil {
		s.Log.Debug("Failed to update local PR info for %s: %v", name, err)
	}

	// Handle navigation comment based on location setting
	switch navOpts.Location {
	case config.NavigationLocationBody, config.NavigationLocationNone:
		// Delete navigation comment only if we have a cached ID (indicates we previously used comment mode)
		// This avoids unnecessary API calls when the user has always used body/none mode
		if commentID, _ := s.Engine.GetNavigationCommentID(branch); commentID != 0 {
			s.deleteNavigationComment(ctx, name, prNumber)
		}
	case config.NavigationLocationComment:
		// Create/update navigation comment
		s.updateNavigationComment(ctx, name, prNumber, navOpts)
	}
}

// deleteNavigationComment removes any existing navigation comment from a PR.
// Uses cached comment ID when available to avoid API search.
func (s PRSync) deleteNavigationComment(ctx context.Context, branchName string, prNumber git.PRNumber) {
	branch := s.Engine.GetBranch(branchName)

	// Try cached comment ID first
	commentID, err := s.Engine.GetNavigationCommentID(branch)
	if err == nil && commentID != 0 {
		if err := s.GitHub().DeletePRComment(ctx, commentID); err == nil {
			if err := s.Engine.ClearNavigationCommentID(ctx, branch); err != nil {
				s.Log.Debug("Failed to clear navigation comment ID cache for %s: %v", branchName, err)
			}
			s.Log.Debug("Deleted navigation comment %d on PR #%d", commentID, prNumber)
			return
		}
		// If delete failed (comment already deleted externally?), clear cache and search
		if err := s.Engine.ClearNavigationCommentID(ctx, branch); err != nil {
			s.Log.Debug("Failed to clear navigation comment ID cache for %s: %v", branchName, err)
		}
	}

	// Fall back to search for existing comment
	comments, err := s.GitHub().ListPRComments(ctx, prNumber)
	if err != nil {
		s.Log.Debug("Failed to list comments on PR #%d: %v", prNumber, err)
		return
	}

	for _, c := range comments {
		if pr.IsStackitComment(c.Body) {
			if err := s.GitHub().DeletePRComment(ctx, c.ID); err == nil {
				s.Log.Debug("Deleted navigation comment %d on PR #%d", c.ID, prNumber)
			}
			break
		}
	}
}

// updateNavigationComment manages the navigation comment on a PR.
// Creates, updates, or deletes the comment as needed based on navigation options.
// Uses cached comment ID when available to avoid API search.
func (s PRSync) updateNavigationComment(ctx context.Context, branchName string, prNumber git.PRNumber, navOpts pr.NavigationOptions) {
	commentBody := pr.CreateNavigationComment(ctx, branchName, s.Engine, navOpts)
	branch := s.Engine.GetBranch(branchName)

	// If navigation should be hidden, delete existing comment
	if commentBody == "" {
		s.deleteNavigationComment(ctx, branchName, prNumber)
		return
	}

	// Try cached comment ID first
	commentID, _ := s.Engine.GetNavigationCommentID(branch)
	if commentID != 0 {
		// Try to update existing comment
		if err := s.GitHub().UpdatePRComment(ctx, commentID, commentBody); err == nil {
			s.Log.Debug("Updated navigation comment %d on PR #%d", commentID, prNumber)
			return
		}
		// If update failed (comment deleted externally?), clear cache and fall through
		if err := s.Engine.ClearNavigationCommentID(ctx, branch); err != nil {
			s.Log.Debug("Failed to clear navigation comment ID cache for %s: %v", branchName, err)
		}
	}

	// Search for existing comment (cache miss or stale)
	comments, err := s.GitHub().ListPRComments(ctx, prNumber)
	if err != nil {
		s.Log.Debug("Failed to list comments on PR #%d: %v", prNumber, err)
		return
	}

	for _, c := range comments {
		if pr.IsStackitComment(c.Body) {
			// Found existing - update it and cache ID
			if err := s.GitHub().UpdatePRComment(ctx, c.ID, commentBody); err == nil {
				if err := s.Engine.SetNavigationCommentID(ctx, branch, c.ID); err != nil {
					s.Log.Debug("Failed to cache navigation comment ID for %s: %v", branchName, err)
				}
				s.Log.Debug("Updated navigation comment %d on PR #%d", c.ID, prNumber)
			}
			return
		}
	}

	// No existing comment - create new one and cache ID
	newID, err := s.GitHub().CreatePRComment(ctx, prNumber, commentBody)
	if err == nil {
		if err := s.Engine.SetNavigationCommentID(ctx, branch, newID); err != nil {
			s.Log.Debug("Failed to cache navigation comment ID for %s: %v", branchName, err)
		}
		s.Log.Debug("Created navigation comment %d on PR #%d", newID, prNumber)
	} else {
		s.Log.Debug("Failed to create navigation comment on PR #%d: %v", prNumber, err)
	}
}

// PushAndSync pushes metadata for the given branches to remote and updates their PRs on GitHub
func (s PRSync) PushAndSync(ctx context.Context, branchNames []string) error {
	if len(branchNames) == 0 {
		return nil
	}

	eng, out := s.Engine, s.Log

	// Update LastModifiedBy for all branches in one metadata commit
	if err := eng.BatchSetLastModifiedBy(ctx, branchNames); err != nil {
		out.Debug("Failed to update metadata: %v", err)
	}

	if err := eng.PrepareRemoteMetadataPush(ctx); err != nil {
		out.Debug("Remote metadata sync not supported: %v", err)
		return nil // Non-fatal
	}

	// Push metadata refs
	if err := eng.PushMetadataForBranches(ctx, branchNames); err != nil {
		out.Debug("Failed to push metadata refs: %v", err)
		return err
	}

	// If GitHub client is available, update PRs to trigger CI checks (and update footers/titles)
	if s.GitHub() != nil {
		repo := s.GitHub().Repo()
		if repo.Owner != "" && repo.Name != "" {
			s.UpdateStack(ctx, branchNames)
		}
	}

	return nil
}
