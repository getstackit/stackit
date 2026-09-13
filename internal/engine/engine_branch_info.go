package engine

import (
	"context"
	"strconv"
	"strings"

	"github.com/getstackit/stackit/internal/git"
)

// BatchCommitInfo returns each branch's tip commit date and author, keyed by
// branch name, resolved in one batched pass.
func (e *engineImpl) BatchCommitInfo(branches Branches) map[string]git.CommitInfo {
	names := make([]string, len(branches))
	for i, b := range branches {
		names[i] = b.GetName()
	}
	return e.git.BatchCommitInfo(names)
}

// GetRevision returns the SHA of a branch
func (e *engineImpl) GetRevision(branch Branch) (string, error) {
	branchName := branch.GetName()
	return e.git.GetRevision(branchName)
}

// GetRevisionForName returns the SHA of a branch by name
func (e *engineImpl) GetRevisionForName(branchName string) (string, error) {
	return e.git.GetRevision(branchName)
}

// GetRevisions returns the SHAs for multiple branches.
func (e *engineImpl) GetRevisions(branchNames []string) (RevisionMap, []error) {
	return e.git.BatchGetRevisions(branchNames)
}

// BatchDivergencePoints returns each branch's divergence point keyed by branch
// name, matching GetDivergencePoint (the stored parent revision when present,
// else the parent's current tip) but resolving the whole set in one batched pass.
func (e *engineImpl) BatchDivergencePoints(branches Branches) RevisionMap {
	return RevisionMap(batchByBranch(e, branches, func(b Branch, head, parentRev, storedBase string) string {
		return statBase(parentRev, storedBase)
	}))
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
	base, head := rr.Base, rr.Head
	if head == base {
		return 0, nil
	}

	cacheKey := base + ":" + head
	if v, ok := e.commitCountCache.Load(cacheKey); ok {
		return v.(int), nil
	}

	out, err := e.git.RunGitCommandWithContext(context.Background(), "rev-list", "--count", base+".."+head)
	if err != nil {
		return 0, err
	}
	count, _ := strconv.Atoi(strings.TrimSpace(out))
	e.commitCountCache.Store(cacheKey, count)
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

// commitsBetween reads formatted commits using Git with pre-resolved revisions.
func (e *engineImpl) commitsBetween(rr git.RevRange, format CommitFormat) ([]string, error) {
	return e.git.GetCommitRange(context.Background(), rr.Base, rr.Head, string(format))
}
