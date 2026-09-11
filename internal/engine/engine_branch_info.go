package engine

import (
	"context"
	"strings"

	"github.com/getstackit/stackit/internal/git"
)

// BatchCommitInfo returns each branch's tip commit date and author, keyed by
// branch name, resolved in one batched pass.
func (e *engineImpl) BatchCommitInfo(branches Branches) map[string]git.CommitInfo {
	names := make([]string, len(branches))
	for i, b := range branches {
		names[i] = "refs/heads/" + b.GetName()
	}
	data := e.git.ReadCommitInfo(context.Background(), names...)
	result := make(map[string]git.CommitInfo, len(branches))
	for ref, info := range data.All() {
		if info.Err == nil {
			result[strings.TrimPrefix(ref, "refs/heads/")] = info.Value
		}
	}
	return result
}

// GetRevision returns the SHA of a branch
func (e *engineImpl) GetRevision(branch Branch) (string, error) {
	branchName := branch.GetName()
	return e.git.ReadRevisions(context.Background(), branchName).One()
}

// BatchRevisions returns each branch's tip SHA keyed by branch name, resolved
// in one batched pass. Branches whose ref does not resolve are omitted.
func (e *engineImpl) BatchRevisions(branches Branches) RevisionMap {
	return e.readRevisions(branches.Names())
}

// readRevisions resolves the SHAs for a set of ref names in one git call. It
// backs the Branch-typed batch readers; callers outside the engine go through
// BatchRevisions.
func (e *engineImpl) readRevisions(names []string) RevisionMap {
	revs, _ := e.git.ReadRevisions(context.Background(), names...).ValuesAndErrors()
	return revs
}

// BatchDivergencePoints returns each branch's divergence point keyed by branch
// name, matching GetDivergencePoint (the stored parent revision when present,
// else the parent's current tip) but resolving the whole set in one batched pass.
func (e *engineImpl) BatchDivergencePoints(branches Branches) RevisionMap {
	result := make(RevisionMap, len(branches))
	for name, rr := range e.branchDiffRanges(branches) {
		result[name] = rr.Base
	}
	return result
}

// GetRecentTrunkCommits returns the most recent commits on the trunk branch,
// including any stack trailer metadata embedded in consolidation merge commits.
func (e *engineImpl) GetRecentTrunkCommits(count int) ([]git.RecentCommit, error) {
	return e.git.GetRecentCommits(context.Background(), e.trunk, count)
}

// GetTrunkCommitsInRange returns the commits in the revision range from..to with
// stack trailer metadata. An empty `to` defaults to the trunk branch tip. Use it
// to build a changelog over a tag range (e.g. from "v1.4.0" to trunk).
func (e *engineImpl) GetTrunkCommitsInRange(rr git.RevRange) ([]git.RecentCommit, error) {
	if rr.Head == "" {
		rr.Head = e.trunk
	}
	return e.git.GetRecentCommitsInRange(context.Background(), rr.String())
}

// GetAllCommits returns newest-first typed display/replay data for a branch.
func (e *engineImpl) GetAllCommits(branch Branch) (git.Commits, error) {
	history, err := e.ReadBranchCommits(context.Background(), BranchesOf(branch)).One()
	return history.Commits, err
}

// GetCommitIDs returns branch identities without loading display/replay fields.
func (e *engineImpl) GetCommitIDs(branch Branch) ([]string, error) {
	nodes, err := e.ReadBranchCommitNodes(context.Background(), BranchesOf(branch)).One()
	ids := make([]string, 0, len(nodes))
	for _, node := range nodes {
		ids = append(ids, node.SHA)
	}
	return ids, err
}
