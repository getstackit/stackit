package merge

import (
	"context"
	"fmt"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/github"
)

// prCleanupEngine is the minimal interface needed for PR cleanup
type prCleanupEngine interface {
	GetBranch(name string) engine.Branch
	UpsertPrInfo(ctx context.Context, branch engine.Branch, prInfo *engine.PrInfo) error
}

// PRCleanupSource identifies how a consolidation happened (for footer text)
type PRCleanupSource string

const (
	// CleanupSourceConsolidate is used when consolidating a single stack
	CleanupSourceConsolidate PRCleanupSource = "consolidation"
	// CleanupSourceMultiStack is used when consolidating multiple stacks
	CleanupSourceMultiStack PRCleanupSource = "multi-stack consolidation"
)

// PRCleanupConfig configures the post-merge PR cleanup behavior
type PRCleanupConfig struct {
	// Source identifies the cleanup source for footer text
	Source PRCleanupSource

	// ConsolidationPRNumber is the PR number of the consolidation PR
	ConsolidationPRNumber int

	// UserName is the username to include in the footer (optional)
	UserName string
}

// PRCleanupResult contains the results of PR cleanup
type PRCleanupResult struct {
	ClosedPRs  []int // PR numbers that were closed
	FailedPRs  []int // PR numbers that failed to close
	SkippedPRs []int // PR numbers that were already closed
}

// ClosedCount returns the number of PRs that were closed
func (r PRCleanupResult) ClosedCount() int { return len(r.ClosedPRs) }

// FailedCount returns the number of PRs that failed to close
func (r PRCleanupResult) FailedCount() int { return len(r.FailedPRs) }

// SkippedCount returns the number of PRs that were skipped (already closed)
func (r PRCleanupResult) SkippedCount() int { return len(r.SkippedPRs) }

// PRCleaner handles post-merge cleanup of individual PRs
type PRCleaner struct {
	ctx    *app.Context
	engine prCleanupEngine
	config PRCleanupConfig
}

// NewPRCleaner creates a new PR cleaner
func NewPRCleaner(ctx *app.Context, eng prCleanupEngine, config PRCleanupConfig) *PRCleaner {
	return &PRCleaner{
		ctx:    ctx,
		engine: eng,
		config: config,
	}
}

// CleanupBranches closes PRs for the given branch names and updates their bodies with a footer
func (c *PRCleaner) CleanupBranches(ctx context.Context, branchNames []string) PRCleanupResult {
	result := PRCleanupResult{}
	out := c.ctx.Output
	githubClient := c.ctx.GitHub()

	if githubClient == nil {
		out.Debug("No GitHub client available for PR cleanup")
		return result
	}

	repoOwner, repoName := githubClient.GetOwnerRepo()
	if repoOwner == "" || repoName == "" {
		out.Debug("Could not get repo owner/name for PR cleanup")
		return result
	}

	affectedBranches := make([]string, 0, len(branchNames))

	// Fetch every PR's state and body in one GraphQL query instead of a REST
	// GetPullRequest per branch. Collect branch+prInfo pairs so we don't
	// call GetPrInfo() twice per branch.
	type branchWithPR struct {
		branch engine.Branch
		prInfo *engine.PrInfo
	}
	branchPRs := make([]branchWithPR, 0, len(branchNames))
	prNumbers := make([]int, 0, len(branchNames))
	for _, branchName := range branchNames {
		branch := c.engine.GetBranch(branchName)
		prInfo, err := branch.GetPrInfo()
		if err != nil || prInfo == nil || prInfo.Number() == nil {
			continue
		}
		branchPRs = append(branchPRs, branchWithPR{branch: branch, prInfo: prInfo})
		prNumbers = append(prNumbers, *prInfo.Number())
	}
	prDetails, err := github.BatchGetPRStateBodyGraphQL(ctx, c.ctx.Git(), repoOwner, repoName, prNumbers) //nolint:forbidigo // GitHub integration needs the git runner to run gh; not a domain bypass
	if err != nil {
		out.Debug("Failed to batch fetch PR details: %v", err)
		prDetails = map[int]github.PRStateBody{}
	}

	for _, bpr := range branchPRs {
		branch := bpr.branch
		prInfo := bpr.prInfo
		prNumber := *prInfo.Number()

		// Get current PR state/body from the batched fetch
		existingPR, ok := prDetails[prNumber]
		if !ok {
			out.Debug("Failed to get PR #%d for %s", prNumber, branch.GetName())
			continue
		}

		// Skip if already closed/merged (e.g., by merge commit strategy)
		if existingPR.State != "OPEN" {
			out.Debug("PR #%d is already %s, skipping", prNumber, existingPR.State)
			result.SkippedPRs = append(result.SkippedPRs, prNumber)
			continue
		}

		// Build and append footer to PR body
		footer := c.buildFooter()
		newBody := existingPR.Body + footer
		updateOpts := github.UpdatePROptions{Body: &newBody}

		if _, err := githubClient.UpdatePullRequest(ctx, repoOwner, repoName, prNumber, updateOpts); err != nil {
			out.Debug("Failed to update PR #%d body: %v", prNumber, err)
		} else {
			out.Debug("Updated PR #%d with consolidation footer", prNumber)
		}

		// Close the PR (handles squash/rebase merge strategies where GitHub doesn't auto-close)
		if err := githubClient.ClosePullRequest(ctx, repoOwner, repoName, prNumber); err != nil {
			out.Debug("Failed to close PR #%d: %v", prNumber, err)
			result.FailedPRs = append(result.FailedPRs, prNumber)
		} else {
			result.ClosedPRs = append(result.ClosedPRs, prNumber)
		}

		// Upsert PR info to keep metadata in sync
		if err := c.engine.UpsertPrInfo(ctx, branch, prInfo); err != nil {
			out.Debug("Failed to upsert PR info for %s: %v", branch.GetName(), err)
		}

		affectedBranches = append(affectedBranches, branch.GetName())
	}

	// Sync metadata to remote
	if len(affectedBranches) > 0 {
		if err := actions.PushMetadataAndSyncPRs(c.ctx, affectedBranches); err != nil {
			out.Debug("Failed to sync PR metadata: %v", err)
		}
	}

	return result
}

// buildFooter creates the footer text for closed PRs
func (c *PRCleaner) buildFooter() string {
	footer := fmt.Sprintf("\n\n---\n*Merged via %s into #%d", c.config.Source, c.config.ConsolidationPRNumber)
	if c.config.UserName != "" {
		footer += fmt.Sprintf(" by %s", c.config.UserName)
	}
	footer += "*"
	return footer
}

// LogResult logs the cleanup result to output
func (c *PRCleaner) LogResult(result PRCleanupResult) {
	out := c.ctx.Output
	if result.ClosedCount() > 0 {
		out.Info("Closed %d individual PR(s)", result.ClosedCount())
	}
	if result.FailedCount() > 0 {
		out.Warn("Failed to close %d PR(s): %v", result.FailedCount(), result.FailedPRs)
	}
}
