package merge

import (
	"context"
	"fmt"
	"strings"
	"time"

	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/git"
	"stackit.dev/stackit/internal/github"
	"stackit.dev/stackit/internal/output"
	"stackit.dev/stackit/internal/utils"
)

// Strategy defines how PRs in the stack should be merged
type Strategy string

const (
	// StrategyBottomUp merges PRs from the bottom of the stack up to the current branch
	StrategyBottomUp Strategy = "bottom-up"
	// StrategySquash creates a single PR containing all stack commits for atomic merging
	StrategySquash Strategy = "squash"
)

// StepType represents the type of step in a merge plan
type StepType string

const (
	// StepMergePR represents merging a PR
	StepMergePR StepType = "MERGE_PR"
	// StepRestack represents restacking a branch onto its parent
	StepRestack StepType = "RESTACK"
	// StepDeleteBranch represents deleting a local branch
	StepDeleteBranch StepType = "DELETE_BRANCH"
	// StepUpdatePRBase represents updating a PR's base branch
	StepUpdatePRBase StepType = "UPDATE_PR_BASE"
	// StepPullTrunk represents pulling the trunk branch
	StepPullTrunk StepType = "PULL_TRUNK"
	// StepWaitCI represents waiting for CI checks to complete
	StepWaitCI StepType = "WAIT_CI"
	// StepConsolidate represents consolidating the entire stack into a single PR
	StepConsolidate StepType = "CONSOLIDATE"
)

// ChecksStatus represents the CI check status for a PR
type ChecksStatus string

const (
	// ChecksPassing indicates all checks passed
	ChecksPassing ChecksStatus = "PASSING"
	// ChecksFailing indicates at least one check failed
	ChecksFailing ChecksStatus = "FAILING"
	// ChecksPending indicates checks are still running
	ChecksPending ChecksStatus = "PENDING"
	// ChecksNone indicates no checks are configured
	ChecksNone ChecksStatus = "NONE"
)

// Timing constants for CI waiting
const (
	// DefaultCITimeout is the default timeout for waiting on CI checks
	DefaultCITimeout = 10 * time.Minute
	// DefaultCIPollInterval is the default interval between CI status checks
	DefaultCIPollInterval = 15 * time.Second
	// CIRegistrationDelay is the delay to allow CI checks to register after push
	CIRegistrationDelay = 5 * time.Second
)

// BranchMergeInfo contains info about a branch to be merged
type BranchMergeInfo struct {
	BranchName    string
	PRNumber      int
	PRURL         string
	IsDraft       bool
	ChecksStatus  ChecksStatus
	MatchesRemote bool
}

// PlanStep represents a single step in the merge plan
type PlanStep struct {
	StepType     StepType
	BranchName   string
	PRNumber     int
	Description  string        // Human-readable description for display
	WaitTimeout  time.Duration // Timeout for waiting steps (e.g., CI checks)
	ExpectChecks bool          // Whether we expect CI checks to be present
}

// HasChecks returns true if the branch has CI checks configured
func (b BranchMergeInfo) HasChecks() bool {
	return b.ChecksStatus != ChecksNone
}

// AnyPRHasChecks returns true if any of the given branches have CI checks configured
func AnyPRHasChecks(branches []BranchMergeInfo) bool {
	for _, b := range branches {
		if b.HasChecks() {
			return true
		}
	}
	return false
}

// Plan is the complete plan for a merge operation
type Plan struct {
	Strategy        Strategy
	CurrentBranch   string
	BranchesToMerge []BranchMergeInfo // Branches that will be merged (bottom to top)
	UpstackBranches []string          // Branches above current that will be restacked
	Steps           []PlanStep        // Ordered steps to execute
	Warnings        []string          // Non-blocking warnings
	Infos           []string          // Informational messages
	CreatedAt       time.Time
}

// PlanValidation contains validation results
type PlanValidation struct {
	Valid    bool
	Errors   []string // Blocking errors
	Warnings []string // Non-blocking warnings
	Infos    []string // Informational messages
}

// CreatePlanOptions contains options for creating a merge plan
type CreatePlanOptions struct {
	Strategy     Strategy
	Force        bool
	Scope        string
	TargetBranch string // Optional branch to merge from (instead of current)
	Wait         bool   // Whether to wait for CI/merge (applies to consolidate strategy)
}

// mergePlanEngine is a minimal interface needed for creating a merge plan
type mergePlanEngine interface {
	engine.BranchReader
	engine.PRManager
	engine.SyncManager
	Git() git.Runner
}

// CreateMergePlan analyzes the current state and builds a merge plan
func CreateMergePlan(ctx context.Context, eng mergePlanEngine, splog output.Output, githubClient github.Client, opts CreatePlanOptions) (*Plan, *PlanValidation, error) {
	// 1. Get target branch, validate not on trunk
	var targetBranch engine.Branch
	if opts.TargetBranch != "" {
		targetBranch = eng.GetBranch(opts.TargetBranch)
		if !targetBranch.IsTracked() && !targetBranch.IsTrunk() {
			return nil, nil, fmt.Errorf("branch %s is not tracked by stackit", opts.TargetBranch)
		}
	} else {
		cb := eng.CurrentBranch()
		if cb == nil {
			return nil, nil, fmt.Errorf("not on a branch")
		}
		targetBranch = *cb
	}

	var allBranches []string
	var planCurrentBranch string

	// Build a StackGraph for efficient traversals
	graph := engine.BuildStackGraph(eng, engine.SortStrategyAlphabetical, nil)

	if opts.Scope != "" {
		// Collect all branches with the specified scope
		scopeBranches := []engine.Branch{}
		for _, b := range eng.AllBranches() {
			if !b.IsTrunk() && eng.GetScope(b).String() == opts.Scope {
				scopeBranches = append(scopeBranches, b)
			}
		}
		if len(scopeBranches) == 0 {
			return nil, nil, fmt.Errorf("no branches found with scope %s", opts.Scope)
		}

		// Sort branches topologically so we merge in correct order (bottom to top)
		scopeBranches = eng.SortBranchesTopologically(scopeBranches)
		allBranches = make([]string, len(scopeBranches))
		for i, b := range scopeBranches {
			allBranches[i] = b.GetName()
		}
		// In scope mode, the "current branch" for the plan is the top-most branch in the scope
		planCurrentBranch = allBranches[len(allBranches)-1]
	} else {
		if targetBranch.IsTrunk() {
			return nil, nil, fmt.Errorf("cannot merge from trunk. You must be on a branch that has a PR")
		}

		// Check if target branch is tracked
		if !targetBranch.IsTracked() {
			return nil, nil, fmt.Errorf("branch %s is not tracked by stackit", targetBranch.GetName())
		}

		// 2. Collect branches from trunk to target
		rng := engine.StackRange{RecursiveParents: true}
		parentBranches := graph.Range(targetBranch, rng)

		// Build full list: parent branches + target branch
		// Filter out trunk (it shouldn't be in the list, but be safe)
		allBranches = make([]string, 0, len(parentBranches)+1)
		for _, branch := range parentBranches {
			if !branch.IsTrunk() {
				allBranches = append(allBranches, branch.GetName())
			}
		}
		allBranches = append(allBranches, targetBranch.GetName())
		planCurrentBranch = targetBranch.GetName()
	}

	// 3. Identify upstack branches that need restacking (moved up for batch loading)
	upstackBranches := []string{}
	if opts.Scope != "" {
		// In scope mode, find all tracked branches with the scope that are not being merged
		for _, branch := range eng.AllBranches() {
			if branch.IsTracked() && eng.GetScope(branch).String() == opts.Scope {
				// Check if this branch is not already being merged
				isBeingMerged := false
				for _, merged := range allBranches {
					if branch.GetName() == merged {
						isBeingMerged = true
						break
					}
				}
				if !isBeingMerged {
					upstackBranches = append(upstackBranches, branch.GetName())
				}
			}
		}
	} else {
		// For upstack branches, we want branches that are descendants of the current branch,
		// but NOT in the list of branches we're merging.
		mergedMap := make(map[string]bool)
		for _, b := range allBranches {
			mergedMap[b] = true
		}

		// Only get upstack of the current branch (the top of the stack being merged)
		upstack := graph.Range(eng.GetBranch(planCurrentBranch), engine.StackRange{RecursiveChildren: true})
		for _, ub := range upstack {
			if ub.IsTracked() && !mergedMap[ub.GetName()] {
				upstackBranches = append(upstackBranches, ub.GetName())
			}
		}
	}

	// 4. Batch fetch metadata and revisions for all involved branches
	involvedBranches := append(append([]string{}, allBranches...), upstackBranches...)
	allMeta, _ := eng.Git().BatchReadMetadata(involvedBranches)
	// We don't strictly need allRevisions here yet, but it's good for cache
	_, _ = eng.Git().BatchGetRevisions(involvedBranches)

	// Fetch CI statuses in batch if possible
	var allCheckStatuses map[string]*github.CheckStatus
	if githubClient != nil {
		allCheckStatuses, _ = githubClient.BatchGetPRChecksStatus(ctx, allBranches)
	}

	// 5. For each branch: fetch PR info, check status, CI checks in parallel
	branchesToMerge := make([]BranchMergeInfo, len(allBranches))
	branchErrors := make([]string, len(allBranches))
	branchWarnings := make([][]string, len(allBranches))
	branchValid := make([]bool, len(allBranches))
	for i := range branchValid {
		branchValid[i] = true
	}

	indices := make([]int, len(allBranches))
	for i := range indices {
		indices[i] = i
	}

	utils.RunWithWorkers(indices, github.MaxGitHubConcurrency, func(idx int) {
		name := allBranches[idx]

		// Get PR info from batch-loaded metadata
		meta := allMeta[name]
		prInfo := engine.NewPrInfoFromMeta(meta)

		// Check if PR exists
		if prInfo == nil || prInfo.Number() == nil {
			branchValid[idx] = false
			branchErrors[idx] = fmt.Sprintf("Branch %s has no associated PR", name)
			return
		}

		// Check PR state
		state := prInfo.State()
		if state != "OPEN" {
			if state == "MERGED" {
				splog.Debug("Skipping %s: PR #%d is already merged", name, *prInfo.Number())
				branchValid[idx] = true // Not an error, just skip
				return
			}
			branchValid[idx] = false
			branchErrors[idx] = fmt.Sprintf("Branch %s PR #%d is %s (not open)", name, *prInfo.Number(), state)
			return
		}

		// Check if draft
		if prInfo.IsDraft() && !opts.Force {
			branchValid[idx] = false
			branchErrors[idx] = fmt.Sprintf("Branch %s PR #%d is a draft", name, *prInfo.Number())
		}

		// Check if local matches remote
		status, err := eng.GetBranchRemoteStatus(eng.GetBranch(name))
		matchesRemote := true
		if err != nil {
			splog.Debug("Failed to get branch remote status: %v", err)
		} else {
			matchesRemote = status.Matches()
		}

		if !matchesRemote && prInfo != nil && prInfo.Number() != nil {
			// Get detailed difference information
			diffInfo, _ := eng.GetBranchRemoteDifference(name)
			if diffInfo != "" {
				branchWarnings[idx] = append(branchWarnings[idx], fmt.Sprintf("Branch %s differs from remote: %s", name, diffInfo))
			} else {
				branchWarnings[idx] = append(branchWarnings[idx], fmt.Sprintf("Branch %s differs from remote", name))
			}
		}

		// Get CI check status from batch-loaded results
		checksStatus := ChecksNone
		if allCheckStatuses != nil {
			if status, ok := allCheckStatuses[name]; ok && status != nil {
				switch {
				case status.Pending:
					checksStatus = ChecksPending
				case !status.Passing:
					checksStatus = ChecksFailing
					if !opts.Force {
						branchValid[idx] = false
						branchErrors[idx] = fmt.Sprintf("Branch %s PR #%d has failing CI checks", name, *prInfo.Number())
					}
				default:
					checksStatus = ChecksPassing
				}
			}
		}

		branchesToMerge[idx] = BranchMergeInfo{
			BranchName:    name,
			PRNumber:      *prInfo.Number(),
			PRURL:         prInfo.URL(),
			IsDraft:       prInfo.IsDraft(),
			ChecksStatus:  checksStatus,
			MatchesRemote: matchesRemote,
		}
	})

	// Collect results and filter skipped branches
	finalBranchesToMerge := []BranchMergeInfo{}
	validation := &PlanValidation{
		Valid:    true,
		Errors:   []string{},
		Warnings: []string{},
	}

	// Pre-flight check: Check if trunk is in sync with remote
	trunk := eng.Trunk()
	if status, err := eng.GetBranchRemoteStatus(trunk); err == nil && status.Diverged() {
		validation.Warnings = append(validation.Warnings, fmt.Sprintf("Trunk branch %s has diverged from remote. You may need to sync it manually or use --force during merge.", trunk.GetName()))
	}

	for i := range allBranches {
		if !branchValid[i] {
			validation.Valid = false
		}
		if branchErrors[i] != "" {
			validation.Errors = append(validation.Errors, branchErrors[i])
		}
		validation.Warnings = append(validation.Warnings, branchWarnings[i]...)
		if branchesToMerge[i].BranchName != "" {
			finalBranchesToMerge = append(finalBranchesToMerge, branchesToMerge[i])
		}
	}

	// If no PRs to merge, return early
	if len(finalBranchesToMerge) == 0 {
		return nil, validation, fmt.Errorf("no open PRs found to merge")
	}

	// 6. Detect branching stacks (siblings)
	mergedSet := make(map[string]bool)
	for _, branch := range allBranches {
		mergedSet[branch] = true
	}

	for _, ancestor := range allBranches {
		if ancestor == eng.Trunk().GetName() {
			continue
		}
		children := graph.ChildBranches(eng.GetBranch(ancestor))
		for _, child := range children {
			if !mergedSet[child.GetName()] {
				validation.Infos = append(validation.Infos, fmt.Sprintf("Branch %s is not part of this merge and will be moved to %s", child.GetName(), eng.Trunk().GetName()))
			}
		}
	}

	// 7. Build ordered steps based on strategy
	var steps []PlanStep
	switch opts.Strategy {
	case StrategySquash:
		steps = buildSquashSteps(finalBranchesToMerge, upstackBranches, opts.Wait)
	default: // StrategyBottomUp or default
		steps = buildBottomUpSteps(finalBranchesToMerge, upstackBranches)
	}

	plan := &Plan{
		Strategy:        opts.Strategy,
		CurrentBranch:   planCurrentBranch,
		BranchesToMerge: finalBranchesToMerge,
		UpstackBranches: upstackBranches,
		Steps:           steps,
		Warnings:        validation.Warnings,
		Infos:           validation.Infos,
		CreatedAt:       time.Now(),
	}

	return plan, validation, nil
}

func buildBottomUpSteps(branchesToMerge []BranchMergeInfo, upstackBranches []string) []PlanStep {
	steps := []PlanStep{}

	for i, branchInfo := range branchesToMerge {
		steps = append(steps, PlanStep{
			StepType:     StepWaitCI,
			BranchName:   branchInfo.BranchName,
			PRNumber:     branchInfo.PRNumber,
			Description:  fmt.Sprintf("Wait for CI checks on PR #%d (%s)", branchInfo.PRNumber, branchInfo.BranchName),
			WaitTimeout:  DefaultCITimeout,
			ExpectChecks: branchInfo.HasChecks(),
		})

		steps = append(steps, PlanStep{
			StepType:    StepMergePR,
			BranchName:  branchInfo.BranchName,
			PRNumber:    branchInfo.PRNumber,
			Description: fmt.Sprintf("Merge PR #%d (%s)", branchInfo.PRNumber, branchInfo.BranchName),
		})

		steps = append(steps, PlanStep{
			StepType:    StepPullTrunk,
			BranchName:  "",
			PRNumber:    0,
			Description: "Pull trunk to get merged changes",
		})

		if i < len(branchesToMerge)-1 {
			nextBranch := branchesToMerge[i+1].BranchName
			steps = append(steps, PlanStep{
				StepType:    StepRestack,
				BranchName:  nextBranch,
				PRNumber:    0,
				Description: fmt.Sprintf("Restack %s onto trunk", nextBranch),
			})
		}
	}

	for _, branchInfo := range branchesToMerge {
		steps = append(steps, PlanStep{
			StepType:    StepDeleteBranch,
			BranchName:  branchInfo.BranchName,
			PRNumber:    0,
			Description: fmt.Sprintf("Delete local branch %s", branchInfo.BranchName),
		})
	}

	for _, upstackBranch := range upstackBranches {
		steps = append(steps, PlanStep{
			StepType:    StepRestack,
			BranchName:  upstackBranch,
			PRNumber:    0,
			Description: fmt.Sprintf("Restack %s onto trunk", upstackBranch),
		})
	}

	return steps
}

func buildSquashSteps(branchesToMerge []BranchMergeInfo, upstackBranches []string, wait bool) []PlanStep {
	steps := []PlanStep{}

	desc := "Consolidate %d branches into single PR"
	if wait {
		desc += " and wait for merge"
	} else {
		desc += " (manual merge required)"
	}

	// Single consolidation step
	steps = append(steps, PlanStep{
		StepType:    StepConsolidate,
		Description: fmt.Sprintf(desc, len(branchesToMerge)),
	})

	// Post-consolidation cleanup steps - only if waiting for auto-merge
	if wait {
		steps = append(steps, PlanStep{
			StepType:    StepPullTrunk,
			Description: "Pull trunk to get merged consolidation changes",
		})

		for _, branchInfo := range branchesToMerge {
			steps = append(steps, PlanStep{
				StepType:    StepDeleteBranch,
				BranchName:  branchInfo.BranchName,
				PRNumber:    0,
				Description: fmt.Sprintf("Delete consolidated branch %s", branchInfo.BranchName),
			})
		}

		for _, upstackBranch := range upstackBranches {
			steps = append(steps, PlanStep{
				StepType:    StepRestack,
				BranchName:  upstackBranch,
				PRNumber:    0,
				Description: fmt.Sprintf("Restack %s onto trunk", upstackBranch),
			})
		}
	}

	return steps
}

// FormatMergePlan returns a human-readable representation of a merge plan
func FormatMergePlan(plan *Plan, validation *PlanValidation) string {
	var result strings.Builder

	result.WriteString(fmt.Sprintf("Merge Strategy: %s\n", plan.Strategy))
	result.WriteString(fmt.Sprintf("Current Branch: %s\n", plan.CurrentBranch))
	result.WriteString("\n")

	if len(validation.Errors) > 0 {
		result.WriteString("Errors:\n")
		for _, err := range validation.Errors {
			result.WriteString(fmt.Sprintf("  ✗ %s\n", err))
		}
		result.WriteString("\n")
	}

	if len(validation.Warnings) > 0 {
		result.WriteString("Warnings:\n")
		for _, warn := range validation.Warnings {
			result.WriteString(fmt.Sprintf("  ⚠ %s\n", warn))
		}
		result.WriteString("\n")
	}

	if len(validation.Infos) > 0 {
		result.WriteString("Information:\n")
		for _, info := range validation.Infos {
			result.WriteString(fmt.Sprintf("  • %s\n", info))
		}
		result.WriteString("\n")
	}

	result.WriteString("Merge Plan:\n")
	if plan.Strategy == StrategySquash {
		// For squash strategy, show grouped steps that match the TUI display
		result.WriteString(formatSquashPlanSteps(plan))
	} else {
		// For bottom-up strategy, show individual steps
		for i, step := range plan.Steps {
			result.WriteString(fmt.Sprintf("  %d. %s\n", i+1, step.Description))
		}
	}

	return result.String()
}

// formatSquashPlanSteps formats squash plan steps in a grouped format
func formatSquashPlanSteps(plan *Plan) string {
	var result strings.Builder
	stepNum := 1

	// 1. Consolidation step
	hasConsolidate := false
	for _, step := range plan.Steps {
		if step.StepType == StepConsolidate {
			hasConsolidate = true
			result.WriteString(fmt.Sprintf("  %d. %s\n", stepNum, step.Description))
			stepNum++
			break
		}
	}

	// 2. Sync trunk (if present)
	for _, step := range plan.Steps {
		if step.StepType == StepPullTrunk {
			result.WriteString(fmt.Sprintf("  %d. Sync trunk\n", stepNum))
			stepNum++
			break
		}
	}

	// 3. Cleanup branches (count delete steps)
	deleteCount := 0
	for _, step := range plan.Steps {
		if step.StepType == StepDeleteBranch {
			deleteCount++
		}
	}
	if deleteCount > 0 {
		result.WriteString(fmt.Sprintf("  %d. Cleanup %d merged branches\n", stepNum, deleteCount))
		stepNum++
	}

	// 4. Restack upstack branches (if any)
	if len(plan.UpstackBranches) > 0 {
		result.WriteString(fmt.Sprintf("  %d. Restack %d upstack branches\n", stepNum, len(plan.UpstackBranches)))
	}

	// If no consolidate step found (shouldn't happen), fall back to listing all steps
	if !hasConsolidate {
		result.Reset()
		for i, step := range plan.Steps {
			result.WriteString(fmt.Sprintf("  %d. %s\n", i+1, step.Description))
		}
	}

	return result.String()
}

// IsSingleBranchLeafMerge returns true if this is a simple merge of a single
// leaf branch (no children, no upstack work needed). This is used to streamline
// the UI for simple single-PR merges.
func IsSingleBranchLeafMerge(plan *Plan, graph *engine.StackGraph) bool {
	if len(plan.BranchesToMerge) != 1 {
		return false
	}
	if len(plan.UpstackBranches) > 0 {
		return false
	}
	return AllBranchesAreLeaves(graph, plan.BranchesToMerge)
}

// AllBranchesAreLeaves checks if all branches in the plan have no children in the stack graph.
// This is used to determine if individual merging is possible (only leaf branches can be
// merged individually without affecting other branches).
func AllBranchesAreLeaves(graph *engine.StackGraph, branches []BranchMergeInfo) bool {
	for _, branchInfo := range branches {
		node := graph.GetNode(branchInfo.BranchName)
		if node == nil {
			continue
		}
		if !graph.IsLeaf(node.Branch) {
			return false
		}
	}
	return true
}

// IndividualMergeStatus contains the result of checking if individual merge is possible
type IndividualMergeStatus struct {
	CanMerge       bool            // True if all PRs can be merged individually
	MergeableState map[string]bool // Per-branch mergeable state (true = mergeable)
	BlockingReason string          // Reason why individual merge is blocked (if any)
}

// CanMergeIndividually checks if all PRs can be merged individually by verifying:
// 1. All branches are leaf branches (no children)
// 2. All PRs have GitHub mergeable state = MERGEABLE (no conflicts with trunk)
//
// Returns the status including per-branch mergeable states for display purposes.
func CanMergeIndividually(ctx context.Context, gitRunner git.Runner, githubClient github.Client, graph *engine.StackGraph, branches []BranchMergeInfo) (*IndividualMergeStatus, error) {
	status := &IndividualMergeStatus{
		MergeableState: make(map[string]bool),
	}

	// Check 1: All branches must be leaves
	if !AllBranchesAreLeaves(graph, branches) {
		status.BlockingReason = "some branches have children"
		return status, nil
	}

	// Check 2: All PRs must have MERGEABLE state
	owner, repo := githubClient.GetOwnerRepo()

	for _, branchInfo := range branches {
		// Get the PR info from GitHub to obtain NodeID
		prInfo, err := githubClient.GetPullRequest(ctx, owner, repo, branchInfo.PRNumber)
		if err != nil {
			return nil, fmt.Errorf("failed to get PR #%d for %s: %w", branchInfo.PRNumber, branchInfo.BranchName, err)
		}

		if prInfo == nil || prInfo.NodeID == "" {
			status.BlockingReason = fmt.Sprintf("branch %s has no PR node ID", branchInfo.BranchName)
			return status, nil
		}

		// Check mergeable state via GraphQL
		mergeState, err := github.GetPRMergeableState(ctx, gitRunner, prInfo.NodeID)
		if err != nil {
			return nil, fmt.Errorf("failed to get mergeable state for %s: %w", branchInfo.BranchName, err)
		}

		status.MergeableState[branchInfo.BranchName] = mergeState.Mergeable

		if !mergeState.Mergeable {
			status.BlockingReason = fmt.Sprintf("PR for %s has conflicts with trunk", branchInfo.BranchName)
		}
	}

	// All checks passed if no blocking reason was set
	status.CanMerge = status.BlockingReason == ""
	return status, nil
}
