package merge

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/github"
	"github.com/getstackit/stackit/internal/pr"
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
	prGenerator       *PRContentGenerator
	mergeMethod       github.MergeMethod // Optional override (empty = auto-detect/prompt)

	handler   EventHandler // Optional progress handler for TUI updates
	stepIndex int          // Current step index for the handler
}

// NewConsolidateMergeExecutor creates a new consolidation executor
func NewConsolidateMergeExecutor(plan *Plan, engine mergeExecuteEngine, ctx *app.Context) *ConsolidateMergeExecutor {
	return &ConsolidateMergeExecutor{
		plan:        plan,
		engine:      engine,
		ctx:         ctx,
		prGenerator: NewPRContentGenerator(engine),
	}
}

// SetProgressHandler sets the progress handler and step index for reporting
func (c *ConsolidateMergeExecutor) SetProgressHandler(handler EventHandler, stepIndex int) {
	c.handler = handler
	c.stepIndex = stepIndex
}

// Execute performs stack consolidation merging
func (c *ConsolidateMergeExecutor) Execute(ctx context.Context, opts ExecuteOptions) (*ConsolidationResult, error) {
	splog := c.ctx.Output
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

	if opts.Wait {
		if err := c.waitForConsolidationMerge(ctx, consolidationBranch, pr); err != nil {
			return nil, fmt.Errorf("consolidation merge failed: %w", err)
		}

		// Store consolidation info for footer updates
		c.consolidationPR = pr
		if userName, err := c.ctx.Engine.GetUserName(ctx); err == nil && userName != "" {
			c.consolidationUser = userName
		}

		c.postMergeCleanup(ctx)

		splog.Info("🎉 Stack consolidation merge completed successfully!")
	} else {
		// Fire-and-forget: enable auto-merge and return
		mergeMethod, err := c.resolveMergeMethod()
		if err != nil {
			return nil, fmt.Errorf("failed to get merge method: %w", err)
		}
		metadata := c.buildStackMetadata()
		if err := github.EnableAutoMerge(ctx, c.engine.Git(), pr.NodeID, github.EnableAutoMergeOptions{ //nolint:forbidigo // GitHub integration needs the git runner to run gh; not a domain bypass
			MergeMethod: mergeMethod,
			CommitBody:  metadata.ToTrailers(),
		}); err != nil {
			splog.Warn("Could not enable automerge: %v", err)
			splog.Tip("Enable automerge manually on the PR: %s", pr.HTMLURL)
		} else {
			splog.Success("Automerge enabled on PR #%d", pr.Number)
		}
		splog.Info("Individual PRs have been locked. The consolidation PR will merge when CI passes.")
		splog.Tip("Run 'stackit sync --restack' after the PR merges to update your stack.")
	}

	result := &ConsolidationResult{
		BranchName: consolidationBranch,
		PRNumber:   pr.Number,
		PRURL:      pr.HTMLURL,
	}
	return result, nil
}

// preValidateStack ensures all PRs are ready for consolidation
func (c *ConsolidateMergeExecutor) preValidateStack(ctx context.Context, force bool) error {
	splog := c.ctx.Output

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
	splog := c.ctx.Output
	// Generate unique branch name
	timestamp := time.Now().Unix()
	scope := c.getStackScopeOrDefault()
	branchName := fmt.Sprintf("stack-merge-%s-%d", scope, timestamp)

	splog.Info("📋 Creating merge branch: %s", branchName)

	trunkName := c.engine.Trunk().GetName()

	// Create branch directly at trunk (avoids creating at HEAD then resetting)
	if err := c.engine.CreateBranch(ctx, branchName, trunkName); err != nil {
		return "", fmt.Errorf("failed to create branch: %w", err)
	}

	if err := c.engine.CheckoutBranch(ctx, c.engine.GetBranch(branchName)); err != nil {
		return "", fmt.Errorf("failed to checkout branch: %w", err)
	}

	// Mark as utility branch so it can be auto-deleted without confirmation during sync
	if err := c.engine.SetBranchType(c.engine.GetBranch(branchName), git.BranchTypeUtility); err != nil {
		splog.Debug("Failed to set branch type: %v", err)
	}

	// Collect branch names for octopus merge
	branches := c.plan.BranchNames()

	// Build commit message listing all branches
	commitMsg := fmt.Sprintf("Consolidate stack [%s]", scope)
	if len(branches) <= 3 {
		commitMsg += ": " + strings.Join(branches, ", ")
	} else {
		commitMsg += fmt.Sprintf(" (%d branches)", len(branches))
	}

	// Append stack trailers for history tracking
	metadata := c.buildStackMetadata()
	commitMsg += "\n\n" + metadata.ToTrailers()

	splog.Info("  Merging %d branches via octopus merge...", len(branches))
	splog.Debug("Consolidation merge: merging branches %v", branches)

	// Perform octopus merge (single merge commit with multiple parents)
	if err := c.engine.MergeMultiple(ctx, branches, engine.MergeOptions{NoFF: true, Message: commitMsg}); err != nil {
		splog.Debug("Consolidation octopus merge failed: %v", err)
		return "", fmt.Errorf("failed to merge branches: %w", err)
	}

	if err := c.engine.PushBranch(ctx, c.engine.GetBranch(branchName), c.engine.GetRemote(), git.PushOptions{
		Force:    false,
		NoVerify: true,
	}); err != nil {
		return "", fmt.Errorf("failed to push consolidation branch %s: %w", branchName, err)
	}

	splog.Info("✅ Merge branch created and pushed")
	return branchName, nil
}

func (c *ConsolidateMergeExecutor) createConsolidationPR(ctx context.Context, branchName string) (*github.PullRequestInfo, error) {
	content := c.prGenerator.GenerateConsolidationPR(c.plan.BranchesToMerge)

	owner, repo := c.getOwnerRepo()
	opts := github.CreatePROptions{
		Title: content.Title,
		Body:  content.Body,
		Head:  branchName,
		Base:  c.engine.Trunk().GetName(),
		Draft: false,
	}

	pr, err := c.ctx.GitHub().CreatePullRequest(ctx, owner, repo, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create PR: %w", err)
	}

	return pr, nil
}

// waitForConsolidationMerge waits for CI to pass and auto-merges the consolidation PR
func (c *ConsolidateMergeExecutor) waitForConsolidationMerge(ctx context.Context, branchName string, pr *github.PullRequestInfo) error {
	expectChecks := AnyPRHasChecks(c.plan.BranchesToMerge)

	// Get merge method: use override if provided, otherwise detect/prompt
	mergeMethod, err := c.resolveMergeMethod()
	if err != nil {
		return fmt.Errorf("failed to get merge method: %w", err)
	}

	waiter := NewCIWaiter(CIWaiterOptions{
		Client: c.ctx.GitHub(),
		Output: c.ctx.Output,
	})
	waiter.SetProgressHandler(c.handler, c.stepIndex)

	metadata := c.buildStackMetadata()
	mergeOpts := github.MergePROptions{
		Method:     mergeMethod,
		CommitBody: metadata.ToTrailers(),
	}
	return waiter.WaitAndMerge(ctx, branchName, pr, expectChecks, mergeOpts)
}

func (c *ConsolidateMergeExecutor) postMergeCleanup(_ context.Context) {
	c.ctx.Output.Info("🧹 Updating individual PRs...")

	c.updateIndividualPRs()
}

func (c *ConsolidateMergeExecutor) updateIndividualPRs() {
	// Skip if we don't have consolidation PR info
	if c.consolidationPR == nil || c.consolidationPR.Number == 0 {
		c.ctx.Output.Debug("No consolidation PR info available for footer updates")
		return
	}

	// Use shared PR cleaner
	cleaner := NewPRCleaner(c.ctx, c.engine, PRCleanupConfig{
		Source:                CleanupSourceConsolidate,
		ConsolidationPRNumber: c.consolidationPR.Number,
		UserName:              c.consolidationUser,
	})

	result := cleaner.CleanupBranches(c.ctx.Context, c.plan.BranchNames())
	cleaner.LogResult(result)
}

func (c *ConsolidateMergeExecutor) lockAndNotifyIndividualPRs(_ context.Context, consolidationBranch string) error {
	splog := c.ctx.Output
	splog.Info("🔒 Locking individual PRs and updating status...")

	branchesToLock := engine.Branches{}
	for _, b := range c.plan.BranchesToMerge {
		branch := c.engine.GetBranch(b.BranchName)
		if !branch.IsLocked() {
			branchesToLock = branchesToLock.Append(branch)
		}
	}
	branchNames := c.plan.BranchNames()

	if len(branchesToLock) > 0 {
		if _, err := c.engine.SetLocked(c.ctx, branchesToLock, engine.LockReasonConsolidating); err != nil {
			return fmt.Errorf("failed to lock branches: %w", err)
		}
	}

	prUpdates := make(map[string]*engine.PrInfo, len(branchesToLock))
	for _, b := range branchesToLock {
		prInfo, err := b.GetPrInfo()
		if err != nil {
			splog.Debug("Failed to read PR info for %s: %v", b.GetName(), err)
			continue
		}
		if prInfo != nil {
			prUpdates[b.GetName()] = prInfo.WithMergeBranch(consolidationBranch)
		}
	}
	if len(prUpdates) > 0 {
		if err := c.engine.BatchUpsertPrInfo(c.ctx.Context, prUpdates); err != nil {
			splog.Debug("Failed to batch upsert PR info: %v", err)
		}
	}

	if err := actions.PushMetadataAndSyncPRs(c.ctx, branchNames); err != nil {
		splog.Warn("Failed to sync individual PRs: %v", err)
	}

	return nil
}

// Helper methods

func (c *ConsolidateMergeExecutor) getStackScopeOrDefault() string {
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

func (c *ConsolidateMergeExecutor) getOwnerRepo() (owner, repo string) {
	return c.ctx.GitHub().GetOwnerRepo()
}

// buildStackMetadata builds stack metadata for consolidation merge commits.
func (c *ConsolidateMergeExecutor) buildStackMetadata() pr.StackMetadata {
	scope := c.getTrailerScope()
	prNumbers := make([]int, 0, len(c.plan.BranchesToMerge))
	for _, b := range c.plan.BranchesToMerge {
		if b.PRNumber > 0 {
			prNumbers = append(prNumbers, b.PRNumber)
		}
	}
	return pr.NewStackMetadata(len(c.plan.BranchesToMerge), prNumbers, scope)
}

// getTrailerScope returns a scope only when all non-empty branch scopes match.
func (c *ConsolidateMergeExecutor) getTrailerScope() string {
	scopes := make([]string, 0, len(c.plan.BranchesToMerge))
	for _, b := range c.plan.BranchesToMerge {
		branch := c.engine.GetBranch(b.BranchName)
		scope := c.engine.GetScope(branch)
		if scope.IsEmpty() {
			continue
		}
		scopes = append(scopes, scope.String())
	}
	return pr.ResolveUnifiedScope(scopes)
}

// resolveMergeMethod returns the merge method to use, preferring the override if set.
func (c *ConsolidateMergeExecutor) resolveMergeMethod() (github.MergeMethod, error) {
	if c.mergeMethod != "" {
		return c.mergeMethod, nil
	}
	return getMergeMethodWithPause(c.ctx, c.ctx.GitHub(), c.handler)
}
