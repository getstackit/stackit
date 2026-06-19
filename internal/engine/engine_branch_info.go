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
func (e *engineImpl) GetRevisions(branchNames []string) (map[string]string, []error) {
	return e.git.BatchGetRevisions(branchNames)
}

// BatchDivergencePoints returns each branch's divergence point keyed by branch
// name, matching GetDivergencePoint (the stored parent revision when present,
// else the parent's current tip) but resolving the whole set in one batched pass.
func (e *engineImpl) BatchDivergencePoints(branches Branches) map[string]string {
	return batchByBranch(e, branches, func(b Branch, head, parentRev, storedBase string) string {
		return statBase(parentRev, storedBase)
	})
}

// GetCommitCount returns the number of commits for a branch.
// Results are cached by (base, head) SHA pair.
func (e *engineImpl) GetCommitCount(branch Branch) (int, error) {
	base, branchRev, err := e.resolveBranchComparisonRevisions(branch.GetName())
	if err != nil {
		return 0, err
	}
	return e.commitCountBetween(base, branchRev)
}

// commitCountBetween returns the commit count in (base, head], using the
// (base, head)-keyed cache. It takes pre-resolved revisions so batched callers
// need not re-resolve a branch's head.
func (e *engineImpl) commitCountBetween(base, head string) (int, error) {
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

// GetDiffStats returns diff stats for a branch.
// Results are cached by (base, head) SHA pair.
func (e *engineImpl) GetDiffStats(branch Branch) (int, int, error) {
	base, branchRev, err := e.resolveBranchComparisonRevisions(branch.GetName())
	if err != nil {
		return 0, 0, err
	}
	return e.diffStatsBetween(base, branchRev)
}

// diffStatsBetween returns the additions/deletions between two revisions, using
// the (base, head)-keyed cache. It takes pre-resolved revisions so batched
// callers (the Batch* readers) need not re-resolve a branch's head.
func (e *engineImpl) diffStatsBetween(base, head string) (int, int, error) {
	if head == base {
		return 0, 0, nil
	}

	cacheKey := base + ":" + head
	if v, ok := e.diffStatsCache.Load(cacheKey); ok {
		stats := v.([2]int)
		return stats[0], stats[1], nil
	}

	output, err := e.git.GetDiffNumstat(base, head)
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

func (e *engineImpl) resolveBranchComparisonRevisions(branchName string) (base, branchRev string, err error) {
	e.mu.RLock()
	trunk := e.trunk
	state := e.readState(branchName)
	e.mu.RUnlock()

	parent := trunk
	if state != nil {
		parent = state.Parent
	}

	// Get base revision (stored parent revision)
	meta, err := e.readMetadata(branchName)
	if rev := meta.GetParentBranchRevision(); err == nil && rev != nil {
		base = *rev
	} else {
		baseRev, err := e.git.GetRevision(parent)
		if err != nil {
			return "", "", err
		}
		base = baseRev
	}

	branchRev, err = e.git.GetRevision(branchName)
	if err != nil {
		return "", "", err
	}

	return base, branchRev, nil
}

// GetRecentTrunkCommits returns the most recent commits on the trunk branch,
// including any stack trailer metadata embedded in consolidation merge commits.
func (e *engineImpl) GetRecentTrunkCommits(count int) ([]git.RecentCommit, error) {
	return e.git.GetRecentCommits(context.Background(), e.trunk, count)
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

	// Get parent revision (base)
	var baseRevision string
	if rev := meta.GetParentBranchRevision(); rev != nil {
		baseRevision = *rev
	}

	return e.commitsBetween(baseRevision, branchRevision, format)
}

// commitsBetween returns the formatted commits in (base, head]. It handles
// formatting in-process via go-git, avoiding per-commit git process spawns, and
// takes pre-resolved revisions so batched callers need not re-resolve the head.
func (e *engineImpl) commitsBetween(base, head string, format CommitFormat) ([]string, error) {
	return e.git.GetCommitRange(context.Background(), base, head, string(format))
}
