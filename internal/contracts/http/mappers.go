package httpcontract

import (
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/getstackit/stackit/internal/actions/stackview"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/github"
	"github.com/getstackit/stackit/internal/utils"
)

// MapperReader is the engine surface the mappers read beyond their batched
// inputs.
type MapperReader interface {
	CurrentBranchName() string
	GetScope(branch engine.Branch) engine.Scope
	GetStackDescription(branch engine.Branch) *git.StackDescription
}

// BranchInput is everything MapBranch needs about one branch. RemoteStatus,
// Stat, Commits, CommitInfo, and NeedsRestack should each come from a single
// batch call covering all branches being mapped in the current request
// (ReadBranchRemoteStatuses, BatchBranchStats, BatchCommits,
// BatchCommitInfo, ReadBranchStatuses), not per-branch calls — per-branch
// reads spawn a git process per field per branch.
type BranchInput struct {
	Branch       engine.Branch
	Node         *engine.StackNode
	Checks       *github.CheckStatus
	RemoteStatus engine.BranchRemoteStatus
	Stat         engine.BranchStat
	Commits      git.Commits
	CommitInfo   git.CommitInfo
	NeedsRestack bool
}

// NewBranchInput assembles a BranchInput from the batched data resolved by
// stackview.FetchBranchData.
func NewBranchInput(node *engine.StackNode, checks *github.CheckStatus, data stackview.BranchData) BranchInput {
	branch := node.Branch
	name := branch.GetName()
	return BranchInput{
		Branch:       branch,
		Node:         node,
		Checks:       checks,
		RemoteStatus: data.RemoteStatuses.ForBranch(branch),
		Stat:         data.Stats[name],
		Commits:      data.Commits[name],
		CommitInfo:   data.CommitInfo[name],
		NeedsRestack: !data.Statuses.IsUpToDate(branch),
	}
}

// MapBranch converts an engine Branch and its StackNode into an API BranchResponse.
func MapBranch(eng MapperReader, in BranchInput) BranchResponse {
	branch, node, checks := in.Branch, in.Node, in.Checks
	remoteStatus, stat, commits, commitInfo, needsRestack := in.RemoteStatus, in.Stat, in.Commits, in.CommitInfo, in.NeedsRestack
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
	resp.RemoteStatus = &RemoteStatus{
		Ahead:         remoteStatus.Ahead(),
		Behind:        remoteStatus.Behind(),
		Diverged:      remoteStatus.Diverged(),
		MissingRemote: remoteStatus.MissingRemote(),
	}

	return resp
}

// StackInput identifies one discovered stack to map.
type StackInput struct {
	// RootBranch is the stack's root (a direct child of trunk).
	RootBranch string
	// AllBranches is every branch in the stack, root to tip.
	AllBranches []string
	// PRCount is the number of PRs in the stack.
	PRCount int
	// Scope is the stack's scope, if any.
	Scope engine.Scope
}

// MapStackSummary creates a StackSummary from stack discovery info. owner is
// the stack root's PR author, or "" when unknown. statuses should come from a
// single ReadBranchStatuses batch call covering every stack being summarized
// in the current request, not one call per stack — see stackview.BranchData.
func MapStackSummary(eng MapperReader, graph *engine.StackGraph, stack StackInput, owner string, statuses engine.BranchStatuses) StackSummary {
	rootBranch, allBranches, prCount := stack.RootBranch, stack.AllBranches, stack.PRCount
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

	status := string(stackview.LocalStatus(graph, displayBranches, statuses))

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
		Scope:       stack.Scope.String(),
		BranchCount: len(displayBranches),
		PRCount:     prCount,
		IsCurrent:   isCurrent,
		HasWorktree: hasWorktree,
		Description: description,
		Owner:       owner,
	}
}

// MapStackDetail creates a full StackDetail with all branch info. data
// should come from stackview.FetchBranchData covering every stack being
// mapped in the current request — see stackview.BranchData.
func MapStackDetail(eng MapperReader, graph *engine.StackGraph, stack StackInput, checksMap github.ChecksByBranch, data stackview.BranchData) StackDetail {
	rootBranch, allBranches := stack.RootBranch, stack.AllBranches
	// Derive owner from root branch's PR author
	var owner string
	if checksMap != nil {
		if rootCheck := checksMap[rootBranch]; rootCheck != nil {
			owner = rootCheck.Author
		}
	}

	summary := MapStackSummary(eng, graph, stack, owner, data.Statuses)

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
		br := MapBranch(eng, NewBranchInput(node, checksMap.Get(node.Branch.GetName()), data))
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
		pr.Number = int(*prInfo.Number())
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

func mapCommits(records git.Commits) []CommitResponse {
	commits := make([]CommitResponse, 0, len(records))
	for _, commit := range records {
		date, err := time.Parse(time.RFC3339, commit.AuthorDate)
		if err != nil {
			continue
		}
		commits = append(commits, CommitResponse{
			SHA: commit.ShortSHA, Message: strings.TrimRightFunc(commit.Subject, unicode.IsSpace), Date: date.UTC().Format(time.RFC3339),
		})
	}
	return commits
}

// MapTrunkCommits converts git RecentCommit values to API TrunkCommitResponse values.
// Commits whose PR number is already represented by a stack-merge's StackPRs are
// filtered out so that consolidated stacks don't show duplicate entries.
// prTitles is an optional map of PR number to title; pass nil if unavailable.
func MapTrunkCommits(commits []git.RecentCommit, prTitles map[git.PRNumber]string) []TrunkCommitResponse {
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
			PRNumber:      int(c.PRNumber),
			Kind:          string(c.Kind),
			StackSize:     c.StackSize,
			StackPRs:      prNumbersToInts(c.StackPRNumbers),
			StackScope:    c.StackScope,
			StackPRTitles: prTitlesToInts(c.ConstituentPRTitles(prTitles)),
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

// prNumbersToInts converts typed PR numbers to the plain ints the API contract
// carries, returning nil for an empty list (as append onto a nil slice did).
func prNumbersToInts(numbers []git.PRNumber) []int {
	if len(numbers) == 0 {
		return nil
	}
	ints := make([]int, len(numbers))
	for i, n := range numbers {
		ints[i] = int(n)
	}
	return ints
}

// prTitlesToInts re-keys a PR-number → title map by plain int for the API
// contract, preserving nil.
func prTitlesToInts(titles map[git.PRNumber]string) map[int]string {
	if titles == nil {
		return nil
	}
	out := make(map[int]string, len(titles))
	for n, title := range titles {
		out[int(n)] = title
	}
	return out
}
