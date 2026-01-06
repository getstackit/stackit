package actions

import (
	"fmt"
	"regexp"
	"strings"

	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/github"
	"stackit.dev/stackit/internal/utils"
)

var scopeRegex = regexp.MustCompile(`^\[[^\]]+\]\s*`)

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
		return
	}

	prNumber := *prInfo.Number()

	// 1. Fetch latest PR state from GitHub (Option 1)
	latestPR, err := ctx.GitHubClient.GetPullRequest(ctx.Context, repoOwner, repoName, prNumber)
	if err != nil {
		return
	}

	// 2. Calculate updates
	scope := ctx.Engine.GetScope(branch)
	currentTitle := latestPR.Title
	currentBody := latestPR.Body

	updatedTitle := currentTitle
	if !scope.IsEmpty() {
		if scopeRegex.MatchString(updatedTitle) {
			if !strings.HasPrefix(strings.ToUpper(updatedTitle), "["+strings.ToUpper(scope.String())+"]") {
				updatedTitle = scopeRegex.ReplaceAllString(updatedTitle, "["+scope.String()+"] ")
			}
		} else {
			updatedTitle = fmt.Sprintf("[%s] %s", scope.String(), updatedTitle)
		}
	}

	footer := CreatePRBodyFooter(name, ctx.Engine)
	updatedBody := UpdatePRBodyFooter(currentBody, footer)

	// 3. Apply updates if needed (Option 2)
	// Don't update body if:
	// - It would become empty (preserve existing body instead)
	// Allow footer to be added even when body is empty (user expects stack information)
	shouldUpdateBody := updatedBody != currentBody && updatedBody != ""
	if updatedTitle != currentTitle || shouldUpdateBody {
		updateOpts := github.UpdatePROptions{}
		if updatedTitle != currentTitle {
			updateOpts.Title = &updatedTitle
		}
		if shouldUpdateBody {
			updateOpts.Body = &updatedBody
		}

		err = ctx.GitHubClient.UpdatePullRequest(ctx.Context, repoOwner, repoName, prNumber, updateOpts)
		if err != nil {
			return
		}
	}

	// Successfully updated (or already up to date), update local engine state
	_ = ctx.Engine.UpsertPrInfo(branch, prInfo.WithTitleAndBody(updatedTitle, updatedBody).WithLockReason(branch.GetLockReason()))
}

// PushMetadataAndSyncPRs pushes metadata for the given branches to remote and updates their PRs on GitHub
func PushMetadataAndSyncPRs(ctx *app.Context, branchNames []string) error {
	if len(branchNames) == 0 {
		return nil
	}

	eng := ctx.Engine
	out := ctx.Output

	// Update LastModifiedBy for each branch
	for _, branchName := range branchNames {
		if err := eng.SetLastModifiedBy(branchName); err != nil {
			out.Debug("Failed to update metadata for %s: %v", branchName, err)
			continue
		}
	}

	// Check if remote sync is enabled; if not, run compatibility test first
	if !eng.IsRemoteSyncEnabled() {
		if err := eng.Git().TestRemoteRefCompatibility(); err != nil {
			out.Debug("Remote metadata sync not supported: %v", err)
			return nil // Non-fatal
		}
		eng.SetRemoteSyncEnabled(true)
		// Configure refspec so future git fetch commands also fetch metadata
		if err := eng.Git().EnsureMetadataRefspecConfigured(); err != nil {
			out.Debug("Failed to configure metadata refspec: %v", err)
		}
	}

	// Push metadata refs
	if err := eng.Git().PushMetadataRefs(branchNames); err != nil {
		out.Debug("Failed to push metadata refs: %v", err)
		return err
	}

	// If GitHub client is available, update PRs to trigger CI checks (and update footers/titles)
	if ctx.GitHubClient != nil {
		owner, repo := ctx.GitHubClient.GetOwnerRepo()
		if owner != "" && repo != "" {
			UpdateStackPRMetadata(ctx, branchNames, owner, repo)
		}
	}

	return nil
}
