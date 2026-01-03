package merge

import (
	"context"
	"fmt"
	"strings"
	"time"

	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/config"
	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/git"
	"stackit.dev/stackit/internal/github"
	"stackit.dev/stackit/internal/tui"
)

const (
	prStateOpen = "OPEN"
)

// ProgressReporter is an interface for reporting merge progress
type ProgressReporter interface {
	StepStarted(stepIndex int, description string)
	StepCompleted(stepIndex int)
	StepFailed(stepIndex int, err error)
	StepWaiting(stepIndex int, elapsed, timeout time.Duration, checks []github.CheckDetail)
	SetEstimatedDuration(duration time.Duration)
}

// mergeExecuteEngine is a minimal interface needed for executing a merge plan
type mergeExecuteEngine interface {
	engine.PRManager
	engine.BranchReader
	engine.BranchWriter
	engine.SyncManager
	engine.SplitManager
	engine.RemoteMetadataManager
	Git() git.Runner
}

// ExecuteOptions contains options for executing a merge plan
type ExecuteOptions struct {
	Plan                    *Plan
	Strategy                Strategy
	Force                   bool
	DryRun                  bool
	Confirm                 bool
	Scope                   string
	TargetBranch            string
	Reporter                ProgressReporter           // Optional progress reporter
	UndoStackDepth          int                        // Maximum undo stack depth (from config)
	ConsolidationResultFunc func(*ConsolidationResult) // Callback for consolidation results
}

// Execute executes a validated merge plan step by step
func Execute(ctx *app.Context, eng mergeExecuteEngine, opts ExecuteOptions) error {
	plan := opts.Plan
	githubClient := ctx.GitHubClient
	splog := ctx.Splog

	// Calculate initial estimate if possible
	var initialEstimate time.Duration
	if githubClient != nil {
		initialEstimate = calculateBaselineEstimate(ctx.Context, plan, githubClient, splog)
	}

	// If no reporter provided and we're in a TTY, use TUI
	if opts.Reporter == nil && tui.IsTTY() {
		reporter := tui.NewChannelMergeProgressReporter()

		// Calculate groups for the TUI
		groups := calculateGroups(plan)

		// Extract step descriptions
		stepDescriptions := make([]string, len(plan.Steps))
		for i, step := range plan.Steps {
			stepDescriptions[i] = step.Description
		}

		// Suppress splog output during TUI execution to prevent console interference
		splog.SetQuiet(true)
		defer splog.SetQuiet(false) // Ensure we restore logging even if there's an error

		// Start TUI in a goroutine
		done := make(chan bool, 1)
		tuiErr := make(chan error, 1)
		go func() {
			err := tui.RunMergeTUI(groups, stepDescriptions, reporter.Updates(), done)
			if err != nil {
				tuiErr <- err
			}
		}()

		// Update opts to use the reporter
		opts.Reporter = reporter

		// Set up callback to collect consolidation results
		var consolidationResult *ConsolidationResult
		opts.ConsolidationResultFunc = func(result *ConsolidationResult) {
			consolidationResult = result
		}

		if initialEstimate > 0 {
			reporter.SetEstimatedDuration(initialEstimate)
		}

		// Execute plan (this will send updates to the reporter)
		err := executeSteps(ctx, eng, opts)

		// Close reporter to signal TUI to finish
		reporter.Close()

		// Wait for TUI to finish or error
		select {
		case <-done:
			// TUI finished normally
		case err := <-tuiErr:
			if err != nil {
				splog.Debug("TUI error: %v", err)
			}
		}

		// Display consolidation result if available
		if consolidationResult != nil {
			splog.Info("✅ Created consolidation PR #%d: %s", consolidationResult.PRNumber, consolidationResult.PRURL)
		}

		return err
	}

	// Execute without TUI
	return executeSteps(ctx, eng, opts)
}

// ExecuteInWorktree executes the merge plan in a temporary worktree
func ExecuteInWorktree(ctx *app.Context, eng mergeExecuteEngine, opts ExecuteOptions) (err error) {
	splog := ctx.Splog

	// Create temporary worktree via engine
	// We use detached HEAD at the current revision to avoid "already used by worktree" errors
	// and to ensure we don't accidentally move any main workspace branch refs.
	worktreePath, cleanup, err := eng.CreateTemporaryWorktree(ctx.Context, "HEAD", "stackit-merge-*")
	if err != nil {
		return err
	}

	splog.Debug("📁 Worktree: %s", worktreePath)

	// Ensure we clean up on exit
	cleanupWorktree := true
	defer func() {
		if cleanupWorktree {
			splog.Debug("Cleaning up worktree at %s", worktreePath)
			cleanup()
		}
	}()

	// 4. Create a new engine for the worktree
	maxUndoDepth := opts.UndoStackDepth
	if maxUndoDepth <= 0 {
		maxUndoDepth = engine.DefaultMaxUndoStackDepth
	}

	// We need to know the trunk name for the new engine.
	// Since we are currently in the main engine, we can get it from there.
	trunk := eng.Trunk()

	worktreeEng, err := engine.NewEngine(engine.Options{
		RepoRoot:          worktreePath,
		Trunk:             trunk.GetName(),
		MaxUndoStackDepth: maxUndoDepth,
	})
	if err != nil {
		return fmt.Errorf("failed to initialize engine in worktree: %w", err)
	}

	// Create a sub-context for the worktree
	worktreeCtx := *ctx
	worktreeCtx.Engine = worktreeEng
	worktreeCtx.RepoRoot = worktreePath

	// 5. Pre-flight operations in the worktree
	// Populate remote SHAs so we can accurately check if branches match remote
	if err := worktreeEng.PopulateRemoteShas(); err != nil {
		splog.Debug("Failed to populate remote SHAs in worktree: %v", err)
	}

	// Pull trunk in the worktree to ensure we have latest changes
	pullResult, err := worktreeEng.PullTrunk(ctx.Context)
	if err != nil {
		splog.Debug("Failed to pull trunk in worktree: %v", err)
	} else if pullResult == engine.PullConflict {
		return fmt.Errorf("trunk could not be fast-forwarded in worktree (conflict)")
	}

	// 6. Create or Validate the plan
	plan := opts.Plan
	var validation *PlanValidation

	if plan == nil {
		// Create plan in worktree
		plan, validation, err = CreateMergePlan(ctx.Context, worktreeEng, splog, ctx.GitHubClient, CreatePlanOptions{
			Strategy:     opts.Strategy,
			Force:        opts.Force,
			Scope:        opts.Scope,
			TargetBranch: opts.TargetBranch,
		})
		if err != nil {
			return err
		}

		// Update opts with the new plan
		opts.Plan = plan

		// Display validation results
		if !validation.Valid {
			splog.Warn("Cannot proceed with merge due to validation errors:")
			for _, errMsg := range validation.Errors {
				splog.Warn("  ✗ %s", errMsg)
			}
			if !opts.DryRun && !opts.Force {
				return fmt.Errorf("validation failed (use --force to override)")
			}
		}

		if len(validation.Warnings) > 0 {
			splog.Warn("Warnings:")
			for _, warn := range validation.Warnings {
				splog.Warn("  %s", warn)
			}
			splog.Newline()
			if !opts.DryRun && !opts.Force {
				return fmt.Errorf("merge blocked due to warnings (use --force to override)")
			}
		}

		if len(validation.Infos) > 0 {
			splog.Info("Information:")
			for _, info := range validation.Infos {
				splog.Info("  • %s", info)
			}
			splog.Newline()
		}
	}

	// 7. Handle Dry Run or Confirmation
	if opts.DryRun {
		planText := FormatMergePlan(plan, validation)
		splog.Page(planText)
		splog.Info("Dry-run mode: plan displayed above. No changes were made.")
		return nil
	}

	if opts.Confirm {
		planText := FormatMergePlan(plan, validation)
		splog.Page(planText)

		confirmed, err := tui.PromptConfirm("Proceed with merge?", false)
		if err != nil {
			return fmt.Errorf("confirmation canceled: %w", err)
		}
		if !confirmed {
			splog.Info("Merge canceled")
			return nil
		}
	}

	// 8. Execute the plan in the worktree
	err = Execute(&worktreeCtx, worktreeEng, opts)

	if err != nil {
		// If it's a conflict, don't clean up so the user can resolve it
		if isConflictError(err) {
			cleanupWorktree = false
			splog.Warn("Conflict detected during merge execution in worktree.")
			splog.Info("The worktree has been preserved for manual resolution.")
			splog.Info("Your main workspace has been left untouched.")
			splog.Newline()
			splog.Info("To resolve the conflict and continue:")
			splog.Info("  1. cd %s", worktreePath)
			splog.Info("  2. Resolve the conflicts and git add the files.")
			splog.Info("  3. Run 'stackit continue' to finish the merge/restack.")
			splog.Info("  4. Once finished, return to your main workspace.")
			return err
		}

		// For other errors (like CI failure), we still want to give instructions
		// but we can clean up the worktree.
		splog.Warn("Merge execution failed in worktree.")
		splog.Info("Your main workspace remains untouched.")
		splog.Newline()
		if isCIFailure(err) {
			splog.Info("CI checks failed. Please:")
			splog.Info("  1. Fix the issues in your main workspace.")
			splog.Info("  2. Run 'stackit submit' to update PRs.")
			splog.Info("  3. Run 'stackit merge' again once CI passes.")
		} else {
			splog.Info("To resolve:")
			splog.Info("  1. Fix the underlying issue in your main workspace.")
			splog.Info("  2. Run 'stackit merge' again.")
		}
		return err
	}

	return nil
}

// calculateBaselineEstimate tries to find a branch with successful CI and use its timing as a baseline
func calculateBaselineEstimate(ctx context.Context, plan *Plan, client github.Client, splog *tui.Splog) time.Duration {
	branchNames := make([]string, len(plan.BranchesToMerge))
	for i, b := range plan.BranchesToMerge {
		branchNames[i] = b.BranchName
	}

	statuses, err := client.BatchGetPRChecksStatus(ctx, branchNames)
	if err != nil {
		return 0
	}

	for _, branchInfo := range plan.BranchesToMerge {
		status := statuses[branchInfo.BranchName]
		if status == nil || !status.Passing || status.Pending {
			continue
		}

		// Found a passing PR, calculate the max duration of its checks
		var maxDuration time.Duration
		for _, check := range status.Checks {
			if !check.FinishedAt.IsZero() && !check.StartedAt.IsZero() {
				duration := check.FinishedAt.Sub(check.StartedAt)
				if duration > maxDuration {
					maxDuration = duration
				}
			}
		}

		if maxDuration > 0 {
			splog.Debug("Using PR #%d (%s) as timing baseline: %v", branchInfo.PRNumber, branchInfo.BranchName, maxDuration.Round(time.Second))
			return maxDuration
		}
	}
	return 0
}

func isConflictError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "hit conflict") ||
		strings.Contains(msg, "rebase conflict") ||
		strings.Contains(msg, "could not be fast-forwarded (conflict)")
}

func isCIFailure(err error) bool {
	if err == nil {
		return false
	}
	errStr := fmt.Sprintf("%v", err)
	return strings.Contains(errStr, "CI checks failed") || strings.Contains(errStr, "failing CI checks") || strings.Contains(errStr, "pending CI checks")
}

func calculateGroups(plan *Plan) []tui.MergeGroup {
	var groups []tui.MergeGroup
	assigned := make(map[int]bool)

	// 1. Create groups for each branch being merged
	for _, branchInfo := range plan.BranchesToMerge {
		var indices []int
		for i, step := range plan.Steps {
			if step.BranchName == branchInfo.BranchName {
				indices = append(indices, i)
				assigned[i] = true
			}
		}
		if len(indices) > 0 {
			groups = append(groups, tui.MergeGroup{
				Label:       fmt.Sprintf("PR #%d (%s)", branchInfo.PRNumber, branchInfo.BranchName),
				StepIndices: indices,
			})
		}
	}

	// 2. Create group for upstack branches
	if len(plan.UpstackBranches) > 0 {
		var indices []int
		for i, step := range plan.Steps {
			if assigned[i] {
				continue
			}
			for _, ub := range plan.UpstackBranches {
				if step.BranchName == ub {
					indices = append(indices, i)
					assigned[i] = true
					break
				}
			}
		}
		if len(indices) > 0 {
			groups = append(groups, tui.MergeGroup{
				Label:       "Restack upstack branches",
				StepIndices: indices,
			})
		}
	}

	// 3. Remaining steps (like PullTrunk)
	for i := 0; i < len(plan.Steps); i++ {
		if assigned[i] {
			continue
		}

		label := plan.Steps[i].Description
		if plan.Steps[i].StepType == StepPullTrunk {
			label = "Sync trunk"
		}

		groups = append(groups, tui.MergeGroup{
			Label:       label,
			StepIndices: []int{i},
		})
		assigned[i] = true
	}

	return groups
}

// executeSteps executes the merge plan steps
func executeSteps(ctx *app.Context, eng mergeExecuteEngine, opts ExecuteOptions) error {
	plan := opts.Plan

	for i, step := range plan.Steps {
		// Report step started
		if opts.Reporter != nil {
			opts.Reporter.StepStarted(i, step.Description)
		}

		// 1. Re-validate preconditions for this step
		if err := validateStepPreconditions(ctx.Context, step, eng, ctx.GitHubClient, opts); err != nil {
			if opts.Reporter != nil {
				opts.Reporter.StepFailed(i, err)
			}
			return fmt.Errorf("step %d (%s) failed precondition: %w", i+1, step.Description, err)
		}

		// 2. Execute the step (with progress reporting for wait steps)
		if err := executeStepWithProgress(ctx, step, i, eng, opts); err != nil {
			ctx.Splog.Debug("Step %d (%s) failed: %v", i+1, step.Description, err)
			if opts.Reporter != nil {
				opts.Reporter.StepFailed(i, err)
			}
			return fmt.Errorf("step %d (%s) failed: %w", i+1, step.Description, err)
		}

		// 3. Report step completed
		if opts.Reporter != nil {
			opts.Reporter.StepCompleted(i)
		}

		// 4. Log progress (if no reporter, use simple logging)
		if opts.Reporter == nil {
			ctx.Splog.Info("✓ %s", step.Description)
		}
	}

	return nil
}

// validateStepPreconditions validates that a step can be executed
func validateStepPreconditions(ctx context.Context, step PlanStep, eng mergeExecuteEngine, githubClient github.Client, opts ExecuteOptions) error {
	switch step.StepType {
	case StepMergePR:
		// Validate PR still exists and is open
		branch := eng.GetBranch(step.BranchName)
		prInfo, err := branch.GetPrInfo()
		if err != nil {
			return fmt.Errorf("failed to get PR info: %w", err)
		}
		if prInfo == nil || prInfo.Number() == nil {
			return fmt.Errorf("PR not found for branch %s", step.BranchName)
		}
		if prInfo.State() != prStateOpen {
			return fmt.Errorf("PR #%d for branch %s is %s (not open)", *prInfo.Number(), step.BranchName, prInfo.State())
		}
		// Optionally check CI checks haven't changed to failing or pending
		if !opts.Force && githubClient != nil {
			status, err := githubClient.GetPRChecksStatus(ctx, step.BranchName)
			if err == nil && status != nil {
				if !status.Passing {
					return fmt.Errorf("PR #%d for branch %s has failing CI checks", *prInfo.Number(), step.BranchName)
				}
				if status.Pending {
					return fmt.Errorf("PR #%d for branch %s has pending CI checks", *prInfo.Number(), step.BranchName)
				}
			}
		}

	case StepRestack:
		// Validate branch still exists
		branch := eng.GetBranch(step.BranchName)
		if !branch.IsTracked() {
			return fmt.Errorf("branch %s is not tracked", step.BranchName)
		}

	case StepDeleteBranch:
		// Validate branch exists (or allow if already deleted)
		// This is non-blocking - branch might already be deleted

	case StepUpdatePRBase:
		// Validate PR exists
		branch := eng.GetBranch(step.BranchName)
		prInfo, err := branch.GetPrInfo()
		if err != nil {
			return fmt.Errorf("failed to get PR info: %w", err)
		}
		if prInfo == nil || prInfo.Number() == nil {
			return fmt.Errorf("PR not found for branch %s", step.BranchName)
		}

	case StepPullTrunk:
		// No preconditions needed

	case StepWaitCI:
		// Validate PR exists and is open
		branch := eng.GetBranch(step.BranchName)
		prInfo, err := branch.GetPrInfo()
		if err != nil {
			return fmt.Errorf("failed to get PR info: %w", err)
		}
		if prInfo == nil || prInfo.Number() == nil {
			return fmt.Errorf("PR not found for branch %s", step.BranchName)
		}
		if prInfo.State() != prStateOpen {
			return fmt.Errorf("PR #%d for branch %s is %s (not open)", *prInfo.Number(), step.BranchName, prInfo.State())
		}
	}

	return nil
}

// executeStepWithProgress executes a step with progress reporting
func executeStepWithProgress(ctx *app.Context, step PlanStep, stepIndex int, eng mergeExecuteEngine, opts ExecuteOptions) error {
	// Special handling for wait steps to report progress
	if step.StepType == StepWaitCI {
		return executeWaitCIWithProgress(ctx, step, stepIndex, eng, opts)
	}
	return executeStep(ctx, step, eng, opts)
}

// executeStep executes a single step
func executeStep(ctx *app.Context, step PlanStep, eng mergeExecuteEngine, opts ExecuteOptions) error {
	trunk := eng.Trunk() // Cache trunk for this function scope
	trunkName := trunk.GetName()
	githubClient := ctx.GitHubClient
	splog := ctx.Splog
	repoRoot := ctx.RepoRoot

	switch step.StepType {
	case StepMergePR:
		if githubClient == nil {
			return fmt.Errorf("GitHub client not available")
		}
		splog.Debug("Executing StepMergePR for branch %s", step.BranchName)
		if err := githubClient.MergePullRequest(ctx.Context, step.BranchName); err != nil {
			splog.Debug("StepMergePR for branch %s failed: %v", step.BranchName, err)
			return fmt.Errorf("failed to merge PR: %w", err)
		}

	case StepPullTrunk:
		splog.Debug("Executing StepPullTrunk")
		pullResult, err := eng.PullTrunk(ctx.Context)
		if err != nil {
			splog.Debug("StepPullTrunk failed: %v", err)
			return fmt.Errorf("failed to pull trunk: %w", err)
		}
		switch pullResult {
		case engine.PullDone:
			trunk := eng.Trunk()
			rev, _ := trunk.GetRevision()
			revShort := rev
			if len(rev) > 7 {
				revShort = rev[:7]
			}
			splog.Debug("Trunk fast-forwarded to %s", revShort)
		case engine.PullUnneeded:
			splog.Debug("Trunk is up to date")
		case engine.PullConflict:
			return fmt.Errorf("trunk could not be fast-forwarded (conflict)")
		}

	case StepRestack:
		// Restack the branch - RestackBranches will automatically handle reparenting
		// if the parent has been merged/deleted
		branch := eng.GetBranch(step.BranchName)
		splog.Debug("Executing StepRestack for branch %s", step.BranchName)
		batchResult, err := eng.RestackBranches(ctx.Context, []engine.Branch{branch})
		result := batchResult.Results[step.BranchName]
		if err != nil {
			splog.Debug("StepRestack for branch %s failed: %v", step.BranchName, err)
			return fmt.Errorf("failed to restack: %w", err)
		}

		// Get the actual parent after restacking (may have been reparented)
		// Use NewParent from result if reparented, otherwise get from engine
		actualParent := result.NewParent
		if actualParent == "" {
			branch := eng.GetBranch(step.BranchName)
			parent := branch.GetParent()
			if parent == nil {
				actualParent = trunkName
			} else {
				actualParent = parent.GetName()
			}
		}

		switch result.Result {
		case engine.RestackDone:
			// Success - now push the rebased branch and update PR base
			// Force push is required since we rebased
			if err := eng.PushBranch(ctx.Context, step.BranchName, eng.GetRemote(), git.PushOptions{
				Force:    true,
				NoVerify: true, // Internal restack usually shouldn't run hooks
			}); err != nil {
				return fmt.Errorf("failed to push rebased branch %s: %w", step.BranchName, err)
			}
			splog.Debug("Pushed rebased branch %s to remote", step.BranchName)

			// Update the PR's base branch to the actual parent (not always trunk)
			if err := updatePRBaseBranchFromContext(ctx.Context, githubClient, step.BranchName, actualParent); err != nil {
				return fmt.Errorf("failed to update PR base for %s: %w", step.BranchName, err)
			}
			splog.Debug("Updated PR base for %s to %s", step.BranchName, actualParent)

		case engine.RestackConflict:
			// Save continuation state
			currentBranch := eng.CurrentBranch()
			currentBranchName := ""
			if currentBranch != nil {
				currentBranchName = currentBranch.GetName()
			}
			continuation := &config.ContinuationState{
				RebasedBranchBase:     result.RebasedBranchBase,
				CurrentBranchOverride: currentBranchName,
			}
			if err := config.PersistContinuationState(repoRoot, continuation); err != nil {
				return fmt.Errorf("failed to persist continuation: %w", err)
			}
			return fmt.Errorf("hit conflict restacking %s", step.BranchName)
		case engine.RestackUnneeded:
			// Already up to date, but still need to ensure PR base is correct
			// Push in case local is ahead of remote
			if err := eng.PushBranch(ctx.Context, step.BranchName, eng.GetRemote(), git.PushOptions{
				Force:    true,
				NoVerify: true,
			}); err != nil {
				splog.Debug("Failed to push branch %s (may already be up to date): %v", step.BranchName, err)
			}
			// Update PR base to the actual parent (not always trunk)
			if err := updatePRBaseBranchFromContext(ctx.Context, githubClient, step.BranchName, actualParent); err != nil {
				splog.Debug("Failed to update PR base for %s: %v", step.BranchName, err)
			}
		}

	case StepDeleteBranch:
		// Only delete if branch is tracked
		branch := eng.GetBranch(step.BranchName)
		if branch.IsTracked() {
			if err := eng.DeleteBranch(ctx.Context, branch); err != nil {
				// Non-fatal - branch might already be deleted
				splog.Debug("Failed to delete branch %s (may already be deleted): %v", step.BranchName, err)
			}
		}

	case StepUpdatePRBase:
		// For top-down strategy: rebase branch onto trunk and update PR base
		splog.Debug("Executing StepUpdatePRBase for branch %s", step.BranchName)
		if err := executeUpdatePRBase(ctx, eng, step); err != nil {
			splog.Debug("StepUpdatePRBase for branch %s failed: %v", step.BranchName, err)
			return err
		}

	case StepConsolidate:
		// Execute stack consolidation
		splog.Debug("Executing StepConsolidate")
		result, err := executeConsolidation(ctx, eng, opts)
		if err != nil {
			splog.Debug("StepConsolidate failed: %v", err)
			return err
		}
		// Notify caller of consolidation result
		if opts.ConsolidationResultFunc != nil {
			opts.ConsolidationResultFunc(result)
		}

	case StepWaitCI:
		// StepWaitCI should be handled by executeStepWithProgress, not executeStep
		// This case should never be reached, but if it is, return an error
		return fmt.Errorf("StepWaitCI should be handled by executeStepWithProgress")

	default:
		return fmt.Errorf("unknown step type: %s", step.StepType)
	}

	return nil
}

// executeUpdatePRBase handles the UPDATE_PR_BASE step
// This is used in top-down strategy to rebase the current branch onto trunk
func executeUpdatePRBase(ctx *app.Context, eng mergeExecuteEngine, step PlanStep) error {
	trunk := eng.Trunk()
	trunkName := trunk.GetName()
	githubClient := ctx.GitHubClient

	// Get the parent revision (old base)
	branch := eng.GetBranch(step.BranchName)
	parent := branch.GetParent()
	parentName := ""
	if parent == nil {
		parentName = trunkName
	} else {
		parentName = parent.GetName()
	}

	// Get the old parent revision
	parentBranch := eng.GetBranch(parentName)
	oldParentRev, err := parentBranch.GetRevision()
	if err != nil {
		return fmt.Errorf("failed to get parent revision: %w", err)
	}

	// If parent is already trunk, we might just need to update the PR base
	if parentName == trunkName {
		// Just update the PR base branch via GitHub API
		return updatePRBaseBranchFromContext(ctx.Context, githubClient, step.BranchName, trunkName)
	}

	// Rebase the branch onto trunk
	// Save current branch state since git.Rebase no longer does this
	currentBranchObj := eng.CurrentBranch()
	currentBranch := ""
	if currentBranchObj != nil {
		currentBranch = currentBranchObj.GetName()
	}
	var currentRev string
	if currentBranch == "" {
		currentRev, _ = eng.GetCurrentRevision(ctx.Context)
	}

	ctx.Splog.Debug("Rebasing %s onto trunk %s (old base %s)", step.BranchName, trunkName, oldParentRev)
	gitResult, err := eng.Rebase(ctx.Context, step.BranchName, trunkName, oldParentRev)

	// Restore original branch state
	if currentBranch != "" {
		_ = eng.CheckoutBranch(ctx.Context, eng.GetBranch(currentBranch))
	} else if currentRev != "" {
		_ = eng.Detach(ctx.Context, currentRev)
	}

	if err != nil {
		ctx.Splog.Debug("Rebase of %s onto %s failed: %v", step.BranchName, trunkName, err)
		return fmt.Errorf("failed to rebase %s onto %s: %w", step.BranchName, trunkName, err)
	}

	if gitResult == engine.RestackConflict {
		return fmt.Errorf("rebase conflict while rebasing %s onto %s", step.BranchName, trunkName)
	}

	// Update parent to trunk
	if err := eng.SetParent(ctx.Context, eng.GetBranch(step.BranchName), eng.GetBranch(trunkName)); err != nil {
		return fmt.Errorf("failed to update parent: %w", err)
	}

	// Update PR base branch via GitHub API
	if err := updatePRBaseBranchFromContext(ctx.Context, githubClient, step.BranchName, trunkName); err != nil {
		return fmt.Errorf("failed to update PR base: %w", err)
	}

	// Rebuild engine to reflect changes
	if err := eng.Rebuild(trunkName); err != nil {
		return fmt.Errorf("failed to rebuild engine: %w", err)
	}

	return nil
}

// updatePRBaseBranchFromContext updates a PR's base branch via GitHub API
func updatePRBaseBranchFromContext(ctx context.Context, githubClient github.Client, branchName, newBase string) error {
	if githubClient == nil {
		// If we can't get GitHub client, skip this step (non-fatal)
		return nil
	}

	owner, repo := githubClient.GetOwnerRepo()

	// Get PR for this branch
	pr, err := githubClient.GetPullRequestByBranch(ctx, owner, repo, branchName)
	if err != nil || pr == nil {
		// PR not found or error - non-fatal
		return nil //nolint:nilerr
	}

	// Update PR base
	updateOpts := github.UpdatePROptions{
		Base: &newBase,
	}

	if err := githubClient.UpdatePullRequest(ctx, owner, repo, pr.Number, updateOpts); err != nil {
		return fmt.Errorf("failed to update PR base: %w", err)
	}

	return nil
}

// executeConsolidation handles the stack consolidation process
func executeConsolidation(ctx *app.Context, eng mergeExecuteEngine, opts ExecuteOptions) (*ConsolidationResult, error) {
	consolidator := NewConsolidateMergeExecutor(opts.Plan, eng, ctx)
	result, err := consolidator.Execute(ctx.Context, opts)
	if err != nil {
		return nil, err
	}

	// In non-TUI mode, display the result immediately
	if opts.Reporter == nil {
		ctx.Splog.Info("✅ Created consolidation PR #%d: %s", result.PRNumber, result.PRURL)
	}

	return result, nil
}

// executeWaitCIWithProgress waits for CI checks with progress reporting
func executeWaitCIWithProgress(ctx *app.Context, step PlanStep, stepIndex int, eng mergeExecuteEngine, opts ExecuteOptions) error {
	githubClient := ctx.GitHubClient
	splog := ctx.Splog

	if githubClient == nil {
		return fmt.Errorf("GitHub client not available")
	}

	timeout := step.WaitTimeout
	if timeout == 0 {
		timeout = 10 * time.Minute // Default timeout
	}

	pollInterval := 15 * time.Second    // Poll every 15 seconds
	progressInterval := 1 * time.Second // Report progress every second
	startTime := time.Now()
	deadline := startTime.Add(timeout)
	lastProgressReport := startTime

	// Get PR info for better error messages
	branch := eng.GetBranch(step.BranchName)
	prInfo, err := branch.GetPrInfo()
	if err != nil {
		return fmt.Errorf("failed to get PR info: %w", err)
	}
	prNumber := step.PRNumber
	if prInfo != nil && prInfo.Number() != nil {
		prNumber = *prInfo.Number()
	}

	if opts.Reporter == nil {
		splog.Info("Waiting for CI checks on PR #%d (%s)...", prNumber, step.BranchName)
	}

	for {
		// Check if we've exceeded the timeout
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for CI checks on PR #%d (%s) after %v", prNumber, step.BranchName, timeout)
		}

		// Report progress periodically
		now := time.Now()
		status, err := githubClient.GetPRChecksStatus(ctx.Context, step.BranchName)
		if err == nil && status != nil && opts.Reporter != nil && now.Sub(lastProgressReport) >= progressInterval {
			elapsed := now.Sub(startTime)
			opts.Reporter.StepWaiting(stepIndex, elapsed, timeout, status.Checks)
			lastProgressReport = now
		}

		if err != nil {
			// Log error but continue polling (might be transient)
			splog.Debug("Error checking CI status: %v", err)
		} else if status != nil {
			if !status.Passing {
				// CI checks failed
				return fmt.Errorf("CI checks failed on PR #%d (%s)", prNumber, step.BranchName)
			}

			// If we expect checks but none have appeared yet, treat as pending
			isReallyPending := status.Pending
			if step.ExpectChecks && len(status.Checks) == 0 {
				isReallyPending = true
				if now.Sub(startTime) > 5*time.Second {
					splog.Debug("No checks found yet for PR #%d, still waiting...", prNumber)
				}
			}

			if !isReallyPending {
				// All checks passed and none are pending
				elapsed := time.Since(startTime)

				// If we don't have an estimate yet, this successful PR can be our baseline
				if opts.Reporter != nil {
					var maxDuration time.Duration
					for _, check := range status.Checks {
						if !check.FinishedAt.IsZero() && !check.StartedAt.IsZero() {
							d := check.FinishedAt.Sub(check.StartedAt)
							if d > maxDuration {
								maxDuration = d
							}
						}
					}
					if maxDuration > 0 {
						opts.Reporter.SetEstimatedDuration(maxDuration)
					}
				}

				if opts.Reporter == nil {
					splog.Info("CI checks passed on PR #%d (%s) after %v", prNumber, step.BranchName, elapsed.Round(time.Second))
				}
				return nil
			}
		}

		// Wait before next poll
		remaining := time.Until(deadline)
		if remaining < pollInterval {
			time.Sleep(remaining)
		} else {
			time.Sleep(pollInterval)
		}
	}
}

// CheckSyncStatus checks if the repository is up to date with remote
func CheckSyncStatus(ctx context.Context, eng engine.Engine, splog *tui.Splog) (bool, []string, error) {
	needsSync := false
	staleBranches := []string{}

	// Check if trunk needs pulling
	pullResult, err := eng.PullTrunk(ctx)
	if err != nil {
		return false, nil, fmt.Errorf("failed to check trunk status: %w", err)
	}

	if pullResult == engine.PullDone {
		needsSync = true
		staleBranches = append(staleBranches, eng.Trunk().GetName())
	}

	// Check all tracked branches
	allBranches := eng.AllBranches()
	for _, branch := range allBranches {
		branchName := branch.GetName()
		branch := eng.GetBranch(branchName)
		if branch.IsTrunk() {
			continue
		}

		matchesRemote, err := eng.BranchMatchesRemote(branchName)
		if err != nil {
			splog.Debug("Failed to check if %s matches remote: %v", branchName, err)
			continue
		}

		if !matchesRemote {
			needsSync = true
			staleBranches = append(staleBranches, branchName)
		}
	}

	return needsSync, staleBranches, nil
}
