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
	allMeta, _ := e.batchReadMetadata(branches)
	allLocalMeta := e.batchReadLocalMetadata(branches)

	e.applyRebuild(branches, currentBranch, allMeta, allLocalMeta)
	return nil
}

// applyRebuild updates the internal state from the provided metadata results.
// The caller MUST hold the engine's write lock (e.mu).
func (e *engineImpl) applyRebuild(branches []string, currentBranch string, allMeta map[string]*git.Meta, allLocalMeta map[string]*git.LocalMeta) {
	e.state.rebuildFromMetadata(e.trunk, branches, allMeta, allLocalMeta)
	if currentBranch != "" {
		e.currentBranch = currentBranch
	}
}

// updateBranchInCache updates the cache for a specific branch after restack/metadata changes
func (e *engineImpl) updateBranchInCache(branchName string) {
	if branchName == e.trunk {
		return
	}

	// Read metadata for this branch
	meta, err := e.readMetadata(branchName)
	if err != nil {
		e.state.removeBranch(branchName)
		return
	}

	// Read local metadata too
	localMeta, _ := e.readLocalMetadata(branchName)
	e.state.updateBranchFromMetadata(branchName, meta, localMeta)
}

// rebuild loads all branches and their metadata from Git
func (e *engineImpl) rebuild() error {
	// Clear metadata cache to pick up external changes (e.g., branches created in another terminal)
	e.git.ClearMetadataCache()

	// 1. Get all branch names (slow)
	branches, err := e.git.GetAllBranchNames()
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
	parentExists := slices.Contains(e.state.branches, parentBranchName)
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
		if meta, ok := metaMap[parentBranchName]; ok && meta != nil {
			prInfo := meta.GetPrInfo()
			if prInfo != nil && prInfo.State != nil && *prInfo.State == "MERGED" {
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
	state := e.state.branchState.GetByName(branchName)
	if state == nil {
		return e.trunk
	}
	current := state.Parent

	for current != "" && current != e.trunk {
		if !e.shouldReparentBranch(ctx, current, metaMap) {
			return current
		}
		// Move to the next parent
		parentState := e.state.branchState.GetByName(current)
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
	meta, err := e.readMetadata(branchName)
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
			// Already captured, skip adding duplicate
			meta = meta.WithMergedDownstack(history)
			if err := e.writeMetadata(branchName, meta); err != nil {
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

	meta = meta.WithMergedDownstack(history)

	// Write and update cache
	if err := e.writeMetadata(branchName, meta); err != nil {
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
	meta, err := e.readMetadata(branchName)
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
