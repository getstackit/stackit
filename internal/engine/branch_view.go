package engine

import (
	"context"

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

// readBranchRanges resolves a point-in-time view shared by history and stats.
// Failures stay attached to branches; display callers may omit failed reads,
// while operations that need complete history must propagate them.
func (e *engineImpl) readBranchRanges(ctx context.Context, branches Branches) git.ReadResults[git.RevRange] {
	result := git.ReadResults[git.RevRange]{}
	refs := make([]string, 0, 2*len(branches))
	for _, branch := range branches {
		refs = append(refs, branch.GetName(), branch.GetParentOrTrunk())
	}
	revs := e.git.ReadRevisions(ctx, refs...)
	metas := e.metadata.ReadMetadata(ctx, branches.Names()...)
	for _, branch := range branches {
		name := branch.GetName()
		head, err := revs.Get(name)
		if err != nil {
			result.Fail(name, err)
			continue
		}
		if e.IsTrunk(branch) {
			result.Set(name, git.RevRange{Base: head, Head: head})
			continue
		}
		meta, err := metas.Get(name)
		if err != nil {
			result.Fail(name, err)
			continue
		}
		base := ""
		if rev := meta.GetParentBranchRevision(); rev != nil {
			base = *rev
		}
		if base == "" {
			base, err = revs.Get(branch.GetParentOrTrunk())
		}
		result.Record(name, git.RevRange{Base: base, Head: head}, err)
	}
	return result
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

// BatchCommits returns typed display/replay data, omitting failed branches for
// best-effort display callers. Use ReadBranchCommits when errors must propagate.
func (e *engineImpl) BatchCommits(branches Branches) map[string]git.Commits {
	data := e.ReadBranchCommits(context.Background(), branches)
	result := make(map[string]git.Commits, len(branches))
	for name, history := range data.All() {
		if history.Err == nil {
			result[name] = history.Value.Commits
		}
	}
	return result
}

// BranchCommitRange retains the resolved endpoints alongside their commits so
// consumers can format history and diffs from the same point-in-time read.
type BranchCommitRange struct {
	Range   git.RevRange
	Commits git.Commits
}

// ReadBranchCommits retains per-branch errors for consumers such as absorb,
// where an incomplete history cannot safely be treated as an empty branch.
func (e *engineImpl) ReadBranchCommits(ctx context.Context, branches Branches) git.ReadResults[BranchCommitRange] {
	return readBranchHistory(e, ctx, branches, e.git.ReadCommitRanges, func(rr git.RevRange, commits []git.CommitMetadata) BranchCommitRange {
		return BranchCommitRange{Range: rr, Commits: commits}
	})
}

// ReadBranchCommitNodes reads branch topology without loading display fields.
func (e *engineImpl) ReadBranchCommitNodes(ctx context.Context, branches Branches) git.ReadResults[[]git.CommitNode] {
	return readBranchHistory(e, ctx, branches, e.git.ReadCommitNodes, func(_ git.RevRange, nodes []git.CommitNode) []git.CommitNode {
		return nodes
	})
}

func readBranchHistory[C, T any](e *engineImpl, ctx context.Context, branches Branches, read func(context.Context, ...git.RevRange) git.ReadResults[C], project func(git.RevRange, C) T) git.ReadResults[T] {
	var result git.ReadResults[T]
	snapshot := e.readBranchRanges(ctx, branches)
	ranges := make([]git.RevRange, 0, len(branches))
	byBranch := make(map[string]git.RevRange)
	for _, branch := range branches {
		name := branch.GetName()
		if e.IsTrunk(branch) {
			var zero T
			result.Set(name, zero)
			continue
		}
		rr, err := snapshot.Get(name)
		if err != nil {
			result.Fail(name, err)
			continue
		}
		ranges = append(ranges, rr)
		byBranch[name] = rr
	}
	data := read(ctx, ranges...)
	for name, rr := range byBranch {
		commits, err := data.Get(rr.String())
		result.Record(name, project(rr, commits), err)
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
