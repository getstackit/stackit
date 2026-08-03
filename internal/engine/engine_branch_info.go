package engine

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/getstackit/stackit/internal/git"
)

// GetCommitDate returns the commit date for a branch
func (e *engineImpl) GetCommitDate(branch Branch) (time.Time, error) {
	branchName := branch.GetName()
	return e.git.GetCommitDate(branchName)
}

// GetCommitAuthor returns the commit author for a branch
func (e *engineImpl) GetCommitAuthor(branch Branch) (string, error) {
	branchName := branch.GetName()
	return e.git.GetCommitAuthor(branchName)
}

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

// diffStatsBetween returns the additions/deletions between two revisions, using
// the (base, head)-keyed cache. It takes pre-resolved revisions so batched
// callers (the Batch* readers) need not re-resolve a branch's head.
func (e *engineImpl) diffStatsBetween(rr git.RevRange) (int, int, error) {
	base, head := rr.Base, rr.Head
	if head == base {
		return 0, 0, nil
	}

	cacheKey := base + ":" + head
	if v, ok := e.diffStatsCache.Load(cacheKey); ok {
		stats := v.([2]int)
		return stats[0], stats[1], nil
	}

	output, err := e.git.GetDiffNumstat(rr)
	if err != nil {
		return 0, 0, err
	}

	added, deleted := 0, 0
	for line := range strings.SplitSeq(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			a, _ := strconv.Atoi(fields[0])
			d, _ := strconv.Atoi(fields[1])
			added += a
			deleted += d
		}
	}

	e.diffStatsCache.Store(cacheKey, [2]int{added, deleted})
	return added, deleted, nil
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

// GetAllCommits returns commits for a branch in various formats
func (e *engineImpl) GetAllCommits(branch Branch, format CommitFormat) ([]string, error) {
	branchName := branch.GetName()
	e.mu.RLock()
	defer e.mu.RUnlock()

	// Check if branch is trunk
	if branchName == e.trunk {
		// Trunk is the base, so it has no commits "on" it relative to a parent
		return []string{}, nil
	}

	// Get metadata to find parent revision
	meta, err := e.readMetadata(branchName)
	if err != nil {
		return nil, err
	}

	// Get branch revision
	branchRevision, err := e.git.GetRevision(branchName)
	if err != nil {
		return nil, err
	}

	// Base for the commit range: the stored divergence point, or the parent's
	// current tip when none is recorded. Falling back to the parent tip — not an
	// empty base, which lists the branch's entire history back to the repo root —
	// keeps the result to the branch's own commits and consistent with the base
	// the batched diff-stat / commit-count readers use (statBase).
	var baseRevision string
	if rev := meta.GetParentBranchRevision(); rev != nil && *rev != "" {
		baseRevision = *rev
	} else {
		parent := e.trunk
		if state := e.readState(branchName); state != nil {
			parent = state.Parent
		}
		if parentRev, err := e.git.GetRevision(parent); err == nil {
			baseRevision = parentRev
		}
	}

	return e.commitsBetween(git.RevRange{Base: baseRevision, Head: branchRevision}, format)
}

// commitsBetween returns the formatted commits in (base, head]. It handles
// formatting in-process via go-git, avoiding per-commit git process spawns, and
// takes pre-resolved revisions so batched callers need not re-resolve the head.
func (e *engineImpl) commitsBetween(rr git.RevRange, format CommitFormat) ([]string, error) {
	return e.git.GetCommitRange(context.Background(), rr.Base, rr.Head, string(format))
}
