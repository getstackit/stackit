package httpcontract

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/github"
)

// MapBranch converts an engine Branch and its StackNode into an API BranchResponse.
func MapBranch(eng engine.BranchReader, branch engine.Branch, node *engine.StackNode, checks *github.CheckStatus) BranchResponse {
	resp := BranchResponse{
		Name:         branch.GetName(),
		Depth:        node.Depth,
		IsCurrent:    branch.GetName() == eng.CurrentBranch().GetName(),
		NeedsRestack: branch.NeedsRestack(),
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

	if rev, err := branch.GetRevision(); err == nil {
		resp.Revision = shortSHA(rev)
	}

	if date, err := branch.GetCommitDate(); err == nil {
		resp.CommitDate = date.Format(time.RFC3339)
	}

	if author, err := eng.GetCommitAuthor(branch); err == nil {
		resp.CommitAuthor = author
	}

	if count, err := branch.GetCommitCount(); err == nil {
		resp.CommitCount = count
	}

	if added, deleted, err := branch.GetDiffStats(); err == nil {
		resp.LinesAdded = added
		resp.LinesDeleted = deleted
	}

	// Map commits
	if commits, err := eng.GetAllCommits(branch, engine.CommitFormatReadableWithDate); err == nil {
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
	remoteStatus := eng.ReadBranchRemoteStatuses(context.Background(), engine.BranchesOf(branch)).ForBranch(branch)
	resp.RemoteStatus = &RemoteStatus{
		Ahead:         remoteStatus.Ahead(),
		Behind:        remoteStatus.Behind(),
		Diverged:      remoteStatus.Diverged(),
		MissingRemote: remoteStatus.MissingRemote(),
	}

	return resp
}

// MapStackSummary creates a StackSummary from stack discovery info.
func MapStackSummary(eng engine.BranchReader, graph *engine.StackGraph, rootBranch string, allBranches []string, prCount int, scope string, owner string) StackSummary {
	currentBranch := eng.CurrentBranch().GetName()
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

	// Compute stack status using display branches (anchor excluded)
	status := computeStackStatus(graph, displayBranches)

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

// MapStackDetail creates a full StackDetail with all branch info.
func MapStackDetail(eng engine.BranchReader, graph *engine.StackGraph, rootBranch string, allBranches []string, prCount int, scope string, checksMap map[string]*github.CheckStatus) StackDetail {
	// Derive owner from root branch's PR author
	var owner string
	if checksMap != nil {
		if rootCheck := checksMap[rootBranch]; rootCheck != nil {
			owner = rootCheck.Author
		}
	}

	summary := MapStackSummary(eng, graph, rootBranch, allBranches, prCount, scope, owner)

	// Check if root is a worktree anchor to filter it from branches
	isAnchor := summary.HasWorktree
	anchorName := rootBranch

	branches := make([]BranchResponse, 0, len(allBranches))
	for _, name := range allBranches {
		if isAnchor && name == anchorName {
			continue
		}
		node := graph.GetNode(name)
		if node == nil {
			continue
		}
		var checks *github.CheckStatus
		if checksMap != nil {
			checks = checksMap[name]
		}
		br := MapBranch(eng, node.Branch, node, checks)
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
		State:   prInfo.State(),
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
		ReviewDecision: checks.ReviewDecision,
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
			Status:     check.Status,
			Conclusion: check.Conclusion,
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

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// computeStackStatus determines the overall status of a stack.
func computeStackStatus(graph *engine.StackGraph, branchNames []string) string {
	allHavePR := true
	anyNeedsRestack := false
	anyLocked := false

	for _, name := range branchNames {
		node := graph.GetNode(name)
		if node == nil {
			continue
		}
		branch := node.Branch

		if branch.NeedsRestack() {
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
	// Collect all PR numbers that are covered by stack-merge consolidation commits.
	coveredPRs := make(map[int]struct{})
	for _, c := range commits {
		if c.StackSize > 0 {
			for _, pr := range c.StackPRNumbers {
				coveredPRs[pr] = struct{}{}
			}
		}
	}

	result := make([]TrunkCommitResponse, 0, len(commits))
	for _, c := range commits {
		// Skip commits whose PR is already represented by a stack-merge.
		if c.PRNumber != 0 && c.StackSize == 0 {
			if _, covered := coveredPRs[c.PRNumber]; covered {
				continue
			}
		}

		message := c.Subject
		// For stack-merge commits, use the consolidation PR's title from GitHub
		// instead of the raw "Merge pull request #N from ..." subject
		if c.StackSize > 0 && c.PRNumber != 0 && len(prTitles) > 0 {
			if title, ok := prTitles[c.PRNumber]; ok {
				message = title
			}
		}

		resp := TrunkCommitResponse{
			SHA:        shortSHA(c.SHA),
			Message:    message,
			Author:     c.Author,
			Date:       c.Date.Format(time.RFC3339),
			PRNumber:   c.PRNumber,
			Kind:       string(c.Kind),
			StackSize:  c.StackSize,
			StackPRs:   append([]int(nil), c.StackPRNumbers...),
			StackScope: c.StackScope,
		}

		// Populate PR titles for stack-merge commits when available
		if c.StackSize > 0 && len(prTitles) > 0 {
			titles := make(map[int]string)
			for _, pr := range c.StackPRNumbers {
				if title, ok := prTitles[pr]; ok {
					titles[pr] = title
				}
			}
			if len(titles) > 0 {
				resp.StackPRTitles = titles
			}
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
