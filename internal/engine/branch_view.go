package engine

import (
	"context"
	"sync"
)

// DiffSummary is a branch's additions/deletions/files-changed relative to its
// parent, captured at the moment a BranchView is built.
type DiffSummary struct {
	Additions    int
	Deletions    int
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

// BatchBranchStats resolves the annotation stats for every branch in batched
// reads (one revision resolve + one cached metadata read, then parallel
// per-branch count and diff), keyed by branch name. It is the branch-state
// counterpart that lets annotation builders drop PreloadBranchData /
// PreloadBranchStats; forge status (CI, reviews) is a separate concern joined at
// render time, not part of this.
func (e *engineImpl) BatchBranchStats(branches Branches) map[string]BranchStat {
	result := make(map[string]BranchStat, len(branches))
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

	type entry struct {
		name string
		stat BranchStat
	}
	entries := make([]entry, len(branches))
	var wg sync.WaitGroup
	for i, b := range branches {
		wg.Add(1)
		go func(i int, b Branch) {
			defer wg.Done()
			name := b.GetName()
			head := revs[name]
			st := BranchStat{}
			if len(head) >= 7 {
				st.ShortSHA = head[:7]
			}
			// Trunk has no parent comparison; only its short SHA is meaningful.
			if !e.IsTrunk(b) {
				base := ""
				if m := metas[name]; m != nil {
					if rev := m.GetParentBranchRevision(); rev != nil && *rev != "" {
						base = *rev
					}
				}
				if base == "" {
					base = revs[b.GetParentOrTrunk()]
				}
				if c, err := e.commitCountBetween(base, head); err == nil {
					st.CommitCount = c
				}
				if a, d, err := e.diffStatsBetween(base, head); err == nil {
					st.LinesAdded = a
					st.LinesDeleted = d
				}
			}
			entries[i] = entry{name: name, stat: st}
		}(i, b)
	}
	wg.Wait()

	for _, en := range entries {
		result[en.name] = en.stat
	}
	return result
}

// BranchView is an immutable, point-in-time snapshot of per-branch read data,
// materialized in batched and parallel git reads when it is built. Consumers
// read plain values from it — there is no engine callback or global cache
// behind a lookup, so the data cannot go stale behind a caller's back the way
// an ambient cache can. Build a fresh view after a mutation rather than reusing
// an old one.
type BranchView struct {
	revisions map[string]string
	commits   map[string][]string
	diffs     map[string]DiffSummary
}

// Revision returns the branch tip SHA captured when the view was built.
func (v *BranchView) Revision(branch string) string { return v.revisions[branch] }

// Commits returns the formatted commit list captured for the branch.
func (v *BranchView) Commits(branch string) []string { return v.commits[branch] }

// Diff returns the diff summary captured for the branch.
func (v *BranchView) Diff(branch string) DiffSummary { return v.diffs[branch] }

// ViewBranches builds a BranchView for the given branches. Every branch and
// parent revision is resolved in one batched call and every branch's metadata is
// read in one batched (cached) call; each branch's commits and diff summary are
// then computed in parallel from those pre-resolved values. The view does not
// warm or read any engine-global revision cache — no PreloadBranchData /
// PreloadBranchStats needed — and no per-branch git rev-parse is spawned.
func (e *engineImpl) ViewBranches(ctx context.Context, branches Branches, format CommitFormat) *BranchView {
	view := &BranchView{
		revisions: make(map[string]string, len(branches)),
		commits:   make(map[string][]string, len(branches)),
		diffs:     make(map[string]DiffSummary, len(branches)),
	}
	if len(branches) == 0 {
		return view
	}

	// Resolve every branch and parent revision in one batched call, and read all
	// branch metadata in one batched (cache-backed) call, so the per-branch work
	// below derives its base/head SHAs from these maps without any git of its own.
	branchNames := make([]string, 0, len(branches))
	revNames := make([]string, 0, len(branches)*2)
	for _, b := range branches {
		branchNames = append(branchNames, b.GetName())
		revNames = append(revNames, b.GetName(), b.GetParentOrTrunk())
	}
	revs, _ := e.GetRevisions(revNames)
	metas, _ := e.batchReadMetadata(branchNames)
	for _, b := range branches {
		view.revisions[b.GetName()] = revs[b.GetName()]
	}

	// Compute commits and the diff summary for each branch concurrently, into a
	// positional slice so the goroutines never share a map.
	type entry struct {
		name    string
		commits []string
		diff    DiffSummary
	}
	entries := make([]entry, len(branches))
	var wg sync.WaitGroup
	for i, b := range branches {
		if e.IsTrunk(b) {
			continue
		}
		wg.Add(1)
		go func(i int, b Branch) {
			defer wg.Done()
			name := b.GetName()
			head := revs[name]
			parentRev := revs[b.GetParentOrTrunk()]

			// Stored divergence point, mirroring how the deep accessors pick a base.
			storedBase := ""
			if m := metas[name]; m != nil {
				if rev := m.GetParentBranchRevision(); rev != nil {
					storedBase = *rev
				}
			}

			en := entry{name: name}
			// Commits: GetAllCommits compares against the stored base (empty if unset).
			if commits, err := e.commitsBetween(storedBase, head, format); err == nil {
				en.commits = commits
			}
			// Additions/deletions: GetDiffStats compares against the stored base,
			// falling back to the parent's current tip when none is stored.
			diffBase := storedBase
			if diffBase == "" {
				diffBase = parentRev
			}
			if added, deleted, err := e.diffStatsBetween(diffBase, head); err == nil {
				en.diff.Additions = added
				en.diff.Deletions = deleted
			}
			// Files changed: matches the original, which diffs against the parent's
			// current tip rather than the stored divergence point.
			if parentRev != "" && head != "" {
				if files, err := e.GetChangedFiles(ctx, parentRev, head); err == nil {
					en.diff.FilesChanged = len(files)
				}
			}
			entries[i] = en
		}(i, b)
	}
	wg.Wait()

	for _, en := range entries {
		if en.name == "" {
			continue
		}
		view.commits[en.name] = en.commits
		view.diffs[en.name] = en.diff
	}
	return view
}
