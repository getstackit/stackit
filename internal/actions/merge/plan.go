package merge

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/errors"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/github"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/utils"
)

// Strategy defines how PRs in the stack should be merged
type Strategy string

const (
	// StrategyBottomUp merges PRs from the bottom of the stack up to the current branch
	StrategyBottomUp Strategy = "bottom-up"
	// StrategyShip creates a single PR containing all stack commits for atomic merging
	StrategyShip Strategy = "ship"
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

// MergeBranches is an ordered collection of branches to merge.
type MergeBranches []BranchMergeInfo

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

// AnyHasChecks returns true if any branch has CI checks configured.
func (branches MergeBranches) AnyHasChecks() bool {
	for _, b := range branches {
		if b.HasChecks() {
			return true
		}
	}
	return false
}

// Names returns the branch names in merge order.
func (branches MergeBranches) Names() []string {
	names := make([]string, len(branches))
	for i, branch := range branches {
		names[i] = branch.BranchName
	}
	return names
}

// AllAreLeaves reports whether every branch is a leaf in graph.
func (branches MergeBranches) AllAreLeaves(graph *engine.StackGraph) bool {
	for _, branchInfo := range branches {
		node := graph.GetNode(branchInfo.BranchName)
		if node == nil || !graph.IsLeaf(node.Branch) {
			return false
		}
	}
	return true
}

// AnyPRHasChecks is retained for callers that have not migrated to MergeBranches.AnyHasChecks.
func AnyPRHasChecks(branches []BranchMergeInfo) bool { return MergeBranches(branches).AnyHasChecks() }

// BranchNames returns the names of all branches to be merged, in order.
func (p *Plan) BranchNames() []string {
	names := make([]string, len(p.BranchesToMerge))
	for i, b := range p.BranchesToMerge {
		names[i] = b.BranchName
	}
	return names
}

// Plan is the complete plan for a merge operation
type Plan struct {
	Strategy        Strategy
	CurrentBranch   string
	BranchesToMerge MergeBranches // Branches that will be merged (bottom to top)
	UpstackBranches []string      // Branches above current that will be restacked
	Steps           []PlanStep    // Ordered steps to execute
	Warnings        []string      // Non-blocking warnings
	Infos           []string      // Informational messages
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

// CollectedBranches holds the intermediate result of branch collection.
// This allows the wizard to collect branches once, then build plans with different strategies.
type CollectedBranches struct {
	BranchesToMerge MergeBranches
	UpstackBranches []string
	CurrentBranch   string
	Validation      *PlanValidation
}

// mergePlanEngine is a minimal interface needed for creating a merge plan
type mergePlanEngine interface {
	engine.BranchReader
	engine.PRManager
	engine.SyncManager
	engine.MetadataInspector
}

// CreateMergePlan analyzes the current state and builds a merge plan.
// This is a convenience wrapper that calls CollectMergeBranches + BuildMergePlan.
func CreateMergePlan(ctx context.Context, eng mergePlanEngine, splog output.Output, githubClient github.Client, opts CreatePlanOptions) (*Plan, *PlanValidation, error) {
	collected, err := CollectMergeBranches(ctx, eng, splog, githubClient, opts)
	if err != nil {
		return nil, nil, err
	}

	plan := BuildMergePlan(collected, opts.Strategy, opts.Wait)
	return plan, collected.Validation, nil
}

// CollectMergeBranches gathers branches, metadata, CI status, and validation
// without building strategy-specific steps. This is the expensive part that
// calls GitHub APIs and should only run once per wizard session.
func CollectMergeBranches(ctx context.Context, eng mergePlanEngine, splog output.Output, githubClient github.Client, opts CreatePlanOptions) (*CollectedBranches, error) {
	// 1. Get target branch, validate not on trunk
	var targetBranch engine.Branch
	if opts.TargetBranch != "" {
		targetBranch = eng.GetBranch(opts.TargetBranch)
		if !targetBranch.IsTracked() && !targetBranch.IsTrunk() {
			return nil, fmt.Errorf("branch %s is not tracked by stackit", opts.TargetBranch)
		}
	} else {
		cb := eng.CurrentBranch()
		if cb == nil {
			return nil, errors.ErrNotOnBranch
		}
		targetBranch = *cb
	}

	var allBranches []string
	var planCurrentBranch string

	// Build a StackGraph for efficient traversals
	graph := eng.Graph(engine.SortStrategyAlphabetical)

	if opts.Scope != "" {
		// Collect all branches with the specified scope
		scopeBranches := engine.Branches{}
		for _, b := range eng.AllBranches() {
			if !b.IsTrunk() && eng.GetScope(b).String() == opts.Scope {
				scopeBranches = scopeBranches.Append(b)
			}
		}
		if len(scopeBranches) == 0 {
			return nil, fmt.Errorf("no branches found with scope %s", opts.Scope)
		}

		// Sort branches topologically so we merge in correct order (bottom to top)
		scopeBranches = eng.SortBranchesTopologically(scopeBranches)
		allBranches = scopeBranches.Names()
		// In scope mode, the "current branch" for the plan is the top-most branch in the scope
		planCurrentBranch = allBranches[len(allBranches)-1]
	} else {
		if targetBranch.IsTrunk() {
			return nil, fmt.Errorf("cannot merge from trunk. You must be on a branch that has a PR")
		}

		// Check if target branch is tracked
		if !targetBranch.IsTracked() {
			return nil, fmt.Errorf("branch %s is not tracked by stackit", targetBranch.GetName())
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
				isBeingMerged := slices.Contains(allBranches, branch.GetName())
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
	allMeta, _ := eng.BatchReadMetadataRaw(involvedBranches)
	// We don't strictly need allRevisions here yet, but it's good for cache
	_, _ = eng.GetRevisions(involvedBranches)

	remoteCtx, cancelRemote := app.WithRemoteOperationTimeout(ctx)
	defer cancelRemote()

	// Fetch CI statuses in batch if possible
	var allCheckStatuses github.ChecksByBranch
	if githubClient != nil {
		allCheckStatuses, _ = githubClient.BatchGetPRChecksStatus(remoteCtx, allBranches)
	}

	statusBranchBuilder := engine.NewBranchesBuilder(len(allBranches) + 1)
	for _, name := range allBranches {
		statusBranchBuilder.Add(eng.GetBranch(name))
	}
	statusBranchBuilder.Add(eng.Trunk())
	statusBranches := statusBranchBuilder.Build()
	remoteStatuses := eng.ReadBranchRemoteStatuses(remoteCtx, statusBranches)

	// 5. For each branch: fetch PR info, check status, CI checks in parallel
	branchesToMerge := make(MergeBranches, len(allBranches))
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

		// Determine PR state: prefer live GitHub state over local metadata
		state := prInfo.State()
		if allCheckStatuses != nil {
			if checkStatus, ok := allCheckStatuses[name]; ok && checkStatus.State != "" {
				state = checkStatus.State
			}
		}
		if state != git.PRStateOpen {
			if state == git.PRStateMerged {
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
		branchObj := eng.GetBranch(name)
		remoteStatus := remoteStatuses.ForBranch(branchObj)
		matchesRemote := remoteStatus.Matches()

		if !matchesRemote {
			// Get detailed difference information
			diffInfo := formatBranchRemoteDifference(remoteStatus)
			if diffInfo != "" {
				branchWarnings[idx] = append(branchWarnings[idx], fmt.Sprintf("Branch %s differs from remote: %s", name, diffInfo))
			} else {
				branchWarnings[idx] = append(branchWarnings[idx], fmt.Sprintf("Branch %s differs from remote", name))
			}
		}

		// Get CI check status from batch-loaded results
		checksStatus := ChecksNone
		if status := allCheckStatuses.Get(name); status != nil {
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
				if len(status.Checks) > 0 {
					checksStatus = ChecksPassing
				}
				// else: no checks configured, keep ChecksNone
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
	finalBranchesToMerge := MergeBranches{}
	validation := &PlanValidation{
		Valid:    true,
		Errors:   []string{},
		Warnings: []string{},
	}

	// Pre-flight check: Check if trunk is in sync with remote
	trunk := eng.Trunk()
	if remoteStatuses.ForBranch(trunk).Diverged() {
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
		return nil, fmt.Errorf("no open PRs found to merge")
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

	return &CollectedBranches{
		BranchesToMerge: finalBranchesToMerge,
		UpstackBranches: upstackBranches,
		CurrentBranch:   planCurrentBranch,
		Validation:      validation,
	}, nil
}

func formatBranchRemoteDifference(status engine.BranchRemoteStatus) string {
	if status.LocalSha == "" {
		return "(branch not found locally)"
	}

	localShort := shortSHA(status.LocalSha)
	if status.RemoteSha == "" {
		return fmt.Sprintf("local: %s (branch not found on remote)", localShort)
	}
	if status.Matches() {
		return ""
	}

	remoteShort := shortSHA(status.RemoteSha)
	switch {
	case status.Behind():
		return fmt.Sprintf("local is behind remote (local: %s, remote: %s)", localShort, remoteShort)
	case status.Ahead():
		return fmt.Sprintf("local is ahead of remote (local: %s, remote: %s)", localShort, remoteShort)
	case status.Diverged():
		return fmt.Sprintf("local and remote have diverged (local: %s, remote: %s)", localShort, remoteShort)
	default:
		return fmt.Sprintf("local: %s, remote: %s", localShort, remoteShort)
	}
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// BuildMergePlan builds a Plan with strategy-specific steps from collected branch data.
// This is the cheap part that only does in-memory computation.
func BuildMergePlan(collected *CollectedBranches, strategy Strategy, wait bool) *Plan {
	var steps []PlanStep
	switch strategy {
	case StrategyShip:
		steps = buildShipSteps(collected.BranchesToMerge, collected.UpstackBranches, wait)
	default: // StrategyBottomUp or default
		steps = buildBottomUpSteps(collected.BranchesToMerge, collected.UpstackBranches)
	}

	return &Plan{
		Strategy:        strategy,
		CurrentBranch:   collected.CurrentBranch,
		BranchesToMerge: collected.BranchesToMerge,
		UpstackBranches: collected.UpstackBranches,
		Steps:           steps,
		Warnings:        collected.Validation.Warnings,
		Infos:           collected.Validation.Infos,
		CreatedAt:       time.Now(),
	}
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

func buildShipSteps(branchesToMerge []BranchMergeInfo, upstackBranches []string, wait bool) []PlanStep {
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

// FormatValidationSection writes validation errors, warnings, and info to the builder.
// Used by all plan format functions to avoid duplication.
func FormatValidationSection(result *strings.Builder, validation *PlanValidation) {
	if validation == nil {
		return
	}

	if len(validation.Errors) > 0 {
		result.WriteString("Errors:\n")
		for _, err := range validation.Errors {
			fmt.Fprintf(result, "  ✗ %s\n", err)
		}
		result.WriteString("\n")
	}

	if len(validation.Warnings) > 0 {
		result.WriteString("Warnings:\n")
		for _, warn := range validation.Warnings {
			fmt.Fprintf(result, "  ⚠ %s\n", warn)
		}
		result.WriteString("\n")
	}

	if len(validation.Infos) > 0 {
		result.WriteString("Information:\n")
		for _, info := range validation.Infos {
			fmt.Fprintf(result, "  • %s\n", info)
		}
		result.WriteString("\n")
	}
}

// FormatMergePlan returns a human-readable representation of a merge plan
func FormatMergePlan(plan *Plan, validation *PlanValidation) string {
	var result strings.Builder

	fmt.Fprintf(&result, "Merge Strategy: %s\n", plan.Strategy)
	fmt.Fprintf(&result, "Current Branch: %s\n", plan.CurrentBranch)
	result.WriteString("\n")

	FormatValidationSection(&result, validation)

	result.WriteString("Merge Plan:\n")
	if plan.Strategy == StrategyShip {
		// For ship strategy, show grouped steps that match the TUI display
		result.WriteString(formatShipPlanSteps(plan))
	} else {
		// For bottom-up strategy, show individual steps
		for i, step := range plan.Steps {
			fmt.Fprintf(&result, "  %d. %s\n", i+1, step.Description)
		}
	}

	return result.String()
}

// formatShipPlanSteps formats ship plan steps in a grouped format
func formatShipPlanSteps(plan *Plan) string {
	var result strings.Builder
	stepNum := 1

	// 1. Consolidation step
	hasConsolidate := false
	for _, step := range plan.Steps {
		if step.StepType == StepConsolidate {
			hasConsolidate = true
			fmt.Fprintf(&result, "  %d. %s\n", stepNum, step.Description)
			stepNum++
			break
		}
	}

	// 2. Sync trunk (if present)
	for _, step := range plan.Steps {
		if step.StepType == StepPullTrunk {
			fmt.Fprintf(&result, "  %d. Sync trunk\n", stepNum)
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
		fmt.Fprintf(&result, "  %d. Cleanup %d merged branches\n", stepNum, deleteCount)
		stepNum++
	}

	// 4. Restack upstack branches (if any)
	if len(plan.UpstackBranches) > 0 {
		fmt.Fprintf(&result, "  %d. Restack %d upstack branches\n", stepNum, len(plan.UpstackBranches))
	}

	// If no consolidate step found (shouldn't happen), fall back to listing all steps
	if !hasConsolidate {
		result.Reset()
		for i, step := range plan.Steps {
			fmt.Fprintf(&result, "  %d. %s\n", i+1, step.Description)
		}
	}

	return result.String()
}

// IsSingleBranchLeafMerge returns true if this is a simple merge of a single
// leaf branch (no children, no upstack work needed).
//
// Why this matters: Single leaf branches with no upstack work represent the simplest
// merge case. We can offer a streamlined confirmation UX (just "Proceed?") instead
// of showing the full plan, since there are no complex steps or dependencies to review.
func IsSingleBranchLeafMerge(plan *Plan, graph *engine.StackGraph) bool {
	if len(plan.BranchesToMerge) != 1 {
		return false
	}
	if len(plan.UpstackBranches) > 0 {
		return false
	}
	return plan.BranchesToMerge.AllAreLeaves(graph)
}

// AllBranchesAreLeaves checks if all branches in the plan have no children in the stack graph.
//
// Why this matters: Only leaf branches (those with no children) can be merged individually
// without affecting other branches. Merging a non-leaf would orphan its children or require
// restacking them, making individual merge inappropriate. This check enables offering the
// "merge individually" option when all selected branches are independent leaves.
//
// Note: Branches not found in the graph (nil node) are treated as non-leaves and cause
// the function to return false. This is a fail-safe behavior - if we can't verify a
// branch's structure, we don't allow individual merging.
func AllBranchesAreLeaves(graph *engine.StackGraph, branches []BranchMergeInfo) bool {
	return MergeBranches(branches).AllAreLeaves(graph)
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
// Returns error if GitHub API call fails; check CanMerge field and BlockingReason for results.
func CanMergeIndividually(ctx context.Context, gitRunner git.Runner, githubClient github.Client, graph *engine.StackGraph, branches []BranchMergeInfo) (*IndividualMergeStatus, error) {
	status := &IndividualMergeStatus{
		MergeableState: make(map[string]bool),
	}

	// Check 1: All branches must be leaves
	if !MergeBranches(branches).AllAreLeaves(graph) {
		status.BlockingReason = "some branches have children"
		return status, nil
	}

	// Check 2: All PRs must have MERGEABLE state
	// Resolve all PR node IDs in a single GraphQL query instead of one REST call per PR.
	prNumbers := make([]int, 0, len(branches))
	for _, branchInfo := range branches {
		prNumbers = append(prNumbers, branchInfo.PRNumber)
	}
	nodeIDByNumber, err := github.BatchGetPRNodeIDsGraphQL(ctx, gitRunner, githubClient.Repo(), prNumbers)
	if err != nil {
		return nil, fmt.Errorf("failed to batch get PR node IDs: %w", err)
	}

	nodeIDToBranch := make(map[string]string, len(branches))
	nodeIDs := make([]string, 0, len(branches))
	for _, branchInfo := range branches {
		nodeID := nodeIDByNumber[branchInfo.PRNumber]
		if nodeID == "" {
			status.BlockingReason = fmt.Sprintf("branch %s has no PR node ID", branchInfo.BranchName)
			return status, nil
		}
		nodeIDToBranch[nodeID] = branchInfo.BranchName
		nodeIDs = append(nodeIDs, nodeID)
	}

	// Batch fetch all mergeable states in a single GraphQL query
	mergeStates, err := github.BatchGetPRMergeableStates(ctx, gitRunner, nodeIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to batch get mergeable states: %w", err)
	}

	// Process results and check for any non-mergeable PRs
	for nodeID, branchName := range nodeIDToBranch {
		mergeState, ok := mergeStates[nodeID]
		if !ok {
			status.BlockingReason = fmt.Sprintf("could not get mergeable state for %s", branchName)
			status.CanMerge = false
			return status, nil
		}

		status.MergeableState[branchName] = mergeState.Mergeable

		if !mergeState.Mergeable {
			status.BlockingReason = fmt.Sprintf("PR for %s has conflicts with trunk", branchName)
		}
	}

	// All checks passed if no blocking reason was set
	status.CanMerge = status.BlockingReason == ""
	return status, nil
}
