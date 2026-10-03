package shippable

import (
	"context"

	"github.com/getstackit/stackit/internal/actions/stackview"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/github"
)

// Analyzer analyzes stacks for shippability.
type Analyzer struct {
	eng    engine.StackView
	client github.ChecksReader
}

// NewAnalyzer creates a new shippability analyzer.
func NewAnalyzer(eng engine.StackView, client github.ChecksReader) *Analyzer {
	return &Analyzer{
		eng:    eng,
		client: client,
	}
}

// AnalyzeAll discovers and analyzes all stacks for shippability.
func (a *Analyzer) AnalyzeAll(ctx context.Context) (*AnalysisResult, error) {
	// Discover all stacks
	stacks := stackview.DiscoverStacks(a.eng)
	if len(stacks) == 0 {
		return &AnalysisResult{Stacks: []Stack{}}, nil
	}

	// Batch fetch PR/CI status from GitHub
	var statusMap github.ChecksByBranch
	if a.client != nil {
		var err error
		statusMap, err = a.client.BatchGetPRChecksStatus(ctx, stacks.AllBranchNames())
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

		result.addCount(analyzed.Status)
	}

	return result, nil
}

// AnalyzeAllLocal analyzes stacks using only locally cached metadata. It never
// contacts the forge or lists remote refs, which makes it suitable for views
// that must remain responsive and useful while offline.
func (a *Analyzer) AnalyzeAllLocal() (*AnalysisResult, error) {
	stacks := stackview.DiscoverStacks(a.eng)

	result := &AnalysisResult{Stacks: make([]Stack, 0, len(stacks))}
	for _, stack := range stacks {
		analyzed := a.analyzeStackLocal(stack)
		result.Stacks = append(result.Stacks, analyzed)
		result.addCount(analyzed.Status)
	}

	return result, nil
}

// AnalyzeStack analyzes a single stack for shippability.
// This can be used when you already have a stack and status information.
func (a *Analyzer) AnalyzeStack(ctx context.Context, stack stackview.StackInfo) (*Stack, error) {
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

	remoteStatuses := a.remoteStatusProviderFor(ctx, []stackview.StackInfo{stack})
	analyzed := a.analyzeStack(stack, statusMap, remoteStatuses)
	return &analyzed, nil
}

// remoteStatusProviderFor builds a single remote-status provider shared across
// the given stacks, so remote branch SHAs are listed once for all of them
// instead of once per stack. Only branches with updateable PRs need remote
// status; missing and draft PRs return before the not-pushed check, so
// incomplete stacks stay offline.
func (a *Analyzer) remoteStatusProviderFor(ctx context.Context, stacks []stackview.StackInfo) *branchRemoteStatusProvider {
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
func (a *Analyzer) analyzeStack(stack stackview.StackInfo, statusMap github.ChecksByBranch, remoteStatuses *branchRemoteStatusProvider) Stack {
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
// establish: whether every branch has an open, non-draft PR. Without forge data
// CI and review state are unknown, so a stack that passes every local check is
// StatusUnverified, never StatusShippable. Forge and remote status are left for
// callers that explicitly opt into them.
func (a *Analyzer) analyzeStackLocal(stack stackview.StackInfo) Stack {
	result := Stack{
		Stack:       stack,
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
		prInfo, err := a.eng.GetBranch(branchName).GetPrInfo()
		if err != nil || prInfo == nil || prInfo.Number() == nil {
			result.BlockingPRs = append(result.BlockingPRs, BlockingPR{Branch: branchName, Reason: ReasonNoPR})
			continue
		}

		blocking := BlockingPR{Branch: branchName, PRNumber: *prInfo.Number()}
		switch {
		case prInfo.State() == git.PRStateClosed:
			blocking.Reason = ReasonPRClosed
		case prInfo.State() == git.PRStateMerged:
			blocking.Reason = ReasonPRMerged
		case prInfo.IsDraft():
			blocking.Reason = ReasonDraft
		default:
			continue
		}
		result.BlockingPRs = append(result.BlockingPRs, blocking)
	}

	if len(result.BlockingPRs) == 0 {
		// Every PR is open and ready for review, but nothing local can say
		// whether CI passed or a reviewer approved.
		result.Status = StatusUnverified
		return result
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
	eng      engine.StackView
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
// It classifies from forge state (CI, review, draft, pushed); the API's stack
// summaries use the local-only stackview.LocalStatus instead, which shares
// the status strings but not the rules.
func determineStatus(result Stack) Status {
	// Check for incomplete state (missing PR or draft)
	for _, blocking := range result.BlockingPRs {
		if blocking.Reason == ReasonNoPR || blocking.Reason == ReasonDraft {
			return StatusIncomplete
		}
	}

	// Check for blocked state (CI failing, changes requested, not pushed, or a
	// PR that is no longer open)
	for _, blocking := range result.BlockingPRs {
		switch blocking.Reason {
		case ReasonCIFailing, ReasonChangesRequested, ReasonNotPushed, ReasonPRClosed, ReasonPRMerged:
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
