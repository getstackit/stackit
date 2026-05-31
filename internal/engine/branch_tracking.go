package engine

import (
	"context"
	"fmt"
	"slices"

	"github.com/getstackit/stackit/internal/git"
)

// TrackBranch tracks a branch with a parent branch
func (e *engineImpl) TrackBranch(ctx context.Context, branchName string, parentBranchName string) error {
	if branchName == e.trunk {
		return fmt.Errorf("cannot track trunk branch %s", e.trunk)
	}
	if branchName == parentBranchName {
		return fmt.Errorf("branch cannot be its own parent")
	}

	// Validate branches exist (under lock for consistent reads)
	e.mu.Lock()
	// Update current branch if it changed
	if current, err := e.git.GetCurrentBranch(); err == nil {
		e.currentBranch = current
	}

	// Validate branch exists
	branchExists := slices.Contains(e.state.branches, branchName)
	if !branchExists {
		// Refresh branches list
		branches, err := e.git.GetAllBranchNames(ctx)
		if err != nil {
			e.mu.Unlock()
			return fmt.Errorf("failed to get branches: %w", err)
		}
		e.state.setBranches(branches)
		branchExists = slices.Contains(e.state.branches, branchName)
		if !branchExists {
			e.mu.Unlock()
			return fmt.Errorf("branch %s does not exist", branchName)
		}
	}

	// Validate parent exists (or is trunk)
	if parentBranchName != e.trunk {
		parentExists := slices.Contains(e.state.branches, parentBranchName)
		if !parentExists {
			// Refresh branches list to check again
			branches, err := e.git.GetAllBranchNames(ctx)
			if err != nil {
				e.mu.Unlock()
				return fmt.Errorf("failed to get branches: %w", err)
			}
			e.state.setBranches(branches)
			parentExists = slices.Contains(e.state.branches, parentBranchName)
			if !parentExists {
				e.mu.Unlock()
				return fmt.Errorf("parent branch %s does not exist", parentBranchName)
			}
		}
	}
	e.mu.Unlock()

	// New branches start without prior divergence metadata, so recompute the
	// merge-base from scratch.
	return e.SetParent(ctx, e.GetBranch(branchName), e.GetBranch(parentBranchName), DivergenceRecompute)
}

// untrackBranch stops tracking a single branch by deleting its metadata and rebuilding the cache.
func (e *engineImpl) untrackBranch(ctx context.Context, branchName string) error {
	if err := e.git.DeleteMetadata(ctx, branchName); err != nil {
		return fmt.Errorf("failed to delete metadata ref: %w", err)
	}
	return e.rebuild()
}

// UntrackBranches removes tracking for multiple branches with a single metadata deletion
// and a single engine rebuild, rather than one rebuild per branch.
func (e *engineImpl) UntrackBranches(ctx context.Context, branchNames []string) error {
	if len(branchNames) == 0 {
		return nil
	}
	if len(branchNames) == 1 {
		return e.untrackBranch(ctx, branchNames[0])
	}

	refNames := make([]string, len(branchNames))
	for i, name := range branchNames {
		refNames[i] = fmt.Sprintf("%s%s", git.MetadataRefPrefix, name)
	}
	if err := e.git.DeleteRefsBatch(ctx, refNames); err != nil {
		return fmt.Errorf("failed to delete metadata refs: %w", err)
	}
	return e.rebuild()
}

// DivergenceMode controls how SetParent updates a branch's recorded divergence
// point (ParentBranchRevision) when its parent changes.
type DivergenceMode int

const (
	// DivergencePreserve keeps the existing divergence point if it's still a
	// valid ancestor of the branch. Use when the branch's own commits should
	// not change — e.g., reparenting a branch onto a sibling stack.
	DivergencePreserve DivergenceMode = iota

	// DivergenceRecompute computes a fresh merge-base against the new parent
	// (with a minor exception: if the old parent was merged into the new
	// parent, the existing revision is retained as a stable anchor). Use when
	// the branch has absorbed commits from a former parent (e.g. after a
	// fold/merge), so that subsequent restacks include those commits.
	DivergenceRecompute
)

// SetParent updates a branch's parent. The mode controls whether the existing
// divergence point is preserved or recomputed; see DivergenceMode for the
// trade-off. Retries on concurrent modification.
func (e *engineImpl) SetParent(ctx context.Context, branch Branch, parentBranch Branch, mode DivergenceMode) error {
	branchName := branch.GetName()
	parentBranchName := parentBranch.GetName()

	if branchName == parentBranchName {
		return fmt.Errorf("branch %s cannot be its own parent", branchName)
	}

	switch mode {
	case DivergencePreserve:
		div, err := e.GetDivergencePoint(branchName)
		if err != nil {
			return fmt.Errorf("failed to determine divergence point for %s: %w", branchName, err)
		}
		return e.setParentPreservingDivergence(ctx, branch, parentBranch, div)
	case DivergenceRecompute:
		return e.setParentRecomputingDivergence(ctx, branch, parentBranch)
	default:
		return fmt.Errorf("unknown DivergenceMode: %d", mode)
	}
}

// setParentRecomputingDivergence updates a branch's parent and refreshes the
// divergence point by computing a new merge-base. Retries on concurrent
// modification.
func (e *engineImpl) setParentRecomputingDivergence(ctx context.Context, branch Branch, parentBranch Branch) error {
	branchName := branch.GetName()
	parentBranchName := parentBranch.GetName()

	return e.WithRetry(ctx, func() error {
		// Get new parent revision (may run multiple times on retry)
		parentRev, err := e.git.GetMergeBase(ctx, branchName, parentBranchName)
		if err != nil {
			return fmt.Errorf("failed to get merge base: %w", err)
		}

		// Read existing metadata
		meta, err := e.readMetadata(branchName)
		if err != nil {
			return fmt.Errorf("failed to read metadata: %w", err)
		}

		// Get old parent
		oldParent := ""
		if meta.GetParentBranchName() != nil {
			oldParent = *meta.GetParentBranchName()
		}

		// Only update ParentBranchRevision if it's currently nil, invalid, or if we're not
		// in a "parent merged into trunk" situation.
		shouldUpdateRevision := true
		if oldParent != "" && oldParent != parentBranchName && meta.GetParentBranchRevision() != nil && *meta.GetParentBranchRevision() != "" {
			// Check if existing revision is still a valid ancestor of the branch
			if isAncestor, _ := e.git.IsAncestor(ctx, *meta.GetParentBranchRevision(), branchName); isAncestor {
				// Check if the old parent was merged into the new parent (the "merge" case)
				if merged, _ := e.git.IsMerged(ctx, oldParent, parentBranchName); merged {
					shouldUpdateRevision = false
				}
			}
		}

		meta = meta.WithParentBranchName(&parentBranchName)
		if shouldUpdateRevision {
			meta = meta.WithParentBranchRevision(&parentRev)
		}

		// Use transaction for atomic update (Commit handles in-memory cache updates)
		tx := e.BeginTx(fmt.Sprintf("set parent: %s -> %s", branchName, parentBranchName))
		if err := tx.UpdateMeta(branchName, meta); err != nil {
			return err
		}
		return tx.Commit(ctx)
	})
}

// setParentName updates only the parent branch name in metadata without
// modifying ParentBranchRevision.
func (e *engineImpl) setParentName(ctx context.Context, branch Branch, parentBranch Branch) error {
	branchName := branch.GetName()
	parentBranchName := parentBranch.GetName()

	if branchName == parentBranchName {
		return fmt.Errorf("branch %s cannot be its own parent", branchName)
	}

	return e.WithRetry(ctx, func() error {
		meta, err := e.readMetadata(branchName)
		if err != nil {
			return fmt.Errorf("failed to read metadata: %w", err)
		}

		meta = meta.WithParentBranchName(&parentBranchName)

		tx := e.BeginTx(fmt.Sprintf("set parent name: %s -> %s", branchName, parentBranchName))
		if err := tx.UpdateMeta(branchName, meta); err != nil {
			return err
		}
		return tx.Commit(ctx)
	})
}

// setParentPreservingDivergence updates a branch's parent while preserving
// the divergence point if it remains a valid ancestor. Uses setParentName
// (not SetParent) so the existing ParentBranchRevision is never overwritten
// with an incorrect merge-base value.
func (e *engineImpl) setParentPreservingDivergence(ctx context.Context, branch Branch, newParent Branch, oldDivergencePoint string) error {
	if err := e.setParentName(ctx, branch, newParent); err != nil {
		return err
	}

	if oldDivergencePoint == "" {
		return nil
	}

	// Set the correct divergence point so restacking replays only this
	// branch's commits, not commits from the old parent.
	isAncestor, err := e.git.IsAncestor(ctx, oldDivergencePoint, branch.GetName())
	if err != nil {
		return fmt.Errorf("failed to check ancestry of divergence point %s for %s: %w", oldDivergencePoint, branch.GetName(), err)
	}
	if !isAncestor {
		return fmt.Errorf("divergence point %s is not an ancestor of %s: cannot preserve divergence point", oldDivergencePoint, branch.GetName())
	}

	return e.updateParentRevision(ctx, branch.GetName(), oldDivergencePoint)
}

// ReparentBranch changes a branch's parent while automatically preserving its
// divergence point. This is the preferred way to reparent an existing branch
// when the branch's own commits should not change.
//
// Automatically propagates the new parent's stack ID to the reparented branch
// when the move crosses a stack boundary; same-stack reparenting is a no-op
// for stack ID.
func (e *engineImpl) ReparentBranch(ctx context.Context, branch Branch, newParent Branch) error {
	div, err := e.GetDivergencePoint(branch.GetName())
	if err != nil {
		return fmt.Errorf("failed to determine divergence point for %s: %w", branch.GetName(), err)
	}
	if err := e.setParentPreservingDivergence(ctx, branch, newParent, div); err != nil {
		return err
	}
	// Fetch the post-reparent branch so syncStackIDFromParent sees the new parent.
	return e.syncStackIDFromParent(ctx, e.GetBranch(branch.GetName()))
}

// ReparentBranches changes multiple branches to the same new parent while
// preserving each branch's divergence point. Divergence points are captured
// for all branches before any reparenting begins, ensuring correctness when
// branches in the list are related to each other.
//
// Automatically propagates the new parent's stack ID to each branch when the
// move crosses a stack boundary.
func (e *engineImpl) ReparentBranches(ctx context.Context, branchNames []string, newParent Branch) error {
	divPoints := make(map[string]string, len(branchNames))
	for _, name := range branchNames {
		div, err := e.GetDivergencePoint(name)
		if err != nil {
			return fmt.Errorf("failed to determine divergence point for %s: %w", name, err)
		}
		divPoints[name] = div
	}

	for _, name := range branchNames {
		if err := e.setParentPreservingDivergence(ctx, e.GetBranch(name), newParent, divPoints[name]); err != nil {
			return fmt.Errorf("failed to reparent %s to %s: %w", name, newParent.GetName(), err)
		}
		if err := e.syncStackIDFromParent(ctx, e.GetBranch(name)); err != nil {
			return fmt.Errorf("failed to sync stack ID for %s: %w", name, err)
		}
	}
	return nil
}

// updateParentRevision updates the parent revision in metadata using transaction API
// with retry logic for concurrent modification resilience.
func (e *engineImpl) updateParentRevision(ctx context.Context, branchName string, parentRev string) error {
	return e.WithRetry(ctx, func() error {
		// Read existing metadata (outside lock for performance)
		meta, err := e.readMetadata(branchName)
		if err != nil {
			return fmt.Errorf("failed to read metadata: %w", err)
		}

		meta = meta.WithParentBranchRevision(&parentRev)

		// Use transaction for atomic update
		tx := e.BeginTx(fmt.Sprintf("update parent revision: %s", branchName))
		if err := tx.UpdateMeta(branchName, meta); err != nil {
			return err
		}
		return tx.Commit(ctx)
	})
}

// SetScope updates a branch's scope with retry logic for concurrent modification resilience.
func (e *engineImpl) SetScope(ctx context.Context, branch Branch, scope Scope) error {
	branchName := branch.GetName()

	return e.WithRetry(ctx, func() error {
		// Read existing metadata (outside lock for performance)
		meta, err := e.readMetadata(branchName)
		if err != nil {
			return fmt.Errorf("failed to read metadata: %w", err)
		}

		// Update scope
		if scope.IsEmpty() {
			meta = meta.WithScope(nil)
		} else {
			scopeStr := scope.String()
			meta = meta.WithScope(&scopeStr)
		}

		// Use transaction for atomic update
		tx := e.BeginTx(fmt.Sprintf("set scope: %s", branchName))
		if err := tx.UpdateMeta(branchName, meta); err != nil {
			return err
		}
		return tx.Commit(ctx)
	})
}

// SetScopeAndMarkForUpdate sets the scope and marks the branch as needing a PR
// body update in a single atomic transaction, saving one git ref write compared
// to calling SetScope + BatchMarkNeedsPRBodyUpdate separately.
func (e *engineImpl) SetScopeAndMarkForUpdate(ctx context.Context, branch Branch, scope Scope) error {
	branchName := branch.GetName()

	return e.WithRetry(ctx, func() error {
		meta, err := e.readMetadata(branchName)
		if err != nil {
			return fmt.Errorf("failed to read metadata: %w", err)
		}

		if scope.IsEmpty() {
			meta = meta.WithScope(nil)
		} else {
			scopeStr := scope.String()
			meta = meta.WithScope(&scopeStr)
		}

		localMeta, _ := e.readLocalMetadata(branchName)
		if localMeta == nil {
			localMeta = &git.LocalMeta{}
		}
		localMeta.NeedsPRBodyUpdate = true

		tx := e.BeginTx(fmt.Sprintf("set scope+mark: %s", branchName))
		if err := tx.UpdateMeta(branchName, meta); err != nil {
			return err
		}
		if err := tx.UpdateLocalMeta(branchName, localMeta); err != nil {
			return err
		}
		return tx.Commit(ctx)
	})
}

// SetLocked updates multiple branches' locked status atomically using transactions.
// It retries on concurrent modification errors with exponential backoff.
func (e *engineImpl) SetLocked(ctx context.Context, branches Branches, reason LockReason) (BatchLockResult, error) {
	result := BatchLockResult{
		AffectedBranches: make([]string, 0, len(branches)),
		Errors:           make(map[string]error),
	}

	if len(branches) == 0 {
		return result, nil
	}

	// Extract branch names for batch read (preserves order for deterministic results)
	branchNames := make([]string, len(branches))
	for i, b := range branches {
		branchNames[i] = b.GetName()
	}

	err := e.WithRetry(ctx, func() error {
		// Reset result for retry
		result.AffectedBranches = result.AffectedBranches[:0]
		result.Errors = make(map[string]error)

		// Batch read all metadata first (parallel, outside any lock)
		metas, readErrs := e.batchReadMetadata(branchNames)

		// Collect read errors
		for name, readErr := range readErrs {
			result.Errors[name] = fmt.Errorf("failed to read metadata: %w", readErr)
		}

		// If all reads failed, return early
		if len(metas) == 0 {
			return fmt.Errorf("failed to read metadata for any branches")
		}

		// Create transaction for atomic update
		tx := e.BeginTx(fmt.Sprintf("lock: set %s on %d branches", reason, len(metas)))

		// Stage all updates - iterate over branchNames for deterministic order
		for _, name := range branchNames {
			meta, ok := metas[name]
			if !ok {
				continue // Skip branches that had read errors
			}
			if meta == nil {
				meta = git.NewMeta()
			}
			meta = meta.WithLockReason(reason)
			if stageErr := tx.UpdateMeta(name, meta); stageErr != nil {
				result.Errors[name] = fmt.Errorf("failed to stage update: %w", stageErr)
			}
		}

		// Commit atomically
		if commitErr := tx.Commit(ctx); commitErr != nil {
			// Transaction failed - all updates rolled back
			for _, name := range branchNames {
				if _, hasErr := result.Errors[name]; !hasErr {
					if _, hasMeta := metas[name]; hasMeta {
						result.Errors[name] = fmt.Errorf("transaction commit failed: %w", commitErr)
					}
				}
			}
			return fmt.Errorf("failed to commit lock changes: %w", commitErr)
		}

		// All staged updates succeeded - iterate over branchNames for deterministic order
		for _, name := range branchNames {
			if _, hasErr := result.Errors[name]; !hasErr {
				if _, hasMeta := metas[name]; hasMeta {
					result.AffectedBranches = append(result.AffectedBranches, name)
				}
			}
		}

		if len(result.Errors) > 0 {
			return fmt.Errorf("failed to update locked status for some branches")
		}

		return nil
	})

	return result, err
}

// SetFrozen updates multiple branches' frozen status atomically using transactions.
// It retries on concurrent modification errors with exponential backoff.
func (e *engineImpl) SetFrozen(ctx context.Context, branches Branches, frozen bool) (BatchFreezeResult, error) {
	result := BatchFreezeResult{
		AffectedBranches: make([]string, 0, len(branches)),
		Errors:           make(map[string]error),
	}

	if len(branches) == 0 {
		return result, nil
	}

	// Extract branch names for batch read (preserves order for deterministic results)
	branchNames := make([]string, len(branches))
	for i, b := range branches {
		branchNames[i] = b.GetName()
	}

	err := e.WithRetry(ctx, func() error {
		// Reset result for retry
		result.AffectedBranches = result.AffectedBranches[:0]
		result.Errors = make(map[string]error)

		// Batch read all local metadata first (parallel, outside any lock)
		metas := e.batchReadLocalMetadata(branchNames)

		// Create transaction for atomic update
		tx := e.BeginTx(fmt.Sprintf("freeze: set frozen=%t on %d branches", frozen, len(branches)))

		// Stage all updates
		for _, name := range branchNames {
			meta := metas[name]
			if meta == nil {
				meta = &git.LocalMeta{}
			}
			meta.Frozen = frozen
			if stageErr := tx.UpdateLocalMeta(name, meta); stageErr != nil {
				result.Errors[name] = fmt.Errorf("failed to stage update: %w", stageErr)
			}
		}

		// Commit atomically
		if commitErr := tx.Commit(ctx); commitErr != nil {
			// Transaction failed - all updates rolled back
			for _, name := range branchNames {
				if _, hasErr := result.Errors[name]; !hasErr {
					result.Errors[name] = fmt.Errorf("transaction commit failed: %w", commitErr)
				}
			}
			return fmt.Errorf("failed to commit freeze changes: %w", commitErr)
		}

		// All staged updates succeeded
		for _, name := range branchNames {
			if _, hasErr := result.Errors[name]; !hasErr {
				result.AffectedBranches = append(result.AffectedBranches, name)
			}
		}

		if len(result.Errors) > 0 {
			return fmt.Errorf("failed to update frozen status for some branches")
		}

		return nil
	})

	return result, err
}
