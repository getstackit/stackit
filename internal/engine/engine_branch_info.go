package engine

import (
	"context"
	"strings"
	"time"

	"github.com/getstackit/stackit/internal/git"
)

// GetCommitDate returns the commit date for a branch
func (e *engineImpl) GetCommitDate(branch Branch) (time.Time, error) {
	branchName := branch.GetName()
	info, err := e.git.ReadCommitInfo(context.Background(), "refs/heads/"+branchName).One()
	return info.Date, err
}

// GetCommitAuthor returns the commit author for a branch
func (e *engineImpl) GetCommitAuthor(branch Branch) (string, error) {
	branchName := branch.GetName()
	info, err := e.git.ReadCommitInfo(context.Background(), "refs/heads/"+branchName).One()
	return info.Author, err
}

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

// GetRevisionForName returns the SHA of a branch by name
func (e *engineImpl) GetRevisionForName(branchName string) (string, error) {
	return e.git.ReadRevisions(context.Background(), branchName).One()
}

// GetRevisions returns the SHAs for multiple branches.
func (e *engineImpl) GetRevisions(branchNames []string) (RevisionMap, []error) {
	return e.git.ReadRevisions(context.Background(), branchNames...).ValuesAndErrors()
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

// CommitCountBetween returns how many commits are in (base, head]. It is the
// exported entry point to the cached counter for callers that have two plain
// revisions rather than a branch set — e.g. reporting how far trunk moved
// during a sync.
func (e *engineImpl) CommitCountBetween(base, head string) (int, error) {
	return e.commitCountBetween(git.RevRange{Base: base, Head: head})
}

// commitCountBetween returns the commit count in (base, head], using the
// (base, head)-keyed cache. It takes pre-resolved revisions so batched callers
// need not re-resolve a branch's head.
func (e *engineImpl) commitCountBetween(rr git.RevRange) (int, error) {
	if rr.Head == rr.Base {
		return 0, nil
	}

	if v, ok := e.commitCountCache.Load(rr); ok {
		return v.(int), nil
	}

	count, err := e.git.ReadCommitCounts(context.Background(), rr).One()
	if err != nil {
		return 0, err
	}
	e.commitCountCache.Store(rr, count)
	return count, nil
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
