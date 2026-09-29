package stackview

import (
	"context"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
)

// BranchData holds per-branch data read via batch calls, keyed by branch
// name. Resolve it once via FetchBranchData over the union of branches in
// every stack being shown, not once per stack — RemoteStatuses in particular
// backs a network round trip (`git ls-remote`) whose cost does not depend on
// how many branches it covers.
type BranchData struct {
	RemoteStatuses engine.BranchRemoteStatuses
	Stats          map[string]engine.BranchStat
	Commits        map[string]git.Commits
	CommitInfo     map[string]git.CommitInfo
	Statuses       engine.BranchStatuses
}

// BranchDataReader is the engine surface FetchBranchData reads.
type BranchDataReader interface {
	ReadBranchRemoteStatuses(ctx context.Context, branches engine.Branches) engine.BranchRemoteStatuses
	BatchBranchStats(branches engine.Branches) map[string]engine.BranchStat
	BatchCommits(branches engine.Branches) map[string]git.Commits
	BatchCommitInfo(branches engine.Branches) map[string]git.CommitInfo
	ReadBranchStatuses(branches engine.Branches) engine.BranchStatuses
}

// FetchBranchData runs the batch reads backing BranchData: one remote
// listing, one stats pass, one commits pass, one commit-info pass, and one
// restack-status pass for all branches.
func FetchBranchData(ctx context.Context, eng BranchDataReader, branches engine.Branches) BranchData {
	return BranchData{
		RemoteStatuses: eng.ReadBranchRemoteStatuses(ctx, branches),
		Stats:          eng.BatchBranchStats(branches),
		Commits:        eng.BatchCommits(branches),
		CommitInfo:     eng.BatchCommitInfo(branches),
		Statuses:       eng.ReadBranchStatuses(branches),
	}
}

// BranchesFromGraph resolves branch names to their graph Branch objects,
// skipping any name not present in the graph.
func BranchesFromGraph(graph *engine.StackGraph, names []string) engine.Branches {
	branches := make(engine.Branches, 0, len(names))
	for _, name := range names {
		if node := graph.GetNode(name); node != nil {
			branches = append(branches, node.Branch)
		}
	}
	return branches
}

// LocalStackStatus is a stack's status judged from local branch state only.
type LocalStackStatus string

// LocalStackStatus values. The strings match shippable.Status on purpose so
// the web UI can style both, but the rules differ — see LocalStatus.
const (
	LocalStackShippable  LocalStackStatus = "shippable"
	LocalStackPending    LocalStackStatus = "pending"
	LocalStackBlocked    LocalStackStatus = "blocked"
	LocalStackIncomplete LocalStackStatus = "incomplete"
)

// LocalStatus classifies a stack from local metadata alone: locked →
// blocked, needs restack → pending, missing PR → incomplete, else shippable.
// statuses should come from one ReadBranchStatuses batch covering
// branchNames.
//
// This is deliberately not shippable.determineStatus, which classifies from
// forge state (CI checks, review decision, draft, pushed-vs-remote) and is
// what `merge`/the dashboard use to decide what can actually ship. This one
// needs no network and backs the API's stack summaries; the two can disagree
// (e.g. a locked stack with green CI, or a failing-CI stack that is up to
// date locally).
func LocalStatus(graph *engine.StackGraph, branchNames []string, statuses engine.BranchStatuses) LocalStackStatus {
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
		return LocalStackBlocked
	case anyNeedsRestack:
		return LocalStackPending
	case !allHavePR:
		return LocalStackIncomplete
	default:
		return LocalStackShippable
	}
}
