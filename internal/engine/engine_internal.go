package engine

import (
	"context"
	"fmt"
	"slices"

	"stackit.dev/stackit/internal/git"
)

// rebuildInternal is the internal rebuild logic without locking
// refreshCurrentBranch indicates whether to refresh currentBranch from Git
func (e *engineImpl) rebuildInternal(refreshCurrentBranch bool) error {
	// Get all branch names
	branches, err := e.git.GetAllBranchNames()
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
	allMeta, _ := e.git.BatchReadMetadata(branches)
	allLocalMeta := e.git.BatchReadLocalMetadata(branches)

	e.applyRebuild(branches, currentBranch, allMeta, allLocalMeta)
	return nil
}

// applyRebuild updates the internal state from the provided metadata results.
// The caller MUST hold the engine's write lock (e.mu).
func (e *engineImpl) applyRebuild(branches []string, currentBranch string, allMeta map[string]*git.Meta, allLocalMeta map[string]*git.LocalMeta) {
	e.branches = branches
	e.branchNamesSet = nil // invalidate cache
	if currentBranch != "" {
		e.currentBranch = currentBranch
	}

	// Reset state
	e.branchState = make(BranchStateMap)
	e.childrenMap = make(map[string][]string)

	// Collect results and populate state sequentially to avoid lock contention/races
	for name, meta := range allMeta {
		if name == e.trunk {
			continue // Trunk branches should never be tracked
		}

		if meta.ParentBranchName == nil {
			continue // No parent means not tracked
		}

		parent := *meta.ParentBranchName
		if parent == name {
			continue // Skip self-parenting to avoid cycles
		}

		// Create or get branch state
		state := &BranchState{
			Parent:     parent,
			LockReason: meta.LockReason,
			BranchType: meta.BranchType,
		}
		if meta.Scope != nil {
			state.Scope = *meta.Scope
		}

		e.branchState.Set(name, state)
		e.childrenMap[parent] = append(e.childrenMap[parent], name)
	}

	// Apply local metadata (frozen state)
	// Note: We create a BranchState entry for frozen branches even if they're not
	// tracked, since frozen status is independent of tracked status.
	for name, meta := range allLocalMeta {
		if meta.Frozen {
			state := e.branchState.GetOrCreate(name)
			state.Frozen = true
		}
	}

	// Sort children by name for deterministic traversal
	for _, children := range e.childrenMap {
		slices.Sort(children)
	}
}

// updateBranchInCache updates the cache for a specific branch after restack/metadata changes
func (e *engineImpl) updateBranchInCache(branchName string) {
	if branchName == e.trunk {
		return
	}

	// Get the old parent before updating (for children map maintenance)
	var oldParent string
	if oldState := e.branchState.GetByName(branchName); oldState != nil {
		oldParent = oldState.Parent
	}

	// Read metadata for this branch
	meta, err := e.git.ReadMetadata(branchName)
	if err != nil {
		// If metadata doesn't exist, remove branch from state and children map
		if oldParent != "" {
			e.removeFromChildren(oldParent, branchName)
		}
		e.branchState.Delete(branchName)
		return
	}

	// Read local metadata too
	localMeta, _ := e.git.ReadLocalMetadata(branchName)

	// Determine new parent
	newParent := ""
	if meta.ParentBranchName != nil {
		newParent = *meta.ParentBranchName
	}

	// If no parent, branch is not tracked - remove it
	if newParent == "" {
		if oldParent != "" {
			e.removeFromChildren(oldParent, branchName)
		}
		e.branchState.Delete(branchName)
		return
	}

	// Create or update branch state
	state := &BranchState{
		Parent:     newParent,
		LockReason: meta.LockReason,
		BranchType: meta.BranchType,
	}
	if meta.Scope != nil {
		state.Scope = *meta.Scope
	}
	if localMeta != nil {
		state.Frozen = localMeta.Frozen
	}

	e.branchState.Set(branchName, state)

	// Update children map - remove from old parent, add to new parent
	if oldParent != "" && oldParent != newParent {
		e.removeFromChildren(oldParent, branchName)
	}

	// Add to new parent's children if not already there
	if newParent != "" && (oldParent == "" || oldParent != newParent) {
		e.childrenMap[newParent] = append(e.childrenMap[newParent], branchName)
		slices.Sort(e.childrenMap[newParent])
	}
}

// removeFromChildren removes a branch from its parent's children list
func (e *engineImpl) removeFromChildren(parent, child string) {
	if children, ok := e.childrenMap[parent]; ok {
		if i := slices.Index(children, child); i >= 0 {
			e.childrenMap[parent] = slices.Delete(children, i, i+1)
		}
		if len(e.childrenMap[parent]) == 0 {
			delete(e.childrenMap, parent)
		}
	}
}

// rebuild loads all branches and their metadata from Git
func (e *engineImpl) rebuild() error {
	// 1. Get all branch names (slow)
	branches, err := e.git.GetAllBranchNames()
	if err != nil {
		return fmt.Errorf("failed to get branches: %w", err)
	}

	// 2. Get current branch (slow)
	currentBranch, _ := e.git.GetCurrentBranch()

	// 3. Load metadata for each branch in parallel (slow)
	allMeta, _ := e.git.BatchReadMetadata(branches)
	allLocalMeta := e.git.BatchReadLocalMetadata(branches)

	e.mu.Lock()
	defer e.mu.Unlock()

	// 4. Update maps (fast)
	e.applyRebuild(branches, currentBranch, allMeta, allLocalMeta)
	return nil
}

// shouldReparentBranch checks if a parent branch should be reparented
// Returns true if the parent branch:
// - No longer exists locally
// - Has been merged into trunk
// - Has a "MERGED" PR state in metadata
func (e *engineImpl) shouldReparentBranch(ctx context.Context, parentBranchName string, metaMap map[string]*git.Meta) bool {
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
	parentExists := false
	for _, name := range e.branches {
		if name == parentBranchName {
			parentExists = true
			break
		}
	}
	if !parentExists {
		return true
	}

	// Check if parent has been merged into trunk
	merged, err := e.git.IsMerged(ctx, parentBranchName, e.trunk)
	if err == nil && merged {
		return true
	}

	// Check if parent has "MERGED" PR state in metadata
	if metaMap != nil {
		if meta, ok := metaMap[parentBranchName]; ok && meta != nil && meta.PrInfo != nil {
			if meta.PrInfo.State != nil && *meta.PrInfo.State == "MERGED" {
				return true
			}
			// If metadata exists but state isn't MERGED, we don't need to check further
			return false
		}
	}

	// Fall back to engine cache/disk if not in metaMap or state unknown
	parentBranch := e.GetBranch(parentBranchName)
	prInfo, err := e.GetPrInfo(parentBranch)
	if err == nil && prInfo != nil && prInfo.State() == "MERGED" {
		return true
	}

	return false
}

// findNearestValidAncestor finds the nearest ancestor that hasn't been merged/deleted
// Returns trunk if all ancestors have been merged
func (e *engineImpl) findNearestValidAncestor(ctx context.Context, branchName string, metaMap map[string]*git.Meta) string {
	// Get the starting parent from branchState
	state := e.branchState.GetByName(branchName)
	if state == nil {
		return e.trunk
	}
	current := state.Parent

	for current != "" && current != e.trunk {
		if !e.shouldReparentBranch(ctx, current, metaMap) {
			return current
		}
		// Move to the next parent
		parentState := e.branchState.GetByName(current)
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
	metaMap map[string]*git.Meta,
) error {
	// Always read fresh metadata from disk for the branch being modified.
	// This is critical because SetParent has just written the new parent,
	// and metaMap may contain stale metadata with the old parent.
	meta, err := e.git.ReadMetadata(branchName)
	if err != nil {
		return err
	}
	if meta == nil {
		return nil
	}

	// Get old parent metadata
	oldParentMeta := e.getMetaFromMapOrDisk(oldParent, metaMap)

	// Build MergedParent from old parent
	mp := git.MergedParent{BranchName: oldParent}
	if oldParentMeta != nil && oldParentMeta.PrInfo != nil {
		mp.PRNumber = oldParentMeta.PrInfo.Number
		mp.PRState = oldParentMeta.PrInfo.State
	}

	// Inherit old parent's history (for multi-level: A→B→C, if B merges)
	var history []git.MergedParent
	if oldParentMeta != nil {
		history = append(history, oldParentMeta.MergedDownstack...)
	}

	// Check if oldParent already in history (prevent duplicates from retried operations)
	for _, existing := range history {
		if existing.BranchName == oldParent {
			// Already captured, skip adding duplicate
			meta.MergedDownstack = history
			if err := e.git.WriteMetadata(branchName, meta); err != nil {
				return err
			}
			if metaMap != nil {
				metaMap[branchName] = meta
			}
			return nil
		}
	}

	history = append(history, mp)

	// Limit to last 5 entries
	const maxHistoryEntries = 5
	if len(history) > maxHistoryEntries {
		history = history[len(history)-maxHistoryEntries:]
	}

	meta.MergedDownstack = history

	// Write and update cache
	if err := e.git.WriteMetadata(branchName, meta); err != nil {
		return err
	}
	if metaMap != nil {
		metaMap[branchName] = meta
	}
	return nil
}

// getMetaFromMapOrDisk retrieves metadata from the cache map or from disk if not found.
func (e *engineImpl) getMetaFromMapOrDisk(branchName string, metaMap map[string]*git.Meta) *git.Meta {
	if metaMap != nil {
		if meta, ok := metaMap[branchName]; ok {
			return meta
		}
	}
	meta, err := e.git.ReadMetadata(branchName)
	if err != nil {
		return nil
	}
	return meta
}

// Helper functions
func getStringValue(s *string) string {
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

func stringPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func boolPtr(b bool) *bool {
	return &b
}
