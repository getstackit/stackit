package httpcontract

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/github"
	"github.com/getstackit/stackit/internal/utils"
)

// MapBranch converts an engine Branch and its StackNode into an API BranchResponse.
// remoteStatus, stat, commits, commitInfo, and needsRestack should each come
// from a single batch call covering all branches being mapped in the current
// request (ReadBranchRemoteStatuses, BatchBranchStats, BatchCommits,
// BatchCommitInfo, ReadBranchStatuses), not per-branch calls — per-branch
// reads spawn a git process per field per branch.
func MapBranch(eng engine.BranchReader, branch engine.Branch, node *engine.StackNode, checks *github.CheckStatus, remoteStatus engine.BranchRemoteStatus, stat engine.BranchStat, commits []string, commitInfo git.CommitInfo, needsRestack bool) BranchResponse {
	resp := BranchResponse{
		Name:         branch.GetName(),
		Depth:        node.Depth,
		IsCurrent:    branch.GetName() == eng.CurrentBranchName(),
		NeedsRestack: needsRestack,
		IsLocked:     branch.IsLocked(),
		IsFrozen:     branch.IsFrozen(),
		Children:     node.Children,
	}

	if parent := branch.GetParent(); parent != nil {
		resp.Parent = parent.GetName()
	}

	if lockReason := branch.GetLockReason(); lockReason.IsLocked() {
		resp.LockReason = string(lockReason)
	}

	if scope := eng.GetScope(branch); scope.IsDefined() {
		resp.Scope = scope.String()
	}

	resp.Revision = stat.ShortSHA
	resp.CommitCount = stat.CommitCount
	resp.LinesAdded = stat.LinesAdded
	resp.LinesDeleted = stat.LinesDeleted

	if !commitInfo.Date.IsZero() {
		resp.CommitDate = commitInfo.Date.Format(time.RFC3339)
	}
	resp.CommitAuthor = commitInfo.Author

	// Map commits
	if len(commits) > 0 {
		resp.Commits = mapCommitsWithDate(commits)
	}

	// Map PR info
	if prInfo, err := branch.GetPrInfo(); err == nil && prInfo != nil && prInfo.Number() != nil {
		resp.PR = mapPR(prInfo)
	}

	// Map CI checks
	if checks != nil {
		resp.CI = mapCI(checks)
	}

	// Map remote status
	resp.RemoteStatus = &RemoteStatus{
		Ahead:         remoteStatus.Ahead(),
		Behind:        remoteStatus.Behind(),
		Diverged:      remoteStatus.Diverged(),
		MissingRemote: remoteStatus.MissingRemote(),
	}

	return resp
}

// MapStackSummary creates a StackSummary from stack discovery info. statuses
// should come from a single ReadBranchStatuses batch call covering every
// stack being summarized in the current request, not one call per stack —
// see BranchBatchData.
func MapStackSummary(eng engine.BranchReader, graph *engine.StackGraph, rootBranch string, allBranches []string, prCount int, scope string, owner string, statuses engine.BranchStatuses) StackSummary {
	currentBranch := eng.CurrentBranchName()
	isCurrent := slices.Contains(allBranches, currentBranch)

	// Detect worktree anchor and filter it from display branches
	hasWorktree := false
	displayBranches := allBranches
	rootNode := graph.GetNode(rootBranch)
	if rootNode != nil && rootNode.Branch.IsWorktreeAnchor() {
		hasWorktree = true
		displayBranches = make([]string, 0, len(allBranches)-1)
		for _, name := range allBranches {
			if name != rootBranch {
				displayBranches = append(displayBranches, name)
			}
		}
	}

	status := computeStackStatus(graph, displayBranches, statuses)

	// Title and description come exclusively from explicit stack descriptions
	// set via `stackit describe`. No fallback to PR titles or branch names.
	var title, description string
	if rootNode != nil {
		if stackDesc := eng.GetStackDescription(rootNode.Branch); stackDesc != nil && !stackDesc.IsEmpty() {
			title = stackDesc.Title
			description = stackDesc.Description
		}
	}

	return StackSummary{
		RootBranch:  rootBranch,
		Title:       title,
		Status:      status,
		Scope:       scope,
		BranchCount: len(displayBranches),
		PRCount:     prCount,
		IsCurrent:   isCurrent,
		HasWorktree: hasWorktree,
		Description: description,
		Owner:       owner,
	}
}

// BranchBatchData holds per-branch data fetched via batch calls, keyed by
// branch name. Compute it once via FetchBranchBatchData over the union of
// branches across every stack being mapped in a request, not once per
// stack — RemoteStatuses in particular backs a network round trip
// (`git ls-remote`), whose cost does not depend on how many branches it
// covers, so paying it once for the whole repo instead of once per stack
// is a straight win.
type BranchBatchData struct {
	RemoteStatuses engine.BranchRemoteStatuses
	Stats          map[string]engine.BranchStat
	Commits        map[string][]string
	CommitInfo     map[string]git.CommitInfo
	Statuses       engine.BranchStatuses
}

// FetchBranchBatchData runs the batch reads backing BranchBatchData.
func FetchBranchBatchData(ctx context.Context, eng engine.BranchReader, branches engine.Branches) BranchBatchData {
	return BranchBatchData{
		RemoteStatuses: eng.ReadBranchRemoteStatuses(ctx, branches),
		Stats:          eng.BatchBranchStats(branches),
		Commits:        eng.BatchCommits(branches, engine.CommitFormatReadableWithDate),
		CommitInfo:     eng.BatchCommitInfo(branches),
		Statuses:       eng.ReadBranchStatuses(branches),
	}
}

// BranchesFromNames resolves branch names to their graph Branch objects,
// skipping any name not present in the graph.
func BranchesFromNames(graph *engine.StackGraph, names []string) engine.Branches {
	branches := make(engine.Branches, 0, len(names))
	for _, name := range names {
		if node := graph.GetNode(name); node != nil {
			branches = append(branches, node.Branch)
		}
	}
	return branches
}

// MapStackDetail creates a full StackDetail with all branch info. data
// should come from FetchBranchBatchData covering every stack being mapped
// in the current request — see BranchBatchData.
func MapStackDetail(eng engine.BranchReader, graph *engine.StackGraph, rootBranch string, allBranches []string, prCount int, scope string, checksMap github.ChecksByBranch, data BranchBatchData) StackDetail {
	// Derive owner from root branch's PR author
	var owner string
	if checksMap != nil {
		if rootCheck := checksMap[rootBranch]; rootCheck != nil {
			owner = rootCheck.Author
		}
	}

	summary := MapStackSummary(eng, graph, rootBranch, allBranches, prCount, scope, owner, data.Statuses)

	// Check if root is a worktree anchor to filter it from branches
	isAnchor := summary.HasWorktree
	anchorName := rootBranch

	nodes := make([]*engine.StackNode, 0, len(allBranches))
	for _, name := range allBranches {
		if isAnchor && name == anchorName {
			continue
		}
		node := graph.GetNode(name)
		if node == nil {
			continue
		}
		nodes = append(nodes, node)
	}

	branches := make([]BranchResponse, 0, len(nodes))
	for _, node := range nodes {
		name := node.Branch.GetName()
		checks := checksMap.Get(name)
		br := MapBranch(eng, node.Branch, node, checks, data.RemoteStatuses.ForBranch(node.Branch), data.Stats[name], data.Commits[name], data.CommitInfo[name], !data.Statuses.IsUpToDate(node.Branch))
		if isAnchor {
			// Anchor's direct children become display roots
			if br.Parent == anchorName {
				br.Parent = ""
			}
			// All branches shift up one depth level
			br.Depth--
		}
		branches = append(branches, br)
	}

	return StackDetail{
		StackSummary: summary,
		Branches:     branches,
	}
}

func mapPR(prInfo *engine.PrInfo) *PRResponse {
	pr := &PRResponse{
		Title:   prInfo.Title(),
		State:   string(prInfo.State()),
		URL:     prInfo.URL(),
		IsDraft: prInfo.IsDraft(),
		Base:    prInfo.Base(),
	}
	if prInfo.Number() != nil {
		pr.Number = *prInfo.Number()
	}
	return pr
}

func mapCI(checks *github.CheckStatus) *CIResponse {
	ci := &CIResponse{
		ReviewDecision: string(checks.ReviewDecision),
	}

	switch {
	case checks.Passing:
		ci.Status = "passing"
	case checks.Pending:
		ci.Status = "pending"
	case len(checks.Checks) > 0:
		ci.Status = "failing"
	default:
		ci.Status = "none"
	}

	ci.Checks = make([]CheckDetailResponse, len(checks.Checks))
	for i, check := range checks.Checks {
		ci.Checks[i] = CheckDetailResponse{
			Name:       check.Name,
			Status:     string(check.Status),
			Conclusion: string(check.Conclusion),
		}
	}

	return ci
}

func mapCommitsWithDate(lines []string) []CommitResponse {
	commits := make([]CommitResponse, 0, len(lines))
	for _, line := range lines {
		// Tab-separated format: "abc1234\t2024-01-15T10:30:00Z\tCommit message"
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 3 {
			continue
		}
		commits = append(commits, CommitResponse{
			SHA:     parts[0],
			Message: parts[2],
			Date:    parts[1],
		})
	}
	return commits
}

// computeStackStatus determines the overall status of a stack. statuses
// should come from a single ReadBranchStatuses batch call covering
// branchNames, not per-branch NeedsRestack() calls.
func computeStackStatus(graph *engine.StackGraph, branchNames []string, statuses engine.BranchStatuses) string {
	allHavePR := true
	anyNeedsRestack := false
	anyLocked := false

	for _, name := range branchNames {
		node := graph.GetNode(name)
		if node == nil {
			continue
		}
		branch := node.Branch

		if !statuses.IsUpToDate(branch) {
			anyNeedsRestack = true
		}
		if branch.IsLocked() {
			anyLocked = true
		}

		prInfo, err := branch.GetPrInfo()
		if err != nil || prInfo == nil || prInfo.Number() == nil {
			allHavePR = false
		}
	}

	switch {
	case anyLocked:
		return "blocked"
	case anyNeedsRestack:
		return "pending"
	case !allHavePR:
		return "incomplete"
	default:
		return "shippable"
	}
}

// MapTrunkCommits converts git RecentCommit values to API TrunkCommitResponse values.
// Commits whose PR number is already represented by a stack-merge's StackPRs are
// filtered out so that consolidated stacks don't show duplicate entries.
// prTitles is an optional map of PR number to title; pass nil if unavailable.
func MapTrunkCommits(commits []git.RecentCommit, prTitles map[int]string) []TrunkCommitResponse {
	// Drop constituent-PR commits already represented by a stack-merge. The
	// collapse logic is shared with the `stackit log` command via internal/git.
	collapsed := git.RecentCommits(commits).Collapse()

	result := make([]TrunkCommitResponse, 0, len(collapsed))
	for _, c := range collapsed {
		// Message substitution and constituent-title selection are shared with
		// the `stackit log` command via internal/git.
		resp := TrunkCommitResponse{
			SHA:           utils.ShortRevision(c.SHA, 0),
			Message:       c.DisplayMessage(prTitles),
			Author:        c.Author,
			Date:          c.Date.Format(time.RFC3339),
			PRNumber:      c.PRNumber,
			Kind:          string(c.Kind),
			StackSize:     c.StackSize,
			StackPRs:      append([]int(nil), c.StackPRNumbers...),
			StackScope:    c.StackScope,
			StackPRTitles: c.ConstituentPRTitles(prTitles),
		}

		if resp.Kind == "" {
			resp.Kind = TrunkCommitKindRegular
			if c.StackSize > 0 {
				resp.Kind = TrunkCommitKindStackMerge
			}
		}

		result = append(result, resp)
	}
	return result
}
