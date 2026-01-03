package merge

import (
	"context"
	"fmt"
	"strings"
	"time"

	"stackit.dev/stackit/internal/actions"
	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/git"
	"stackit.dev/stackit/internal/github"
)

// ConsolidationResult contains information about a completed consolidation
type ConsolidationResult struct {
	BranchName string
	PRNumber   int
	PRURL      string
}

// ConsolidateMergeExecutor handles stack consolidation merging
type ConsolidateMergeExecutor struct {
	plan              *Plan
	engine            mergeExecuteEngine
	ctx               *app.Context
	consolidationPR   *github.PullRequestInfo // Set after consolidation PR is merged
	consolidationUser string                  // GitHub username who performed the consolidation
}

// NewConsolidateMergeExecutor creates a new consolidation executor
func NewConsolidateMergeExecutor(plan *Plan, engine mergeExecuteEngine, ctx *app.Context) *ConsolidateMergeExecutor {
	return &ConsolidateMergeExecutor{
		plan:   plan,
		engine: engine,
		ctx:    ctx,
	}
}

// Execute performs stack consolidation merging
func (c *ConsolidateMergeExecutor) Execute(ctx context.Context, opts ExecuteOptions) (*ConsolidationResult, error) {
	splog := c.ctx.Splog
	splog.Info("🔀 Starting stack consolidation merge...")

	if err := c.preValidateStack(ctx, opts.Force); err != nil {
		return nil, fmt.Errorf("pre-validation failed: %w", err)
	}

	splog.Info("Stack to consolidate:")
	for i, branchInfo := range c.plan.BranchesToMerge {
		symbol := "  ○"
		if i == len(c.plan.BranchesToMerge)-1 {
			symbol = "  ◉"
		}
		splog.Info("%s %s PR #%d", symbol, branchInfo.BranchName, branchInfo.PRNumber)
	}
	splog.Info("    ↓")
	splog.Info("  📦 Consolidated PR")
	splog.Newline()

	consolidationBranch, err := c.createMergeBranch(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create consolidation branch: %w", err)
	}

	pr, err := c.createConsolidationPR(ctx, consolidationBranch)
	if err != nil {
		return nil, fmt.Errorf("failed to create consolidation PR: %w", err)
	}

	splog.Info("✅ Created consolidation branch: %s", consolidationBranch)

	// Lock individual PRs and update them with a notice
	if err := c.lockAndNotifyIndividualPRs(ctx, consolidationBranch); err != nil {
		splog.Warn("Failed to lock and notify individual PRs: %v", err)
	}

	if err := c.waitForConsolidationMerge(ctx, consolidationBranch, pr); err != nil {
		return nil, fmt.Errorf("consolidation merge failed: %w", err)
	}

	// Store consolidation info for footer updates
	c.consolidationPR = pr
	if userName, err := git.NewRunner().GetUserName(ctx); err == nil && userName != "" {
		c.consolidationUser = userName
	}

	if err := c.postMergeCleanup(ctx); err != nil {
		splog.Warn("Post-merge cleanup had issues: %v", err)
	}

	splog.Info("🎉 Stack consolidation merge completed successfully!")

	result := &ConsolidationResult{
		BranchName: consolidationBranch,
		PRNumber:   pr.Number,
		PRURL:      pr.HTMLURL,
	}
	return result, nil
}

// preValidateStack ensures all PRs are ready for consolidation
func (c *ConsolidateMergeExecutor) preValidateStack(ctx context.Context, force bool) error {
	splog := c.ctx.Splog

	// We trust the plan created by CreateMergePlan for PR status and remote matching
	// unless --force is used to bypass those checks entirely.
	// We only re-validate if we're not forced and want to be absolutely sure.
	if !force {
		for _, branchInfo := range c.plan.BranchesToMerge {
			// Quick local checks
			if !branchInfo.MatchesRemote {
				return fmt.Errorf("branch %s differs from remote, use --force to proceed", branchInfo.BranchName)
			}
			splog.Debug("✅ Branch %s is ready for consolidation (from plan)", branchInfo.BranchName)
		}
	}

	// This is the only "heavy" operation that really needs to happen here
	pullResult, err := c.engine.PullTrunk(ctx)
	if err != nil {
		return fmt.Errorf("failed to update trunk: %w", err)
	}
	if pullResult == engine.PullConflict {
		return fmt.Errorf("trunk has conflicts with remote")
	}

	return nil
}

// createMergeBranch creates a branch containing all stack commits
func (c *ConsolidateMergeExecutor) createMergeBranch(ctx context.Context) (string, error) {
	splog := c.ctx.Splog
	// Generate unique branch name
	timestamp := time.Now().Unix()
	scope := c.getStackScope()
	branchName := fmt.Sprintf("stack-merge-%s-%d", scope, timestamp)

	splog.Info("📋 Creating merge branch: %s", branchName)

	if err := c.engine.CreateAndCheckoutBranch(ctx, c.engine.GetBranch(branchName)); err != nil {
		return "", fmt.Errorf("failed to create and checkout branch: %w", err)
	}

	// Reset to trunk since CreateAndCheckoutBranch creates from current HEAD
	if err := c.engine.ResetHard(ctx, c.engine.Trunk().GetName()); err != nil {
		return "", fmt.Errorf("failed to reset to trunk: %w", err)
	}

	// Merge all stack branches with --no-ff to preserve branch structure and enable auto-closing
	for i, branchInfo := range c.plan.BranchesToMerge {
		splog.Info("  Merging %s (%d/%d)...", branchInfo.BranchName, i+1, len(c.plan.BranchesToMerge))
		splog.Debug("Consolidation merge: merging %s", branchInfo.BranchName)

		commitMsg := fmt.Sprintf("Consolidate %s: %s", branchInfo.BranchName, c.getBranchTitle(branchInfo))
		if err := c.engine.Merge(ctx, branchInfo.BranchName, engine.MergeOptions{NoFF: true, Message: commitMsg}); err != nil {
			splog.Debug("Consolidation merge failed for %s: %v", branchInfo.BranchName, err)
			return "", fmt.Errorf("failed to merge %s: %w", branchInfo.BranchName, err)
		}
	}

	if err := c.engine.PushBranch(ctx, branchName, c.engine.GetRemote(), git.PushOptions{
		Force:    false,
		NoVerify: true,
	}); err != nil {
		return "", fmt.Errorf("failed to push consolidation branch %s: %w", branchName, err)
	}

	splog.Info("✅ Merge branch created and pushed")
	return branchName, nil
}

func (c *ConsolidateMergeExecutor) createConsolidationPR(ctx context.Context, branchName string) (*github.PullRequestInfo, error) {
	scope := c.getStackScope()
	title := fmt.Sprintf("[%s] Consolidate stack: %s", scope, c.getConsolidationTitle())

	body := c.buildConsolidationPRBody()

	owner, repo := c.getOwnerRepo()
	opts := github.CreatePROptions{
		Title: title,
		Body:  body,
		Head:  branchName,
		Base:  c.engine.Trunk().GetName(),
		Draft: false,
	}

	pr, err := c.ctx.GitHubClient.CreatePullRequest(ctx, owner, repo, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create PR: %w", err)
	}

	return pr, nil
}

// waitForConsolidationCI waits for CI checks to pass on the consolidation PR
func (c *ConsolidateMergeExecutor) waitForConsolidationCI(ctx context.Context, branchName string, prNumber int) error {
	githubClient := c.ctx.GitHubClient
	splog := c.ctx.Splog
	if githubClient == nil {
		return fmt.Errorf("GitHub client not available")
	}

	timeout := 10 * time.Minute // Default timeout for consolidation CI
	pollInterval := 15 * time.Second
	startTime := time.Now()
	deadline := startTime.Add(timeout)

	// Determine if we expect checks based on whether any constituent branches had them
	expectChecks := false
	for _, b := range c.plan.BranchesToMerge {
		if b.ChecksStatus != ChecksNone {
			expectChecks = true
			break
		}
	}

	if expectChecks {
		splog.Info("   Waiting for CI checks to register...")
		// Give GitHub a moment to register the PR and start CI
		time.Sleep(5 * time.Second)
	}

	splog.Info("   Waiting for CI checks (timeout: %v)...", timeout)

	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for CI checks on consolidation PR #%d after %v", prNumber, timeout)
		}

		status, err := githubClient.GetPRChecksStatus(ctx, branchName)
		if err != nil {
			splog.Debug("Error checking CI status: %v", err)
		} else if status != nil {
			if !status.Passing {
				return fmt.Errorf("CI checks failed on consolidation PR #%d", prNumber)
			}

			// If we expect checks but none have appeared yet, treat as pending
			isReallyPending := status.Pending
			if expectChecks && len(status.Checks) == 0 {
				isReallyPending = true
				splog.Debug("No checks found yet for PR #%d, still waiting...", prNumber)
			}

			if !isReallyPending {
				elapsed := time.Since(startTime)
				splog.Info("✅ CI checks passed on consolidation PR #%d after %v", prNumber, elapsed.Round(time.Second))
				return nil
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

// waitForConsolidationMerge waits for CI to pass and auto-merges the consolidation PR
func (c *ConsolidateMergeExecutor) waitForConsolidationMerge(ctx context.Context, branchName string, pr *github.PullRequestInfo) error {
	splog := c.ctx.Splog
	splog.Info("Consolidation PR:")
	splog.Info("  ◉ %s PR #%d ⏳", branchName, pr.Number)
	splog.Info("     %s", pr.HTMLURL)
	splog.Info("     Waiting for CI checks to pass...")

	if err := c.waitForConsolidationCI(ctx, branchName, pr.Number); err != nil {
		return fmt.Errorf("CI checks failed for consolidation PR: %w", err)
	}

	splog.Info("Consolidation PR:")
	splog.Info("  ◉ %s PR #%d ✓", branchName, pr.Number)
	splog.Info("     Auto-merging...")

	if err := c.ctx.GitHubClient.MergePullRequest(ctx, branchName); err != nil {
		return fmt.Errorf("failed to auto-merge consolidation PR #%d: %w", pr.Number, err)
	}

	splog.Info("✅ Consolidation PR #%d has been merged automatically!", pr.Number)

	splog.Info("Consolidation complete:")
	splog.Info("  ✓ %s (merged)", branchName)

	return nil
}

func (c *ConsolidateMergeExecutor) postMergeCleanup(ctx context.Context) error {
	c.ctx.Splog.Info("🧹 Running post-merge cleanup...")

	c.updateIndividualPRs()

	if err := c.restackRemainingBranches(ctx); err != nil {
		return fmt.Errorf("failed to restack branches: %w", err)
	}

	return nil
}

func (c *ConsolidateMergeExecutor) updateIndividualPRs() {
	splog := c.ctx.Splog
	ctx := c.ctx.Context
	githubClient := c.ctx.GitHubClient

	// Skip if we don't have consolidation PR info
	if c.consolidationPR == nil || c.consolidationPR.Number == 0 {
		splog.Debug("No consolidation PR info available for footer updates")
		return
	}

	repoOwner, repoName := githubClient.GetOwnerRepo()
	if repoOwner == "" || repoName == "" {
		splog.Debug("Could not get repo owner/name for PR updates")
		return
	}

	affectedBranches := []string{}
	for _, branchInfo := range c.plan.BranchesToMerge {
		branch := c.engine.GetBranch(branchInfo.BranchName)
		prInfo, err := branch.GetPrInfo()
		if err != nil || prInfo == nil || prInfo.Number() == nil {
			continue
		}

		// Get current PR body to append footer
		existingPR, err := githubClient.GetPullRequest(ctx, repoOwner, repoName, *prInfo.Number())
		if err != nil {
			splog.Debug("Failed to get PR #%d for %s: %v", *prInfo.Number(), branchInfo.BranchName, err)
			continue
		}

		// Build footer
		footer := fmt.Sprintf("\n\n---\n*Merged via consolidation into #%d", c.consolidationPR.Number)
		if c.consolidationUser != "" {
			footer += fmt.Sprintf(" by %s", c.consolidationUser)
		}
		footer += "*"

		// Append footer to existing body
		newBody := existingPR.Body + footer
		updateOpts := github.UpdatePROptions{
			Body: &newBody,
		}

		if err := githubClient.UpdatePullRequest(ctx, repoOwner, repoName, *prInfo.Number(), updateOpts); err != nil {
			splog.Debug("Failed to update PR #%d body: %v", *prInfo.Number(), err)
		} else {
			splog.Debug("Updated PR #%d with consolidation footer", *prInfo.Number())
		}

		// Keep the consolidation PR number so the footer knows it was consolidated
		_ = c.engine.UpsertPrInfo(branch, prInfo) // Just re-upsert to trigger update if needed
		affectedBranches = append(affectedBranches, branchInfo.BranchName)
	}

	if len(affectedBranches) > 0 {
		if err := actions.PushMetadataAndSyncPRs(c.ctx, affectedBranches); err != nil {
			splog.Warn("Failed to sync individual PRs: %v", err)
		}
	}
}

func (c *ConsolidateMergeExecutor) lockAndNotifyIndividualPRs(_ context.Context, consolidationBranch string) error {
	splog := c.ctx.Splog
	splog.Info("🔒 Locking individual PRs and updating status...")

	branchesToLock := []engine.Branch{}
	branchNames := []string{}
	for _, b := range c.plan.BranchesToMerge {
		branch := c.engine.GetBranch(b.BranchName)
		if !branch.IsLocked() {
			branchesToLock = append(branchesToLock, branch)
		}
		branchNames = append(branchNames, b.BranchName)
	}

	if len(branchesToLock) > 0 {
		if _, err := c.engine.SetLocked(branchesToLock, engine.LockReasonConsolidating); err != nil {
			return fmt.Errorf("failed to lock branches: %w", err)
		}
	}

	for _, b := range branchesToLock {
		prInfo, _ := b.GetPrInfo()
		if prInfo != nil {
			_ = c.engine.UpsertPrInfo(b, prInfo.WithMergeBranch(consolidationBranch))
		}
	}

	if err := actions.PushMetadataAndSyncPRs(c.ctx, branchNames); err != nil {
		splog.Warn("Failed to sync individual PRs: %v", err)
	}

	return nil
}

func (c *ConsolidateMergeExecutor) restackRemainingBranches(ctx context.Context) error {
	splog := c.ctx.Splog
	if _, err := c.engine.PullTrunk(ctx); err != nil {
		return err
	}

	if len(c.plan.UpstackBranches) == 0 {
		return nil
	}

	branches := make([]engine.Branch, len(c.plan.UpstackBranches))
	for i, name := range c.plan.UpstackBranches {
		branches[i] = c.engine.GetBranch(name)
	}

	batchResult, err := c.engine.RestackBranches(ctx, branches)
	if err != nil {
		if batchResult.ConflictBranch != "" {
			splog.Warn("Conflict restacking %s - manual resolution needed", batchResult.ConflictBranch)
			return nil // Don't fail the whole cleanup for a conflict
		}
		return fmt.Errorf("failed to restack branches: %w", err)
	}

	return nil
}

// Helper methods

func (c *ConsolidateMergeExecutor) getStackScope() string {
	// Get scope from the first branch in the stack
	if len(c.plan.BranchesToMerge) > 0 {
		branch := c.engine.GetBranch(c.plan.BranchesToMerge[0].BranchName)
		scope := c.engine.GetScope(branch)
		if !scope.IsEmpty() {
			return scope.String()
		}
	}
	return "stack"
}

func (c *ConsolidateMergeExecutor) getBranchTitle(branchInfo BranchMergeInfo) string {
	branch := c.engine.GetBranch(branchInfo.BranchName)
	prInfo, _ := branch.GetPrInfo()
	if prInfo != nil {
		return prInfo.Title()
	}
	return branchInfo.BranchName
}

func (c *ConsolidateMergeExecutor) getConsolidationTitle() string {
	if len(c.plan.BranchesToMerge) == 0 {
		return "Stack consolidation"
	}

	// Use the title of the top-most PR as the main title
	topBranch := c.plan.BranchesToMerge[len(c.plan.BranchesToMerge)-1]
	return c.getBranchTitle(topBranch)
}

func (c *ConsolidateMergeExecutor) buildConsolidationPRBody() string {
	var body strings.Builder

	body.WriteString(fmt.Sprintf("## Stack Consolidation: %s\n\n", c.getStackScope()))
	body.WriteString("This PR consolidates the following stack of changes into a single merge:\n\n")

	for i, branchInfo := range c.plan.BranchesToMerge {
		branch := c.engine.GetBranch(branchInfo.BranchName)
		prInfo, _ := branch.GetPrInfo()
		if prInfo != nil && prInfo.Number() != nil {
			body.WriteString(fmt.Sprintf("%d. **PR #%d**: %s\n", i+1, *prInfo.Number(), prInfo.Title()))
		} else {
			body.WriteString(fmt.Sprintf("%d. **%s**: %s\n", i+1, branchInfo.BranchName, c.getBranchTitle(branchInfo)))
		}
	}

	body.WriteString("\n### Benefits\n")
	body.WriteString("- ✅ Single CI run validates entire stack\n")
	body.WriteString("- ✅ Atomic merge prevents partial stack states\n")
	body.WriteString("- ✅ Faster than sequential merging\n")
	body.WriteString("- ✅ Cleaner merge history\n")

	body.WriteString("\n### After Merge\n")
	body.WriteString("Individual PRs will be automatically documented and closed.\n")

	body.WriteString("\n### Stack Structure\n")
	body.WriteString("```\n")
	body.WriteString(c.buildStackTree())
	body.WriteString("```\n")

	return body.String()
}

func (c *ConsolidateMergeExecutor) buildStackTree() string {
	var tree strings.Builder
	trunkName := c.engine.Trunk().GetName()

	tree.WriteString(trunkName + "\n")

	for _, branchInfo := range c.plan.BranchesToMerge {
		branch := c.engine.GetBranch(branchInfo.BranchName)
		depth := 0
		parent := branch.GetParent()
		for parent != nil && !parent.IsTrunk() {
			depth++
			parent = parent.GetParent()
		}

		indent := strings.Repeat("  ", depth+1)
		tree.WriteString(fmt.Sprintf("%s├─ %s (PR #%d)\n", indent, branchInfo.BranchName, branchInfo.PRNumber))
	}

	return tree.String()
}

func (c *ConsolidateMergeExecutor) getOwnerRepo() (owner, repo string) {
	return c.ctx.GitHubClient.GetOwnerRepo()
}
