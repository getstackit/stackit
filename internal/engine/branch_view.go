package engine

import (
	"context"
	"sync"

	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/utils"
)

// DiffStat is a branch's additions/deletions relative to its divergence point.
type DiffStat struct {
	Added        int
	Deleted      int
	FilesChanged int
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
	ranges := e.branchDiffRanges(branches)
	diffs := e.readBranchDiffs(context.Background(), ranges, git.DiffStats)
	result := make(map[string]DiffStat, len(branches))
	for _, branch := range branches {
		diff := diffs[branch.GetName()]
		result[branch.GetName()] = DiffStat{Added: diff.Added, Deleted: diff.Deleted, FilesChanged: len(diff.Files)}
	}
	return result
}

func (e *engineImpl) branchDiffRanges(branches Branches) map[string]git.RevRange {
	return batchByBranch(e, branches, func(b Branch, head, parentRev, storedBase string) git.RevRange {
		if e.IsTrunk(b) {
			return git.RevRange{}
		}
		return git.RevRange{Base: statBase(parentRev, storedBase), Head: head}
	})
}

// Read only uncached immutable ranges; no per-branch subprocesses are launched.
func (e *engineImpl) readBranchDiffs(ctx context.Context, ranges map[string]git.RevRange, mode git.DiffReadMode) map[string]git.DiffSummary {
	result := make(map[string]git.DiffSummary, len(ranges))
	pending := make([]git.RevRange, 0, len(ranges))
	for name, rr := range ranges {
		if rr.Base == "" || rr.Head == "" || rr.Base == rr.Head {
			continue
		}
		if cached, ok := e.diffStatsCache.Load(rr.Base + ":" + rr.Head); ok {
			result[name] = cached.(git.DiffSummary)
			continue
		}
		pending = append(pending, rr)
	}
	diffs := e.git.ReadDiffs(ctx, mode, pending...)
	for name, rr := range ranges {
		if diff, err := diffs.Get(rr.String()); err == nil {
			result[name] = diff
			if mode == git.DiffStats {
				e.diffStatsCache.Store(rr.Base+":"+rr.Head, diff)
			}
		}
	}
	return result
}

// BatchCommits returns each non-trunk branch's formatted commits, keyed by
// branch name, resolved in one batched pass. It matches GetAllCommits: the base
// is the stored divergence point, or the parent's current tip when none is
// recorded — never an empty base, which would list a branch's entire history
// back to the repo root.
func (e *engineImpl) BatchCommits(branches Branches, format CommitFormat) map[string][]string {
	return batchByBranch(e, branches, func(b Branch, head, parentRev, storedBase string) []string {
		if e.IsTrunk(b) {
			return nil
		}
		commits, _ := e.commitsBetween(git.RevRange{Base: statBase(parentRev, storedBase), Head: head}, format)
		return commits
	})
}

// BatchChangedFileCounts returns each non-trunk branch's number of files changed
// in its own range — measured against its divergence point, the same base
// BatchDiffStats uses — keyed by branch name, resolved in one batched pass. This
// keeps the file count consistent with the additions/deletions for a branch
// whose parent has advanced since it diverged.
func (e *engineImpl) BatchChangedFileCounts(ctx context.Context, branches Branches) map[string]int {
	diffs := e.readBranchDiffs(ctx, e.branchDiffRanges(branches), git.DiffNames)
	result := make(map[string]int, len(branches))
	for _, branch := range branches {
		result[branch.GetName()] = len(diffs[branch.GetName()].Files)
	}
	return result
}

// BatchBranchStats resolves the annotation stats (short SHA, commit count,
// additions/deletions) for every branch in one batched pass, keyed by branch
// name. Forge status (CI, reviews) is a separate concern joined at render time,
// not part of this.
func (e *engineImpl) BatchBranchStats(branches Branches) map[string]BranchStat {
	// Keep trunk's head for its short SHA, but exclude its range from diff/count work.
	ranges := batchByBranch(e, branches, func(b Branch, head, parentRev, storedBase string) git.RevRange {
		base := statBase(parentRev, storedBase)
		if e.IsTrunk(b) {
			base = head
		}
		return git.RevRange{Base: base, Head: head}
	})
	// Counts and the diff batch are independent. Overlap them so batching does
	// not add a serial diff phase to the latency of a small stack.
	var diffs map[string]git.DiffSummary
	var diffRead sync.WaitGroup
	diffRead.Go(func() {
		diffs = e.readBranchDiffs(context.Background(), ranges, git.DiffStats)
	})
	stats := make([]BranchStat, len(branches))
	indices := make([]int, len(branches))
	for i := range indices {
		indices[i] = i
	}
	utils.Run(indices, func(i int) {
		name := branches[i].GetName()
		rr := ranges[name]
		stats[i] = BranchStat{ShortSHA: utils.ShortRevision(rr.Head, 0)}
		if rr.Base != "" && rr.Head != "" {
			if count, err := e.commitCountBetween(rr); err == nil {
				stats[i].CommitCount = count
			}
		}
	})
	diffRead.Wait()
	result := make(map[string]BranchStat, len(branches))
	for i, branch := range branches {
		diff := diffs[branch.GetName()]
		stats[i].LinesAdded, stats[i].LinesDeleted = diff.Added, diff.Deleted
		result[branch.GetName()] = stats[i]
	}
	return result
}
