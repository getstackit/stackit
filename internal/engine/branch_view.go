package engine

import (
	"context"

	"github.com/getstackit/stackit/internal/utils"
)

// DiffStat is a branch's additions/deletions relative to its divergence point.
type DiffStat struct {
	Added   int
	Deleted int
}

// BranchStat holds the git-computed fields a branch annotation needs (short SHA,
// commit count, additions/deletions), resolved in batch so annotation builders
// do no per-branch git.
type BranchStat struct {
	ShortSHA     string
	CommitCount  int
	LinesAdded   int
	LinesDeleted int
}

// batchByBranch is the shared scaffold behind the per-concern batch readers. It
// resolves every branch's head revision, parent revision, and stored divergence
// base in two batched, cache-backed reads, then runs fn for each branch on a
// bounded worker pool and collects the results by branch name. Callers supply
// only the per-concern computation; the (otherwise duplicated) batched
// resolution lives here once. storedBase is the metadata ParentBranchRevision
// ("" when unset); each concern decides how to derive its base from
// storedBase/parentRev.
//
// Concurrency is bounded by utils.Run (GOMAXPROCS workers) rather than spawning
// one goroutine per branch, so a large stack does not fan out to hundreds of
// concurrent git subprocesses on a cold cache.
func batchByBranch[T any](e *engineImpl, branches Branches, fn func(b Branch, head, parentRev, storedBase string) T) map[string]T {
	result := make(map[string]T, len(branches))
	if len(branches) == 0 {
		return result
	}

	branchNames := make([]string, 0, len(branches))
	revNames := make([]string, 0, len(branches)*2)
	for _, b := range branches {
		branchNames = append(branchNames, b.GetName())
		revNames = append(revNames, b.GetName(), b.GetParentOrTrunk())
	}
	revs, _ := e.GetRevisions(revNames)
	metas, _ := e.batchReadMetadata(branchNames)

	// Each worker writes only its own index, so the slice is filled without
	// synchronization and assembled into the result map serially afterward.
	type indexedBranch struct {
		index  int
		branch Branch
	}
	indexed := make([]indexedBranch, len(branches))
	values := make([]T, len(branches))
	for i, b := range branches {
		indexed[i] = indexedBranch{index: i, branch: b}
	}

	utils.Run(indexed, func(item indexedBranch) {
		name := item.branch.GetName()
		storedBase := ""
		if m := metas[name]; m != nil {
			if rev := m.GetParentBranchRevision(); rev != nil && *rev != "" {
				storedBase = *rev
			}
		}
		values[item.index] = fn(item.branch, revs[name], revs[item.branch.GetParentOrTrunk()], storedBase)
	})

	for i, b := range branches {
		result[b.GetName()] = values[i]
	}
	return result
}

// statBase returns the comparison base used by diff stats and commit counts:
// the stored divergence point, or the parent's current tip when none is stored.
func statBase(parentRev, storedBase string) string {
	if storedBase != "" {
		return storedBase
	}
	return parentRev
}

// BatchDiffStats returns each non-trunk branch's additions/deletions against its
// divergence point, keyed by branch name, resolved in one batched pass.
func (e *engineImpl) BatchDiffStats(branches Branches) map[string]DiffStat {
	ranges := e.branchStatRanges(branches)
	stats := e.readDiffStats(context.Background(), ranges)
	result := make(map[string]DiffStat, len(ranges))
	for name, rr := range ranges {
		result[name] = stats[rr].DiffStat
	}
	return result
}

// BatchChangedFileCounts returns each non-trunk branch's number of files changed
// in its own range — measured against its divergence point, the same base
// BatchDiffStats uses — keyed by branch name, resolved in one batched pass. This
// keeps the file count consistent with the additions/deletions for a branch
// whose parent has advanced since it diverged.
func (e *engineImpl) BatchChangedFileCounts(ctx context.Context, branches Branches) map[string]int {
	ranges := e.branchStatRanges(branches)
	stats := e.readDiffStats(ctx, ranges)
	result := make(map[string]int, len(ranges))
	for name, rr := range ranges {
		result[name] = stats[rr].Files
	}
	return result
}

// BatchBranchStats resolves the annotation stats (short SHA, commit count,
// additions/deletions) for every branch in one batched pass, keyed by branch
// name. Forge status (CI, reviews) is a separate concern joined at render time,
// not part of this.
func (e *engineImpl) BatchBranchStats(branches Branches) map[string]BranchStat {
	ranges := e.branchStatRanges(branches)
	diffs := e.readDiffStats(context.Background(), ranges)
	result := make(map[string]BranchStat, len(branches))
	values := make(map[string]*BranchStat, len(branches))
	for name, rr := range ranges {
		values[name] = &BranchStat{
			ShortSHA:   utils.ShortRevision(rr.Head, 0),
			LinesAdded: diffs[rr].Added, LinesDeleted: diffs[rr].Deleted,
		}
	}
	utils.Run(branches, func(b Branch) {
		name := b.GetName()
		values[name].CommitCount, _ = e.commitCountBetween(ranges[name])
	})
	for name, stat := range values {
		result[name] = *stat
	}
	return result
}
