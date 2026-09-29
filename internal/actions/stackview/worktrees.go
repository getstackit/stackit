// Package stackview builds read-only view models of the stack — the managed
// worktree index and per-branch display annotations — for adapters (CLI, TUI,
// API) to render. It never renders anything itself: adapters convert these
// values into their own presentation types.
package stackview

import (
	"iter"

	"github.com/getstackit/stackit/internal/engine"
)

// WorktreeSource is the engine surface BuildWorktreeIndex reads.
type WorktreeSource interface {
	ListManagedWorktrees() ([]engine.WorktreeInfo, error)
	GetBranch(branchName string) engine.Branch
	BranchesDepthFirst(startBranch engine.Branch) iter.Seq2[engine.Branch, int]
}

// WorktreeIndex holds managed worktree information resolved from a single
// ListManagedWorktrees call, so per-branch lookups are map reads.
type WorktreeIndex struct {
	// Empty maps worktree anchor branches that have no child branches.
	Empty map[string]*engine.WorktreeInfo
	// ByStackRoot maps every managed worktree's stack root to its info.
	ByStackRoot map[string]*engine.WorktreeInfo
}

// BuildWorktreeIndex lists managed worktrees once and indexes them by stack
// root, also recording which worktree anchors are empty (have no descendants).
// A listing failure yields an empty index.
func BuildWorktreeIndex(src WorktreeSource) *WorktreeIndex {
	idx := &WorktreeIndex{
		Empty:       make(map[string]*engine.WorktreeInfo),
		ByStackRoot: make(map[string]*engine.WorktreeInfo),
	}

	worktrees, err := src.ListManagedWorktrees()
	if err != nil {
		return idx
	}

	for i := range worktrees {
		wt := &worktrees[i]
		idx.ByStackRoot[wt.AnchorBranch] = wt

		anchor := src.GetBranch(wt.AnchorBranch)
		if !anchor.IsTracked() || !anchor.IsWorktreeAnchor() {
			continue
		}
		if !hasDescendants(src, anchor) {
			idx.Empty[wt.AnchorBranch] = wt
		}
	}

	return idx
}

func hasDescendants(src WorktreeSource, anchor engine.Branch) bool {
	for _, depth := range src.BranchesDepthFirst(anchor) {
		if depth > 0 {
			return true
		}
	}
	return false
}

// EmptyAnchorNames returns the set of empty worktree anchor names, or nil when
// there are none, for renderers that keep empty anchors visible.
func (idx *WorktreeIndex) EmptyAnchorNames() map[string]bool {
	if idx == nil || len(idx.Empty) == 0 {
		return nil
	}
	names := make(map[string]bool, len(idx.Empty))
	for name := range idx.Empty {
		names[name] = true
	}
	return names
}

// stackRootResolver resolves a branch's stack root for worktree lookups.
type stackRootResolver interface {
	GetStackRootForBranch(branch engine.Branch) string
}

// apply populates the worktree fields of ann. An empty worktree anchor is
// flagged as such; otherwise any branch whose stack has a managed worktree
// carries that worktree's path, so stack ownership is visible on every branch.
func (idx *WorktreeIndex) apply(eng stackRootResolver, branch engine.Branch, ann *BranchAnnotation) {
	if idx == nil {
		return
	}
	if wt, ok := idx.Empty[branch.GetName()]; ok {
		ann.IsEmptyWorktree = true
		ann.WorktreePath = wt.Path
		return
	}
	if wt, ok := idx.ByStackRoot[eng.GetStackRootForBranch(branch)]; ok {
		ann.WorktreePath = wt.Path
	}
}
