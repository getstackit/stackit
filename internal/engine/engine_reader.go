package engine

import (
	"cmp"
	"context"
	"fmt"
	"iter"
	"slices"

	"github.com/getstackit/stackit/internal/errors"
	"github.com/getstackit/stackit/internal/git"
)

// AllBranches returns all branches.
func (e *engineImpl) AllBranches() Branches {
	e.mu.RLock()
	defer e.mu.RUnlock()
	branches := make([]Branch, len(e.state.branches))
	for i, name := range e.state.branches {
		branches[i] = NewBranch(name, e)
	}
	return NewBranches(branches)
}

// BranchNames returns a cached BranchSet for O(1) branch name lookups.
func (e *engineImpl) BranchNames() *BranchSet {
	e.mu.RLock()
	if e.state.branchNamesSet != nil {
		defer e.mu.RUnlock()
		return e.state.branchNamesSet
	}
	e.mu.RUnlock()

	// Build and cache with write lock
	e.mu.Lock()
	defer e.mu.Unlock()

	// Double-check after acquiring write lock
	if e.state.branchNamesSet != nil {
		return e.state.branchNamesSet
	}

	e.state.branchNamesSet = newBranchSet(e.state.branches)
	return e.state.branchNamesSet
}

// CurrentBranch returns the current branch (nil if not on a branch)
func (e *engineImpl) CurrentBranch() *Branch {
	current, err := e.git.GetCurrentBranch()
	if err != nil {
		// Not on a branch (e.g., detached HEAD)
		current = ""
	}

	e.mu.Lock()
	e.currentBranch = current
	e.mu.Unlock()

	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.currentBranch == "" {
		return nil
	}
	branch := NewBranch(e.currentBranch, e)
	return &branch
}

// CurrentBranchName returns the current branch name, or "" when HEAD is not on
// a branch (e.g. a detached-HEAD server mirror). Use this for nil-safe reads
// where a missing current branch should be absent rather than an error.
func (e *engineImpl) CurrentBranchName() string {
	if cb := e.CurrentBranch(); cb != nil {
		return cb.GetName()
	}
	return ""
}

// ValidateOnBranch ensures the user is on a branch
func (e *engineImpl) ValidateOnBranch() (string, error) {
	currentBranch := e.CurrentBranch()
	if currentBranch == nil {
		return "", errors.ErrNotOnBranch
	}
	return currentBranch.GetName(), nil
}

// Trunk returns the trunk branch
func (e *engineImpl) Trunk() Branch {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return NewBranch(e.trunk, e)
}

// GetBranch returns a Branch wrapper for the given branch name
func (e *engineImpl) GetBranch(branchName string) Branch {
	return NewBranch(branchName, e)
}

// GetParent returns the parent branch (nil if no parent)
func (e *engineImpl) GetParent(branch Branch) *Branch {
	e.ensureBranchSharedLoaded(branch.GetName())
	e.mu.RLock()
	defer e.mu.RUnlock()

	if state := e.readState(branch.GetName()); state != nil {
		b := NewBranch(state.Parent, e)
		return &b
	}
	return nil
}

// FindNearestNonExcludedAncestor walks the parent chain starting from
// startParent and returns the first branch name for which isExcluded returns
// false. Falls back to trunk if every ancestor up the chain is excluded.
// Used by branch-deletion and similar workflows that need to reparent
// children past a set of branches being removed.
func (e *engineImpl) FindNearestNonExcludedAncestor(startParent string, isExcluded func(name string) bool) string {
	current := startParent
	for isExcluded(current) {
		parent := e.GetBranch(current).GetParent()
		if parent == nil {
			return e.Trunk().GetName()
		}
		current = parent.GetName()
	}
	return current
}

// FindMostRecentTrackedAncestors finds the most recent tracked ancestors of a branch
// by checking the branch's commit history against tracked branch tips.
// Returns a slice of branch names that point to the most recent tracked commit in history.
func (e *engineImpl) FindMostRecentTrackedAncestors(ctx context.Context, branchName string) ([]string, error) {
	if e.IsTrunk(e.GetBranch(branchName)) {
		return nil, nil
	}

	e.mu.RLock()
	defer e.mu.RUnlock()

	trunk := e.trunk

	// Collect trunk plus every tracked candidate (skipping the branch itself
	// and branches already merged into trunk) so their revisions can be
	// resolved in a single batched git call instead of one per branch.
	candidates := []string{trunk}
	for _, candidate := range e.state.branches {
		// Skip the branch itself and trunk (already handled)
		if candidate == branchName || candidate == trunk {
			continue
		}

		// Only consider tracked branches
		if !e.state.branchState.HasByName(candidate) {
			continue
		}

		// Skip branches merged into trunk
		if merged, err := e.git.IsMerged(ctx, candidate, trunk); err == nil && merged {
			continue
		}

		candidates = append(candidates, candidate)
	}

	revisions, _ := e.git.BatchGetRevisions(candidates)

	// Map of commit SHA to slice of tracked branch names
	trackedBranchTips := make(map[string][]string)
	for _, candidate := range candidates {
		rev, ok := revisions[candidate]
		if !ok {
			continue
		}
		trackedBranchTips[rev] = append(trackedBranchTips[rev], candidate)
	}

	// Get history of the branch we're tracking
	history, err := e.git.GetCommitHistorySHAs(ctx, branchName)
	if err != nil {
		return nil, err
	}

	// Iterate through history (newest to oldest) and find the first tracked tip(s)
	for i := range history {
		sha := history[i]
		if ancestors, ok := trackedBranchTips[sha]; ok {
			// Found the most recent tracked commit(s)
			return ancestors, nil
		}
	}

	return nil, nil
}

// FindBranchesForCommits maps each requested commit SHA to the tracked branch
// that owns it (the branch whose parent..tip range contains the commit). It
// scans each branch at most once, so the cost is O(branches) git-log
// invocations regardless of how many SHAs are requested — the batch counterpart
// to looking up commits one at a time. Commits not owned by any branch are
// simply absent from the returned map.
func (e *engineImpl) FindBranchesForCommits(commitSHAs []string) map[string]string {
	result := make(map[string]string, len(commitSHAs))
	if len(commitSHAs) == 0 {
		return result
	}

	want := make(map[string]struct{}, len(commitSHAs))
	for _, sha := range commitSHAs {
		want[sha] = struct{}{}
	}

	e.mu.RLock()
	branches := make([]string, len(e.state.branches))
	copy(branches, e.state.branches)
	e.mu.RUnlock()

	for _, branchName := range branches {
		if len(result) == len(want) {
			break
		}
		commits, err := e.GetAllCommits(NewBranch(branchName, e), CommitFormatSHA)
		if err != nil {
			continue
		}
		for _, sha := range commits {
			if _, ok := want[sha]; !ok {
				continue
			}
			if _, mapped := result[sha]; !mapped {
				result[sha] = branchName
			}
		}
	}

	return result
}

// RefDecorations returns local branch and tag refs grouped by the commit SHA
// they point at, for git-log-style annotations. Annotated tags are dereferenced
// to the commit they wrap. Thin passthrough to the git layer.
func (e *engineImpl) RefDecorations() (map[string][]git.RefDecoration, error) {
	return e.git.RefDecorations()
}

// SortBranchesTopologically sorts branches so parents come before children.
// This ensures correct restack order (bottom of stack first).
func (e *engineImpl) SortBranchesTopologically(branches Branches) Branches {
	if len(branches) == 0 {
		return branches
	}

	// Build the graph once and pre-compute each branch's depth to avoid
	// repeated lookups inside the sort comparator (O(n²) → O(n log n)).
	graph := e.Graph(SortStrategyAlphabetical)
	depth := make(map[string]int, len(branches))
	for _, b := range branches {
		if node := graph.GetNode(b.GetName()); node != nil {
			depth[b.GetName()] = node.Depth
		}
	}

	result := make([]Branch, len(branches))
	copy(result, branches)
	slices.SortFunc(result, func(a, b Branch) int {
		if c := cmp.Compare(depth[a.GetName()], depth[b.GetName()]); c != 0 {
			return c
		}
		return cmp.Compare(a.GetName(), b.GetName())
	})

	return result
}

// BranchesDepthFirst returns an iterator that yields branches starting from startBranch in depth-first order.
// Each iteration yields (branchName, depth) where depth is 0 for the start branch.
// The iterator can be used with range loops and supports early termination with break.
func (e *engineImpl) BranchesDepthFirst(startBranch Branch) iter.Seq2[Branch, int] {
	return func(yield func(Branch, int) bool) {
		visited := make(map[string]bool)
		var visit func(branch string, depth int) bool
		visit = func(branch string, depth int) bool {
			if visited[branch] {
				return true // cycle detection
			}
			visited[branch] = true

			if !yield(NewBranch(branch, e), depth) {
				return false // iterator wants to stop
			}

			// Get children directly from internal map
			e.mu.RLock()
			children := e.state.childrenMap[branch]
			e.mu.RUnlock()

			for _, childName := range children {
				if !visit(childName, depth+1) {
					return false
				}
			}
			return true
		}

		visit(startBranch.GetName(), 0)
	}
}

// GetDivergencePoint returns the divergence point of a branch from its parent.
// This is the commit at which the branch diverged from its parent, used as the
// OldUpstream for rebase operations.
//
// Returns the ParentBranchRevision from metadata if available and non-empty,
// otherwise falls back to the parent's current revision.
func (e *engineImpl) GetDivergencePoint(branchName string) (string, error) {
	// First, try to get from metadata
	meta, err := e.readMetadata(branchName)
	if rev := meta.GetParentBranchRevision(); err == nil && rev != nil && *rev != "" {
		return *rev, nil
	}

	// Get the parent branch
	e.mu.RLock()
	state := e.readState(branchName)
	e.mu.RUnlock()

	if state == nil {
		return "", fmt.Errorf("branch %s is not tracked", branchName)
	}

	parentName := state.Parent
	if parentName == "" {
		// No parent means parent is trunk
		e.mu.RLock()
		parentName = e.trunk
		e.mu.RUnlock()
	}

	// Fall back to parent's current revision
	return e.git.GetRevision(parentName)
}
