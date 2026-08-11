package shippable

import (
	"context"

	"github.com/getstackit/stackit/internal/actions/merge"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/github"
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
	var statusMap github.ChecksByBranch
	if a.client != nil {
		statusMap, err = a.client.BatchGetPRChecksStatus(ctx, allBranches)
		if err != nil {
			// Non-fatal: continue with analysis but without GitHub status
			statusMap = make(github.ChecksByBranch)
		}
	} else {
		statusMap = make(github.ChecksByBranch)
	}

	// Analyze each stack, sharing one remote-status provider so remote SHAs
	// are listed once for the whole run instead of once per stack.
	remoteStatuses := a.remoteStatusProviderFor(ctx, stacks)

	result := &AnalysisResult{
		Stacks: make([]Stack, 0, len(stacks)),
	}

	for _, stack := range stacks {
		analyzed := a.analyzeStack(stack, statusMap, remoteStatuses)
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

// AnalyzeAllLocal analyzes stacks using only locally cached metadata. It never
// contacts the forge or lists remote refs, which makes it suitable for views
// that must remain responsive and useful while offline.
func (a *Analyzer) AnalyzeAllLocal() (*AnalysisResult, error) {
	stacks, err := merge.DiscoverStacks(a.eng)
	if err != nil {
		return nil, err
	}

	result := &AnalysisResult{Stacks: make([]Stack, 0, len(stacks))}
	for _, stack := range stacks {
		analyzed := a.analyzeStackLocal(stack)
		result.Stacks = append(result.Stacks, analyzed)

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
	var statusMap github.ChecksByBranch
	var err error
	if a.client != nil {
		statusMap, err = a.client.BatchGetPRChecksStatus(ctx, stack.AllBranches)
		if err != nil {
			// Non-fatal: continue without GitHub status
			statusMap = make(github.ChecksByBranch)
		}
	} else {
		statusMap = make(github.ChecksByBranch)
	}

	remoteStatuses := a.remoteStatusProviderFor(ctx, []merge.MultiStackInfo{stack})
	analyzed := a.analyzeStack(stack, statusMap, remoteStatuses)
	return &analyzed, nil
}

// remoteStatusProviderFor builds a single remote-status provider shared across
// the given stacks, so remote branch SHAs are listed once for all of them
// instead of once per stack. Only branches with updateable PRs need remote
// status; missing and draft PRs return before the not-pushed check, so
// incomplete stacks stay offline.
func (a *Analyzer) remoteStatusProviderFor(ctx context.Context, stacks []merge.MultiStackInfo) *branchRemoteStatusProvider {
	var remoteStatusBranches engine.Branches
	for _, stack := range stacks {
		for _, branchName := range stack.AllBranches {
			branch := a.eng.GetBranch(branchName)
			prInfo, err := branch.GetPrInfo()
			if err == nil && prInfo != nil && prInfo.Number() != nil && !prInfo.IsDraft() {
				remoteStatusBranches = append(remoteStatusBranches, branch)
			}
		}
	}
	return &branchRemoteStatusProvider{
		ctx:      ctx,
		eng:      a.eng,
		branches: remoteStatusBranches,
	}
}

// analyzeStack performs the actual analysis of a single stack.
func (a *Analyzer) analyzeStack(stack merge.MultiStackInfo, statusMap github.ChecksByBranch, remoteStatuses *branchRemoteStatusProvider) Stack {
	result := Stack{
		Stack:       stack,
		ApprovalOK:  true,
		GitHubCIOK:  true,
		BlockingPRs: make([]BlockingPR, 0),
	}

	// Get display title from root branch (PR title > commit subject > branch name)
	if stack.RootBranch != "" {
		rootBranch := a.eng.GetBranch(stack.RootBranch)
		if prInfo, err := rootBranch.GetPrInfo(); err == nil && prInfo != nil && prInfo.Title() != "" {
			result.PRTitle = prInfo.Title()
		} else {
			// Fall back to commit subject (DefaultPRTitle returns commit subject or branch name)
			result.PRTitle = rootBranch.DefaultPRTitle()
		}
	}

	// Check each branch in the stack
	for _, branchName := range stack.AllBranches {
		// Extract author from first branch with PR status
		if result.Author == "" {
			if status := statusMap.Get(branchName); status != nil && status.Author != "" {
				result.Author = status.Author
			}
		}

		blocking := a.analyzeBranch(branchName, statusMap, remoteStatuses)
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

// analyzeStackLocal reports only the state that local branch metadata can
// establish: whether every branch has a non-draft PR. Forge and remote status
// are deliberately left for callers that explicitly opt into them.
func (a *Analyzer) analyzeStackLocal(stack merge.MultiStackInfo) Stack {
	result := Stack{
		Stack:       stack,
		ApprovalOK:  true,
		GitHubCIOK:  true,
		BlockingPRs: make([]BlockingPR, 0),
	}

	if stack.RootBranch != "" {
		rootBranch := a.eng.GetBranch(stack.RootBranch)
		if prInfo, err := rootBranch.GetPrInfo(); err == nil && prInfo != nil && prInfo.Title() != "" {
			result.PRTitle = prInfo.Title()
		} else {
			result.PRTitle = rootBranch.DefaultPRTitle()
		}
	}

	for _, branchName := range stack.AllBranches {
		branch := a.eng.GetBranch(branchName)
		prInfo, err := branch.GetPrInfo()
		if err != nil || prInfo == nil || prInfo.Number() == nil {
			result.BlockingPRs = append(result.BlockingPRs, BlockingPR{Branch: branchName, Reason: ReasonNoPR})
			continue
		}
		if prInfo.IsDraft() {
			result.BlockingPRs = append(result.BlockingPRs, BlockingPR{
				Branch:   branchName,
				PRNumber: *prInfo.Number(),
				Reason:   ReasonDraft,
			})
		}
	}

	result.Status = determineStatus(result)
	return result
}

// analyzeBranch analyzes a single branch and returns blocking info if any.
// remoteStatuses lazily reads remote status once for branches that can reach
// the not-pushed check.
func (a *Analyzer) analyzeBranch(branchName string, statusMap github.ChecksByBranch, remoteStatuses *branchRemoteStatusProvider) *BlockingPR {
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
	if !remoteStatuses.ForBranch(branch).Matches() {
		return &BlockingPR{
			Branch:   branchName,
			PRNumber: prNumber,
			Reason:   ReasonNotPushed,
		}
	}

	// Check GitHub status
	status := statusMap.Get(branchName)
	if status == nil {
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

type branchRemoteStatusProvider struct {
	ctx      context.Context
	eng      engine.BranchReader
	branches engine.Branches

	loaded   bool
	statuses engine.BranchRemoteStatuses
}

func (p *branchRemoteStatusProvider) ForBranch(branch engine.Branch) engine.BranchRemoteStatus {
	if p == nil {
		return engine.BranchRemoteStatus{}
	}
	if !p.loaded {
		p.statuses = engine.BranchRemoteStatuses{}
		if len(p.branches) > 0 {
			p.statuses = p.eng.ReadBranchRemoteStatuses(p.ctx, p.branches)
		}
		p.loaded = true
	}
	return p.statuses.ForBranch(branch)
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
