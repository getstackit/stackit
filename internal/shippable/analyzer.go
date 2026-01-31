package shippable

import (
	"context"

	"stackit.dev/stackit/internal/actions/merge"
	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/github"
)

// Analyzer analyzes stacks for shippability.
type Analyzer struct {
	eng    engine.BranchReader
	client github.Client
}

// NewAnalyzer creates a new shippability analyzer.
func NewAnalyzer(eng engine.BranchReader, client github.Client) *Analyzer {
	return &Analyzer{
		eng:    eng,
		client: client,
	}
}

// AnalyzeAll discovers and analyzes all stacks for shippability.
func (a *Analyzer) AnalyzeAll(ctx context.Context) (*AnalysisResult, error) {
	// Discover all stacks
	stacks, err := merge.DiscoverStacks(a.eng)
	if err != nil {
		return nil, err
	}

	if len(stacks) == 0 {
		return &AnalysisResult{Stacks: []Stack{}}, nil
	}

	// Collect all branches for batch status fetch
	var allBranches []string
	for _, stack := range stacks {
		allBranches = append(allBranches, stack.AllBranches...)
	}

	// Batch fetch PR/CI status from GitHub
	var statusMap map[string]*github.CheckStatus
	if a.client != nil {
		statusMap, err = a.client.BatchGetPRChecksStatus(ctx, allBranches)
		if err != nil {
			// Non-fatal: continue with analysis but without GitHub status
			statusMap = make(map[string]*github.CheckStatus)
		}
	} else {
		statusMap = make(map[string]*github.CheckStatus)
	}

	// Analyze each stack
	result := &AnalysisResult{
		Stacks: make([]Stack, 0, len(stacks)),
	}

	for _, stack := range stacks {
		analyzed := a.analyzeStack(stack, statusMap)
		result.Stacks = append(result.Stacks, analyzed)

		// Update counts
		switch analyzed.Status {
		case StatusShippable:
			result.ShippableCount++
		case StatusPending:
			result.PendingCount++
		case StatusBlocked:
			result.BlockedCount++
		case StatusIncomplete:
			result.IncompleteCount++
		}
	}

	return result, nil
}

// AnalyzeStack analyzes a single stack for shippability.
// This can be used when you already have a stack and status information.
func (a *Analyzer) AnalyzeStack(ctx context.Context, stack merge.MultiStackInfo) (*Stack, error) {
	// Fetch PR/CI status for this stack's branches
	var statusMap map[string]*github.CheckStatus
	var err error
	if a.client != nil {
		statusMap, err = a.client.BatchGetPRChecksStatus(ctx, stack.AllBranches)
		if err != nil {
			// Non-fatal: continue without GitHub status
			statusMap = make(map[string]*github.CheckStatus)
		}
	} else {
		statusMap = make(map[string]*github.CheckStatus)
	}

	analyzed := a.analyzeStack(stack, statusMap)
	return &analyzed, nil
}

// analyzeStack performs the actual analysis of a single stack.
func (a *Analyzer) analyzeStack(stack merge.MultiStackInfo, statusMap map[string]*github.CheckStatus) Stack {
	result := Stack{
		Stack:       stack,
		ApprovalOK:  true,
		GitHubCIOK:  true,
		BlockingPRs: make([]BlockingPR, 0),
	}

	// Check each branch in the stack
	for _, branchName := range stack.AllBranches {
		// Extract author from first branch with PR status
		if result.Author == "" {
			if status, ok := statusMap[branchName]; ok && status != nil && status.Author != "" {
				result.Author = status.Author
			}
		}

		blocking := a.analyzeBranch(branchName, statusMap)
		if blocking != nil {
			result.BlockingPRs = append(result.BlockingPRs, *blocking)

			// Update approval/CI status based on blocking reason
			switch blocking.Reason {
			case ReasonChangesRequested, ReasonReviewRequired:
				result.ApprovalOK = false
			case ReasonCIFailing:
				result.GitHubCIOK = false
			case ReasonCIPending:
				// Pending doesn't fail the check, but indicates status is pending
			case ReasonNoPR, ReasonDraft:
				// These affect overall status but not individual flags
			case ReasonNotPushed:
				// Branch not pushed - this blocks shipping
			}
		}
	}

	// Determine overall status
	result.Status = determineStatus(result)

	return result
}

// analyzeBranch analyzes a single branch and returns blocking info if any.
func (a *Analyzer) analyzeBranch(branchName string, statusMap map[string]*github.CheckStatus) *BlockingPR {
	// Get PR info from engine metadata
	branch := a.eng.GetBranch(branchName)
	prInfo, err := branch.GetPrInfo()

	// Check if PR exists
	if err != nil || prInfo == nil || prInfo.Number() == nil {
		return &BlockingPR{
			Branch:   branchName,
			PRNumber: 0,
			Reason:   ReasonNoPR,
		}
	}

	prNumber := *prInfo.Number()

	// Check if PR is draft
	if prInfo.IsDraft() {
		return &BlockingPR{
			Branch:   branchName,
			PRNumber: prNumber,
			Reason:   ReasonDraft,
		}
	}

	// Check if local branch matches remote
	// This is critical for shipping: if local differs from remote, the octopus merge
	// will use local SHAs but PRs track remote SHAs, so GitHub won't auto-close them
	remoteStatus, err := a.eng.GetBranchRemoteStatus(branch)
	if err == nil && !remoteStatus.Matches() {
		return &BlockingPR{
			Branch:   branchName,
			PRNumber: prNumber,
			Reason:   ReasonNotPushed,
		}
	}

	// Check GitHub status
	status, hasStatus := statusMap[branchName]
	if !hasStatus || status == nil {
		// No status means we can't determine shippability from GitHub
		// This could be because there's no open PR or the branch isn't tracked
		return nil
	}

	// Check review decision
	if status.ReviewDecision == github.ReviewDecisionChangesRequested {
		return &BlockingPR{
			Branch:   branchName,
			PRNumber: prNumber,
			Reason:   ReasonChangesRequested,
		}
	}

	// Only block if reviews are explicitly required by the repo settings
	// Empty ReviewDecision means reviews are not required for this repo
	if status.ReviewDecision == github.ReviewDecisionReviewRequired {
		if !status.IsApproved() {
			return &BlockingPR{
				Branch:   branchName,
				PRNumber: prNumber,
				Reason:   ReasonReviewRequired,
			}
		}
	}

	// Check CI status
	if status.HasPendingChecks() {
		return &BlockingPR{
			Branch:   branchName,
			PRNumber: prNumber,
			Reason:   ReasonCIPending,
		}
	}

	if status.HasFailingChecks() {
		return &BlockingPR{
			Branch:   branchName,
			PRNumber: prNumber,
			Reason:   ReasonCIFailing,
		}
	}

	// No blocking issues found
	return nil
}

// determineStatus determines the overall Status based on analysis results.
func determineStatus(result Stack) Status {
	// Check for incomplete state (missing PR or draft)
	for _, blocking := range result.BlockingPRs {
		if blocking.Reason == ReasonNoPR || blocking.Reason == ReasonDraft {
			return StatusIncomplete
		}
	}

	// Check for blocked state (CI failing, changes requested, or not pushed)
	for _, blocking := range result.BlockingPRs {
		if blocking.Reason == ReasonCIFailing || blocking.Reason == ReasonChangesRequested || blocking.Reason == ReasonNotPushed {
			return StatusBlocked
		}
	}

	// Check for pending state (CI pending or review required)
	for _, blocking := range result.BlockingPRs {
		if blocking.Reason == ReasonCIPending || blocking.Reason == ReasonReviewRequired {
			return StatusPending
		}
	}

	// All checks pass
	if result.ApprovalOK && result.GitHubCIOK {
		return StatusShippable
	}

	// Default to pending if we can't determine status
	return StatusPending
}

// GetStatusDescription returns a human-readable description of a Status.
func GetStatusDescription(status Status) string {
	switch status {
	case StatusShippable:
		return "Ready to ship"
	case StatusPending:
		return "Waiting on CI or review"
	case StatusBlocked:
		return "CI failed or changes requested"
	case StatusIncomplete:
		return "Missing PRs or has drafts"
	default:
		return "Unknown status"
	}
}

// GetBlockingReasonDescription returns a human-readable description of a BlockingReason.
func GetBlockingReasonDescription(reason BlockingReason) string {
	switch reason {
	case ReasonChangesRequested:
		return "Changes requested"
	case ReasonCIFailing:
		return "CI failing"
	case ReasonCIPending:
		return "CI pending"
	case ReasonDraft:
		return "Draft PR"
	case ReasonNoPR:
		return "No PR"
	case ReasonReviewRequired:
		return "Review required"
	case ReasonNotPushed:
		return "Not pushed"
	default:
		return "Unknown"
	}
}
