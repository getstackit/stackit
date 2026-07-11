package engine

import (
	"context"
	"fmt"
	"slices"

	"github.com/getstackit/stackit/internal/git"
)

// rebuildInternal is the internal rebuild logic without locking
// refreshCurrentBranch indicates whether to refresh currentBranch from Git
func (e *engineImpl) rebuildInternal(refreshCurrentBranch bool) error {
	// Get all branch names
	branches, err := e.git.GetAllBranchNames(context.Background())
	if err != nil {
		return fmt.Errorf("failed to get branches: %w", err)
	}

	var currentBranch string
	// Refresh current branch from Git if requested (needed when called from Rebuild/Reset after branch switches)
	if refreshCurrentBranch {
		cb, err := e.git.GetCurrentBranch()
		if err == nil {
			currentBranch = cb
		}
	}

	// Load metadata for each branch in parallel
	allMeta, _ := e.batchReadMetadata(branches)
	allLocalMeta := e.batchReadLocalMetadata(branches)

	e.applyRebuild(branches, currentBranch, allMeta, allLocalMeta)
	return nil
}

// applyRebuild updates the internal state from the provided metadata results.
// The caller MUST hold the engine's write lock (e.mu).
//
// After a full rebuild both lazy-load gates are considered open — subsequent
// ensureSharedLoaded / ensureLocalLoaded calls become no-ops via the atomic
// fast path. We can't run sharedLoadOnce/localLoadOnce here (they'd block
// future loads even on Reset), so we set the atomic flags directly.
func (e *engineImpl) applyRebuild(branches []string, currentBranch string, allMeta MetaMap, allLocalMeta map[string]*git.LocalMeta) {
	e.state.rebuildFromMetadata(e.trunk, branches, allMeta, allLocalMeta)
	if currentBranch != "" {
		e.currentBranch = currentBranch
	}
	e.sharedLoaded.Store(true)
	e.localLoaded.Store(true)
}

// rebuild loads all branches and their metadata from Git
func (e *engineImpl) rebuild() error {
	// Clear metadata cache to pick up external changes (e.g., branches created in another terminal)
	e.git.ClearMetadataCache()

	// 1. Get all branch names (slow)
	branches, err := e.git.GetAllBranchNames(context.Background())
	if err != nil {
		return fmt.Errorf("failed to get branches: %w", err)
	}

	// 2. Get current branch (slow)
	currentBranch, _ := e.git.GetCurrentBranch()

	// 3. Load metadata for each branch in parallel (slow)
	allMeta, _ := e.batchReadMetadata(branches)
	allLocalMeta := e.batchReadLocalMetadata(branches)

	e.mu.Lock()
	defer e.mu.Unlock()

	// 4. Update maps (fast)
	e.applyRebuild(branches, currentBranch, allMeta, allLocalMeta)
	return nil
}

// prStateIsMerged reports whether a branch's PR was recorded as merged in
// metadata. Works even when the branch ref itself no longer exists, since
// PR info lives on the metadata ref.
func (e *engineImpl) prStateIsMerged(branchName string) bool {
	prInfo, err := e.GetPrInfo(e.GetBranch(branchName))
	return err == nil && prInfo != nil && prInfo.State() == git.PRStateMerged
}

func metaHasSubmittedPR(meta *git.Meta) bool {
	if meta == nil {
		return false
	}
	prMeta := meta.GetPrInfo()
	return prMeta != nil && prMeta.Number != nil && *prMeta.Number != 0
}

// branchLanded reports whether branchName's changes have landed into target.
// PR metadata is authoritative across GitHub's merge, squash, and rebase merge
// methods. Git fallback is intentionally limited to trunk: for non-trunk
// parents, patch-equivalence can also mean a local stack operation made a child
// empty, not that a GitHub PR landed.
//
// The trunk Git fallback deliberately does not require stackit PR metadata.
// Collaborators may squash- or rebase-merge a plain GitHub branch into main, and
// a child stacked on that local branch still needs to reparent past it without
// replaying the collaborator's commits. Callers that delete branches must add
// their own metadata/confirmation gate before using this predicate.
func (e *engineImpl) branchLanded(ctx context.Context, branchName, target string, squashCache *git.SquashMergeCache) bool {
	if branchName == e.trunk {
		return false
	}
	if e.prStateIsMerged(branchName) {
		return true
	}
	if target == "" || target != e.trunk {
		return false
	}
	// IsMerged covers ancestry and per-commit (rebase) merges cheaply.
	if merged, err := e.git.IsMerged(ctx, branchName, target); err == nil && merged {
		return true
	}
	// Squash merges need the aggregate patch-id scan, which is expensive and
	// intentionally stays out of the hot IsMerged primitive. Only explicit
	// "did this branch land into trunk" checks opt into it.
	merged, err := e.git.IsSquashMerged(ctx, branchName, target, squashCache)
	return err == nil && merged
}

// shouldReparentBranch checks if a parent branch should be reparented
// Returns true if the parent branch:
// - No longer exists locally
// - Has been merged into its own parent
// - Has a "MERGED" PR state in metadata
func (e *engineImpl) shouldReparentBranch(ctx context.Context, parentBranchName string, metaMap MetaMap, squashCache *git.SquashMergeCache) bool {
	// Check if parent is trunk (no need to reparent)
	if parentBranchName == e.trunk {
		return false
	}

	// Worktree anchor branches should never be reparented - they are permanent parents
	// for their stacks, even though they may be at trunk HEAD
	if e.IsWorktreeAnchor(e.GetBranch(parentBranchName)) {
		return false
	}

	// Check if parent branch still exists locally
	parentExists := slices.Contains(e.state.branches, parentBranchName)
	if !parentExists {
		return true
	}

	// Check if parent has landed. Git-only detection is safe for trunk; stacked
	// PR bases rely on merged PR metadata so local fold/squash operations do
	// not get mistaken for GitHub merges.
	mergeTarget := e.trunk
	if state := e.readState(parentBranchName); state != nil && state.Parent != "" {
		mergeTarget = state.Parent
	}
	if e.branchLanded(ctx, parentBranchName, mergeTarget, squashCache) {
		return true
	}

	// Check if parent has "MERGED" PR state in metadata
	if metaMap != nil {
		if meta, ok := metaMap[parentBranchName]; ok && meta != nil {
			prInfo := meta.GetPrInfo()
			if prInfo != nil && prInfo.State != nil && *prInfo.State == git.PRStateMerged {
				return true
			}
			// If metadata exists but state isn't MERGED, we don't need to check further
			return false
		}
	}

	// Fall back to engine cache/disk if not in metaMap or state unknown
	return e.prStateIsMerged(parentBranchName)
}

// findNearestValidAncestor finds the nearest ancestor that hasn't been merged/deleted
// Returns trunk if all ancestors have been merged
func (e *engineImpl) findNearestValidAncestor(ctx context.Context, branchName string, metaMap MetaMap, squashCache *git.SquashMergeCache) string {
	// Get the starting parent from branchState
	state := e.readState(branchName)
	if state == nil {
		return e.trunk
	}
	current := state.Parent

	for current != "" && current != e.trunk {
		if !e.shouldReparentBranch(ctx, current, metaMap, squashCache) {
			return current
		}
		// Move to the next parent
		parentState := e.readState(current)
		if parentState == nil {
			break
		}
		current = parentState.Parent
	}

	return e.trunk
}

// appendMergedDownstack captures the old parent information when a branch is reparented.
// It also inherits any merged history from the old parent for multi-level reparenting.
func (e *engineImpl) appendMergedDownstack(
	branchName string,
	oldParent string,
	metaMap MetaMap,
) error {
	// Always read fresh metadata from disk for the branch being modified.
	// This is critical because SetParent has just written the new parent,
	// and metaMap may contain stale metadata with the old parent.
	meta, err := e.readMetadata(branchName)
	if err != nil {
		return err
	}
	if meta == nil {
		return nil
	}

	meta = e.withMergedDownstack(meta, oldParent, metaMap)

	// Write and update cache
	if err := e.writeMetadata(branchName, meta); err != nil {
		return err
	}
	if metaMap != nil {
		metaMap[branchName] = meta
	}
	return nil
}

func (e *engineImpl) withMergedDownstack(meta *git.Meta, oldParent string, metaMap MetaMap) *git.Meta {
	if meta == nil || oldParent == "" {
		return meta
	}

	oldParentMeta := e.getMetaFromMapOrDisk(oldParent, metaMap)
	mp := git.MergedParent{BranchName: oldParent}
	if oldParentMeta != nil {
		oldParentPrInfo := oldParentMeta.GetPrInfo()
		if oldParentPrInfo != nil {
			mp.PRNumber = oldParentPrInfo.Number
			mp.PRState = oldParentPrInfo.State
		}
	}

	// Inherit old parent's history (for multi-level: A→B→C, if B merges)
	var history []git.MergedParent
	if oldParentMeta != nil {
		history = append(history, oldParentMeta.GetMergedDownstack()...)
	}

	// Check if oldParent already in history (prevent duplicates from retried operations)
	for _, existing := range history {
		if existing.BranchName == oldParent {
			return meta.WithMergedDownstack(history)
		}
	}

	history = append(history, mp)

	// Limit to last 5 entries
	const maxHistoryEntries = 5
	if len(history) > maxHistoryEntries {
		history = history[len(history)-maxHistoryEntries:]
	}

	return meta.WithMergedDownstack(history)
}

// getMetaFromMapOrDisk retrieves metadata from the cache map or from disk if not found.
func (e *engineImpl) getMetaFromMapOrDisk(branchName string, metaMap MetaMap) *git.Meta {
	if meta := metaMap.Get(branchName); meta != nil {
		return meta
	}
	meta, err := e.readMetadata(branchName)
	if err != nil {
		return nil
	}
	return meta
}

// Helper functions
func getStringValue[T ~string](s *T) T {
	if s == nil {
		return ""
	}
	return *s
}

func getBoolValue(b *bool) bool {
	if b == nil {
		return false
	}
	return *b
}

func stringPtr[T ~string](s T) *T {
	if s == "" {
		return nil
	}
	return &s
}
