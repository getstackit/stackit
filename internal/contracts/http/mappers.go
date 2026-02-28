package httpcontract

import (
	"slices"
	"time"

	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/github"
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
	if commits, err := eng.GetAllCommits(branch, engine.CommitFormatReadable); err == nil {
		resp.Commits = mapCommits(commits)
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
	if remoteStatus, err := eng.GetBranchRemoteStatus(branch); err == nil {
		resp.RemoteStatus = &RemoteStatus{
			Ahead:         remoteStatus.Ahead(),
			Behind:        remoteStatus.Behind(),
			Diverged:      remoteStatus.Diverged(),
			MissingRemote: remoteStatus.MissingRemote(),
		}
	}

	return resp
}

// MapStackSummary creates a StackSummary from stack discovery info.
func MapStackSummary(eng engine.BranchReader, graph *engine.StackGraph, rootBranch string, allBranches []string, prCount int, scope string, owner string) StackSummary {
	currentBranch := eng.CurrentBranch().GetName()
	isCurrent := slices.Contains(allBranches, currentBranch)

	// Get title from root branch's PR or commit subject
	title := rootBranch
	rootNode := graph.GetNode(rootBranch)
	if rootNode != nil {
		if prInfo, err := rootNode.Branch.GetPrInfo(); err == nil && prInfo != nil && prInfo.Title() != "" {
			title = prInfo.Title()
		}
	}

	// Compute stack status
	status := computeStackStatus(graph, allBranches)

	var description string
	if rootNode != nil {
		if stackDesc := eng.GetStackDescription(rootNode.Branch); stackDesc != nil && !stackDesc.IsEmpty() {
			description = stackDesc.Description
		}
	}

	return StackSummary{
		RootBranch:  rootBranch,
		Title:       title,
		Status:      status,
		Scope:       scope,
		BranchCount: len(allBranches),
		PRCount:     prCount,
		IsCurrent:   isCurrent,
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

	branches := make([]BranchResponse, 0, len(allBranches))
	for _, name := range allBranches {
		node := graph.GetNode(name)
		if node == nil {
			continue
		}
		var checks *github.CheckStatus
		if checksMap != nil {
			checks = checksMap[name]
		}
		branches = append(branches, MapBranch(eng, node.Branch, node, checks))
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

func mapCommits(readable []string) []CommitResponse {
	commits := make([]CommitResponse, 0, len(readable))
	for _, line := range readable {
		if len(line) < 8 {
			continue
		}
		// Readable format is "abc1234 Commit message"
		sha := line[:7]
		msg := ""
		if len(line) > 8 {
			msg = line[8:]
		}
		commits = append(commits, CommitResponse{
			SHA:     sha,
			Message: msg,
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
