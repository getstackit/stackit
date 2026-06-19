package engine

import (
	"context"
	"strconv"
	"strings"
	"sync"
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
// else the parent's current tip) but resolving the whole set with one batched
// metadata read and one batched revision lookup instead of per-branch git.
func (e *engineImpl) BatchDivergencePoints(branches Branches) map[string]string {
	result := make(map[string]string, len(branches))
	if len(branches) == 0 {
		return result
	}

	names := make([]string, 0, len(branches))
	parentNames := make([]string, 0, len(branches))
	for _, b := range branches {
		names = append(names, b.GetName())
		parentNames = append(parentNames, b.GetParentOrTrunk())
	}
	metas, _ := e.batchReadMetadata(names)
	parentRevs, _ := e.GetRevisions(parentNames)

	for _, b := range branches {
		name := b.GetName()
		if m := metas[name]; m != nil {
			if rev := m.GetParentBranchRevision(); rev != nil && *rev != "" {
				result[name] = *rev
				continue
			}
		}
		result[name] = parentRevs[b.GetParentOrTrunk()]
	}
	return result
}

// GetCommitCount returns the number of commits for a branch.
// Results are cached by (base, head) SHA pair and populated by PreloadBranchStats.
func (e *engineImpl) GetCommitCount(branch Branch) (int, error) {
	base, branchRev, err := e.resolveBranchComparisonRevisions(branch.GetName())
	if err != nil {
		return 0, err
	}
	if branchRev == base {
		return 0, nil
	}

	cacheKey := base + ":" + branchRev
	if v, ok := e.commitCountCache.Load(cacheKey); ok {
		return v.(int), nil
	}

	out, err := e.git.RunGitCommandWithContext(context.Background(), "rev-list", "--count", base+".."+branchRev)
	if err != nil {
		return 0, err
	}
	count, _ := strconv.Atoi(strings.TrimSpace(out))
	e.commitCountCache.Store(cacheKey, count)
	return count, nil
}

// GetDiffStats returns diff stats for a branch.
// Results are cached by (base, head) SHA pair and populated by PreloadBranchStats.
func (e *engineImpl) GetDiffStats(branch Branch) (int, int, error) {
	base, branchRev, err := e.resolveBranchComparisonRevisions(branch.GetName())
	if err != nil {
		return 0, 0, err
	}
	return e.diffStatsBetween(base, branchRev)
}

// diffStatsBetween returns the additions/deletions between two revisions, using
// the (base, head)-keyed cache. It takes pre-resolved revisions so batched
// callers (e.g. ViewBranches) need not re-resolve a branch's head.
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

// PreloadBranchStats warms the diff-stats and commit-count caches for all given
// branches in parallel. Call this before iterating branches in utils.Run so
// subsequent GetDiffStats / GetCommitCount calls are instant cache hits.
func (e *engineImpl) PreloadBranchStats(branches []Branch) {
	var wg sync.WaitGroup
	for _, b := range branches {
		if b.IsTrunk() || b.IsWorktreeAnchor() {
			continue
		}
		wg.Add(1)
		go func(branch Branch) {
			defer wg.Done()
			_, _, _ = e.GetDiffStats(branch)
			_, _ = e.GetCommitCount(branch)
		}(b)
	}
	wg.Wait()
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

// PreloadBranchData batch-loads metadata and revisions for all tracked branches
// into their respective caches. This replaces N individual lookups (each requiring
// mutex acquisition or subprocess spawning) with two bulk operations:
//   - one batched branch-ref load for all branch SHAs
//   - BatchReadMetadata (parallel metadata reads, cached via sync.Map)
//
// After calling this, parallel annotation building via utils.Run will find all
// data cached, eliminating go-git mutex contention for revision lookups.
func (e *engineImpl) PreloadBranchData() {
	e.mu.RLock()
	branches := make([]string, 0, len(e.state.branchState))
	for name := range e.state.branchState {
		branches = append(branches, name)
	}
	e.mu.RUnlock()

	// Batch load all branch revisions in one pass
	_ = e.git.LoadAllBranchRevisions()

	// Batch load all metadata (populates metadataCache via sync.Map)
	if len(branches) > 0 {
		e.batchReadMetadata(branches)
	}
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
