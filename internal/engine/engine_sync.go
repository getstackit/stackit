package engine

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/getstackit/stackit/internal/git"
)

// PullTrunk pulls the trunk branch from remote
func (e *engineImpl) PullTrunk(ctx context.Context) (PullResult, error) {
	remote := e.git.GetRemote()
	e.mu.RLock()
	trunk := e.trunk
	e.mu.RUnlock()
	gitResult, err := e.git.PullBranch(ctx, remote, trunk)
	if err != nil {
		return PullConflict, err
	}

	// Convert git.PullResult to engine.PullResult
	var result PullResult
	switch gitResult {
	case git.PullDone:
		result = PullDone
	case git.PullUnneeded:
		result = PullUnneeded
	case git.PullConflict:
		result = PullConflict
	default:
		result = PullConflict
	}

	// Rebuild to refresh branch cache
	if err := e.rebuild(); err != nil {
		return result, fmt.Errorf("failed to rebuild after pull: %w", err)
	}

	return result, nil
}

// ResetTrunkToRemote resets trunk to match remote
func (e *engineImpl) ResetTrunkToRemote(ctx context.Context) error {
	remote := e.git.GetRemote()

	e.mu.RLock()
	trunk := e.trunk
	currentBranch := e.currentBranch
	e.mu.RUnlock()

	// Get remote SHA
	remoteSha, err := e.git.GetRemoteSha(remote, trunk)
	if err != nil {
		// Fallback: try to get it from ls-remote if the tracking branch is missing
		remoteShas, fetchErr := e.git.FetchRemoteShas(ctx, remote)
		if fetchErr == nil {
			if sha, ok := remoteShas[trunk]; ok {
				remoteSha = sha
				err = nil
			}
		}
	}

	if err != nil {
		return fmt.Errorf("failed to get remote SHA: %w", err)
	}

	// Checkout trunk
	trunkBranch := e.Trunk()
	if err := e.CheckoutBranch(ctx, trunkBranch); err != nil {
		return fmt.Errorf("failed to checkout trunk: %w", err)
	}

	// Hard reset to remote
	if err := e.git.HardReset(ctx, remoteSha); err != nil {
		// Try to switch back
		if currentBranch != "" {
			currentBranchObj := e.GetBranch(currentBranch)
			_ = e.CheckoutBranch(ctx, currentBranchObj)
		}
		return fmt.Errorf("failed to reset trunk: %w", err)
	}

	// Switch back to original branch
	if currentBranch != "" && currentBranch != trunk {
		currentBranchObj := e.GetBranch(currentBranch)
		if err := e.CheckoutBranch(ctx, currentBranchObj); err != nil {
			return fmt.Errorf("failed to switch back: %w", err)
		}
	}

	// Rebuild to refresh branch cache
	if err := e.rebuild(); err != nil {
		return fmt.Errorf("failed to rebuild after reset: %w", err)
	}

	return nil
}

// restackBranch rebases a branch onto its parent
// If the parent has been merged/deleted, it will automatically reparent to the nearest valid ancestor
func (e *engineImpl) restackBranch(
	ctx context.Context,
	branch Branch,
	metaMap map[string]*git.Meta,
	revMap map[string]string,
) (RestackBranchResult, error) {
	branchName := branch.GetName()
	if e.IsTrunk(branch) {
		return RestackBranchResult{Result: RestackUnneeded}, nil
	}

	e.mu.RLock()
	state := e.readState(branchName)
	e.mu.RUnlock()

	var parent string
	tracked := state != nil
	if tracked {
		parent = state.Parent
	} else {
		// RESILIENCY: Try to auto-discover parent if branch is not tracked
		ancestors, err := e.FindMostRecentTrackedAncestors(ctx, branchName)
		if err == nil && len(ancestors) > 0 {
			parent = ancestors[0]
			// Auto-track the branch
			if err := e.TrackBranch(ctx, branchName, parent); err == nil {
				tracked = true
			}
		}

		if !tracked {
			return RestackBranchResult{Result: RestackUnneeded}, fmt.Errorf("branch %s is not tracked", branchName)
		}
	}

	lockReason := e.GetLockReason(branch)
	if lockReason.IsLocked() && lockReason != LockReasonDraining {
		return RestackBranchResult{Result: RestackUnneeded, LockReason: lockReason}, nil
	}

	if e.IsFrozen(branch) {
		// For frozen branches, we update via hard reset to remote instead of rebase
		remoteSha, err := e.git.GetRemoteRevision(branchName)
		if err != nil {
			// If remote branch is not found, just skip restack
			return RestackBranchResult{Result: RestackUnneeded, Frozen: true}, nil //nolint:nilerr
		}
		if remoteSha == "" {
			return RestackBranchResult{Result: RestackUnneeded, Frozen: true}, nil
		}

		localSha, err := branch.GetRevision()
		if err != nil {
			return RestackBranchResult{Result: RestackConflict}, fmt.Errorf("failed to get local revision for frozen branch %s: %w", branchName, err)
		}
		if localSha != remoteSha {
			// Get parent revision for metadata update
			parentBranch := e.GetBranch(parent)
			parentRev, err := parentBranch.GetRevision()
			if err != nil {
				return RestackBranchResult{Result: RestackConflict}, fmt.Errorf("failed to get parent revision for %s: %w", branchName, err)
			}

			// Get current metadata SHA for optimistic locking
			oldMetadataSHA, _ := e.git.GetRef(fmt.Sprintf("%s%s", git.MetadataRefPrefix, branchName))

			// Prepare metadata update
			meta, err := e.readMetadata(branchName)
			if err != nil {
				meta = git.NewMeta()
			}
			if parentRev != "" {
				meta = meta.WithParentBranchRevision(&parentRev)
			}
			metadataJSON, err := json.Marshal(meta)
			if err != nil {
				return RestackBranchResult{Result: RestackConflict}, fmt.Errorf("failed to marshal metadata for frozen branch %s: %w", branchName, err)
			}
			metadataSHA, err := e.git.CreateBlob(string(metadataJSON))
			if err != nil {
				return RestackBranchResult{Result: RestackConflict}, fmt.Errorf("failed to prepare metadata blob for frozen branch %s: %w", branchName, err)
			}

			// Atomic update of both branch ref and metadata ref
			updates := []git.RefUpdate{
				{RefName: fmt.Sprintf("refs/heads/%s", branchName), NewSHA: remoteSha, OldSHA: localSha},
				{RefName: fmt.Sprintf("%s%s", git.MetadataRefPrefix, branchName), NewSHA: metadataSHA, OldSHA: oldMetadataSHA},
			}
			if err := e.git.UpdateRefsBatch(ctx, updates); err != nil {
				return RestackBranchResult{Result: RestackConflict}, fmt.Errorf("failed to update refs atomically for frozen branch %s: %w", branchName, err)
			}

			// If the branch is currently checked out in this context, reset the working tree
			current := e.CurrentBranch()
			if current != nil && current.GetName() == branchName {
				if err := e.git.HardReset(ctx, "HEAD"); err != nil {
					return RestackBranchResult{Result: RestackConflict}, fmt.Errorf("failed to reset working tree for frozen branch %s: %w", branchName, err)
				}
			} else {
				// If the branch is checked out in a different worktree, reset that worktree.
				// This is best-effort: sync checks for uncommitted changes before proceeding,
				// so failure here just means the worktree may be briefly out of sync with HEAD.
				// The ResetWorktreeWorkingDir command itself is logged via debugLog.
				if worktreePath, wtErr := e.git.GetWorktreePathForBranch(ctx, branchName); wtErr == nil && worktreePath != "" {
					_ = e.git.ResetWorktreeWorkingDir(ctx, worktreePath) //nolint:errcheck // best-effort
				}
			}

			return RestackBranchResult{
				Result:            RestackDone,
				RebasedBranchBase: remoteSha,
			}, nil
		}

		return RestackBranchResult{Result: RestackUnneeded, Frozen: true}, nil
	}

	// Handle worktree anchor branches: fast-forward to trunk instead of rebase
	if e.IsWorktreeAnchor(branch) {
		// Get trunk's current SHA
		trunkRev, err := e.Trunk().GetRevision()
		if err != nil {
			return RestackBranchResult{Result: RestackConflict}, fmt.Errorf("failed to get trunk revision for anchor %s: %w", branchName, err)
		}

		// Get anchor's current SHA
		anchorRev, err := branch.GetRevision()
		if err != nil {
			return RestackBranchResult{Result: RestackConflict}, fmt.Errorf("failed to get anchor revision for %s: %w", branchName, err)
		}

		// If already at trunk, nothing to do
		if anchorRev == trunkRev {
			return RestackBranchResult{Result: RestackUnneeded}, nil
		}

		// Get current metadata SHA for optimistic locking
		oldMetadataSHA, _ := e.git.GetRef(fmt.Sprintf("%s%s", git.MetadataRefPrefix, branchName))

		// Prepare metadata update with new parent revision
		meta, err := e.readMetadata(branchName)
		if err != nil {
			meta = git.NewMeta()
		}
		meta = meta.WithParentBranchRevision(&trunkRev)
		metadataJSON, err := json.Marshal(meta)
		if err != nil {
			return RestackBranchResult{Result: RestackConflict}, fmt.Errorf("failed to marshal metadata for anchor %s: %w", branchName, err)
		}
		metadataSHA, err := e.git.CreateBlob(string(metadataJSON))
		if err != nil {
			return RestackBranchResult{Result: RestackConflict}, fmt.Errorf("failed to prepare metadata blob for anchor %s: %w", branchName, err)
		}

		// Atomic update of both branch ref and metadata ref
		updates := []git.RefUpdate{
			{RefName: fmt.Sprintf("refs/heads/%s", branchName), NewSHA: trunkRev, OldSHA: anchorRev},
			{RefName: fmt.Sprintf("%s%s", git.MetadataRefPrefix, branchName), NewSHA: metadataSHA, OldSHA: oldMetadataSHA},
		}
		if err := e.git.UpdateRefsBatch(ctx, updates); err != nil {
			return RestackBranchResult{Result: RestackConflict}, fmt.Errorf("failed to update refs atomically for anchor %s: %w", branchName, err)
		}

		// If the anchor branch is currently checked out in this context, reset the working tree
		current := e.CurrentBranch()
		if current != nil && current.GetName() == branchName {
			if err := e.git.HardReset(ctx, "HEAD"); err != nil {
				return RestackBranchResult{Result: RestackConflict}, fmt.Errorf("failed to reset working tree for anchor %s: %w", branchName, err)
			}
		} else {
			// If the branch is checked out in a different worktree, reset that worktree.
			// This is best-effort: sync checks for uncommitted changes before proceeding,
			// so failure here just means the worktree may be briefly out of sync with HEAD.
			if worktreePath, wtErr := e.git.GetWorktreePathForBranch(ctx, branchName); wtErr == nil && worktreePath != "" {
				_ = e.git.ResetWorktreeWorkingDir(ctx, worktreePath) //nolint:errcheck // best-effort
			}
		}

		return RestackBranchResult{
			Result:            RestackDone,
			RebasedBranchBase: trunkRev,
		}, nil
	}

	// Track reparenting info
	var reparented bool
	var oldParent string

	// Check if parent needs reparenting (merged, deleted, or has MERGED PR state)
	e.mu.RLock()
	needsReparent := e.shouldReparentBranch(ctx, parent, metaMap)
	e.mu.RUnlock()

	if needsReparent {
		oldParent = parent

		// Find nearest valid ancestor
		e.mu.RLock()
		newParent := e.findNearestValidAncestor(ctx, branchName, metaMap)
		e.mu.RUnlock()

		// Reparent to the nearest valid ancestor. Using SetParent (not
		// SetParentPreservingDivergence) is intentional here: the old parent
		// was merged/deleted, so SetParent's shouldUpdateRevision logic
		// correctly preserves the existing divergence point when the old
		// parent was merged into the new parent. The subsequent restack then
		// rebases the branch's own commits onto the new parent.
		if err := e.SetParent(ctx, e.GetBranch(branchName), e.GetBranch(newParent)); err != nil {
			return RestackBranchResult{Result: RestackConflict}, fmt.Errorf("failed to reparent %s to %s: %w", branchName, newParent, err)
		}
		parent = newParent
		reparented = true

		// Capture the old parent in merged history (best-effort, don't fail on error).
		// appendMergedDownstack reads fresh from disk and updates metaMap.
		_ = e.appendMergedDownstack(branchName, oldParent, metaMap)
	}

	// Get parent revision (needed for rebasedBranchBase even if restack is unneeded)
	parentBranch := e.GetBranch(parent)
	var parentRev string
	var err error
	if revMap != nil {
		parentRev = revMap[parent]
	}
	if parentRev == "" {
		parentRev, err = parentBranch.GetRevision()
		if err != nil {
			return RestackBranchResult{Result: RestackConflict, RebasedBranchBase: parentRev}, fmt.Errorf("failed to get parent revision: %w", err)
		}
	}

	// Get metadata (read once to avoid duplicate disk I/O)
	var meta *git.Meta
	if metaMap != nil {
		meta = metaMap[branchName]
	}
	if meta == nil {
		meta, err = e.readMetadata(branchName)
		if err != nil {
			return RestackBranchResult{Result: RestackConflict, RebasedBranchBase: parentRev}, fmt.Errorf("failed to read metadata: %w", err)
		}
	}

	// Get oldParentRev from metadata (or use parentRev as default)
	oldParentRev := parentRev
	if meta.GetParentBranchRevision() != nil {
		oldParentRev = *meta.GetParentBranchRevision()
	}

	// RESILIENCY: If oldParentRev is no longer an ancestor of branchName,
	// or if it's empty, find the actual merge base. This handles cases where
	// the parent was amended or rebased outside of stackit, or where metadata
	// was set to a revision that was never actually an ancestor.
	// This check MUST run before the early-exit checks to match buildRebaseSpecs behavior.
	if oldParentRev != "" {
		if isAncestor, _ := e.git.IsAncestor(oldParentRev, branchName); !isAncestor {
			if mergeBase, err := e.git.GetMergeBase(branchName, parent); err == nil {
				oldParentRev = mergeBase
			}
		}
	} else {
		// No old parent revision in metadata, try to find merge base
		if mergeBase, err := e.git.GetMergeBase(branchName, parent); err == nil {
			oldParentRev = mergeBase
		}
	}

	// Check if branch needs restacking - parent must have changed
	if parentRev == oldParentRev {
		return RestackBranchResult{
			Result:            RestackUnneeded,
			RebasedBranchBase: parentRev,
			Reparented:        reparented,
			OldParent:         oldParent,
			NewParent:         parent,
		}, nil
	}

	// Perform rebase
	gitResult, err := e.git.Rebase(ctx, branchName, parent, oldParentRev)
	if err != nil {
		return RestackBranchResult{
			Result:              RestackConflict,
			RebasedBranchBase:   parentRev,
			Reparented:          reparented,
			OldParent:           oldParent,
			NewParent:           parent,
			RerereResolvedCount: gitResult.RerereResolvedCount,
		}, fmt.Errorf("rebase failed for %s onto %s (old base %s): %w", branchName, parent, oldParentRev, err)
	}

	if gitResult.Result == git.RebaseConflict {
		return RestackBranchResult{
			Result:              RestackConflict,
			RebasedBranchBase:   parentRev,
			Reparented:          reparented,
			OldParent:           oldParent,
			NewParent:           parent,
			RerereResolvedCount: gitResult.RerereResolvedCount,
		}, nil
	}

	// Get the new rebased SHA
	newRev, err := e.git.GetCurrentRevision(ctx)
	if err != nil {
		return RestackBranchResult{
			Result:            RestackConflict,
			RebasedBranchBase: parentRev,
			Reparented:        reparented,
			OldParent:         oldParent,
			NewParent:         parent,
		}, fmt.Errorf("failed to get new revision after rebase: %w", err)
	}

	// Get current SHAs for optimistic locking
	oldBranchSHA, _ := branch.GetRevision()
	oldMetadataSHA, _ := e.git.GetRef(fmt.Sprintf("%s%s", git.MetadataRefPrefix, branchName))

	// Prepare metadata update
	meta, metaReadErr := e.readMetadata(branchName)
	if metaReadErr != nil {
		meta = git.NewMeta()
	}
	meta = meta.WithParentBranchRevision(&parentRev)
	metadataJSON, err := json.Marshal(meta)
	if err != nil {
		return RestackBranchResult{
			Result:            RestackConflict,
			RebasedBranchBase: parentRev,
			Reparented:        reparented,
			OldParent:         oldParent,
			NewParent:         parent,
		}, fmt.Errorf("failed to marshal metadata: %w", err)
	}
	// If this branch is checked out in a worktree, reset that worktree's working directory
	// to match the new branch content. Without this, the worktree would have stale content
	// that appears as staged changes reverting the rebased commits.
	// This is best-effort: sync checks for uncommitted changes before proceeding,
	// so failure here just means the worktree may be briefly out of sync with HEAD.
	if worktreePath, wtErr := e.git.GetWorktreePathForBranch(ctx, branchName); wtErr == nil && worktreePath != "" {
		_ = e.git.ResetWorktreeWorkingDir(ctx, worktreePath) //nolint:errcheck // best-effort
	}

	metadataSHA, err := e.git.CreateBlob(string(metadataJSON))
	if err != nil {
		return RestackBranchResult{
			Result:            RestackConflict,
			RebasedBranchBase: parentRev,
			Reparented:        reparented,
			OldParent:         oldParent,
			NewParent:         parent,
		}, fmt.Errorf("failed to prepare metadata blob: %w", err)
	}

	// Atomic update of both branch ref and metadata ref with OldSHA verification
	updates := []git.RefUpdate{
		{RefName: fmt.Sprintf("refs/heads/%s", branchName), NewSHA: newRev, OldSHA: oldBranchSHA},
		{RefName: fmt.Sprintf("%s%s", git.MetadataRefPrefix, branchName), NewSHA: metadataSHA, OldSHA: oldMetadataSHA},
	}
	if err := e.git.UpdateRefsBatch(ctx, updates); err != nil {
		return RestackBranchResult{
			Result:            RestackConflict,
			RebasedBranchBase: parentRev,
			Reparented:        reparented,
			OldParent:         oldParent,
			NewParent:         parent,
		}, fmt.Errorf("failed to update refs atomically: %w", err)
	}

	// Update the cached metadata if we're using a metaMap, so subsequent branches in the batch
	// see the updated ParentBranchRevision.
	if metaMap != nil {
		if updatedMeta, err := e.readMetadata(branchName); err == nil {
			metaMap[branchName] = updatedMeta
		}
	}

	return RestackBranchResult{
		Result:              RestackDone,
		RebasedBranchBase:   parentRev,
		Reparented:          reparented,
		OldParent:           oldParent,
		NewParent:           parent,
		RerereResolvedCount: gitResult.RerereResolvedCount,
	}, nil
}

func (e *engineImpl) restackBranchWithValidatedRebase(
	ctx context.Context,
	branch Branch,
	validation *RebaseValidation,
	plan *RestackPlan,
	metaMap map[string]*git.Meta,
	revMap map[string]string,
) (RestackBranchResult, error) {
	branchName := branch.GetName()
	if e.IsTrunk(branch) {
		return RestackBranchResult{Result: RestackUnneeded}, nil
	}

	item, ok := RestackPlanItem{}, false
	if plan != nil && plan.Items != nil {
		item, ok = plan.Items[branchName]
	}
	if !ok {
		return RestackBranchResult{Result: RestackConflict}, fmt.Errorf("missing restack plan item for %s", branchName)
	}
	if item.Skip {
		return item.SkipResult, nil
	}

	switch item.Action {
	case RestackPlanApplyFrozen, RestackPlanApplyAnchor:
		return e.applyPlannedRefUpdate(ctx, branch, item, metaMap, revMap)
	case RestackPlanApplyValidated:
		return e.applyValidatedRestack(ctx, branch, validation, item, metaMap, revMap)
	default:
		return RestackBranchResult{Result: RestackConflict}, fmt.Errorf("unknown restack plan action for %s", branchName)
	}
}

func (e *engineImpl) applyValidatedRestack(
	ctx context.Context,
	branch Branch,
	validation *RebaseValidation,
	item RestackPlanItem,
	metaMap map[string]*git.Meta,
	revMap map[string]string,
) (RestackBranchResult, error) {
	branchName := branch.GetName()
	newRev := ""
	if validation != nil && validation.NewSHAs != nil {
		newRev = validation.NewSHAs[branchName]
	}
	if newRev == "" {
		return RestackBranchResult{Result: RestackConflict}, fmt.Errorf("missing validated SHA for %s", branchName)
	}

	parentRev := ""
	if validation != nil && validation.NewSHAs != nil {
		parentRev = validation.NewSHAs[item.NewParent]
	}
	if parentRev == "" && revMap != nil {
		parentRev = revMap[item.NewParent]
	}
	if parentRev == "" {
		parentRev = item.ParentRev
	}
	if parentRev == "" {
		rev, err := e.GetBranch(item.NewParent).GetRevision()
		if err != nil {
			return RestackBranchResult{Result: RestackConflict}, fmt.Errorf("failed to get parent revision: %w", err)
		}
		parentRev = rev
	}

	result, err := e.applyBranchAndMetadata(ctx, branch, item, newRev, parentRev, metaMap, revMap)
	if err != nil {
		return result, err
	}

	if validation != nil && validation.RerereResolved != nil {
		result.RerereResolvedCount = validation.RerereResolved[branchName]
	}
	return result, nil
}

func (e *engineImpl) applyPlannedRefUpdate(
	ctx context.Context,
	branch Branch,
	item RestackPlanItem,
	metaMap map[string]*git.Meta,
	revMap map[string]string,
) (RestackBranchResult, error) {
	if item.TargetRev == "" {
		return RestackBranchResult{Result: RestackConflict}, fmt.Errorf("missing target revision for %s", item.Branch)
	}

	parentRev := item.ParentRev
	if parentRev == "" {
		rev, err := e.GetBranch(item.NewParent).GetRevision()
		if err != nil {
			return RestackBranchResult{Result: RestackConflict}, fmt.Errorf("failed to get parent revision: %w", err)
		}
		parentRev = rev
	}

	return e.applyBranchAndMetadata(ctx, branch, item, item.TargetRev, parentRev, metaMap, revMap)
}

func (e *engineImpl) applyBranchAndMetadata(
	ctx context.Context,
	branch Branch,
	item RestackPlanItem,
	newRev string,
	parentRev string,
	metaMap map[string]*git.Meta,
	revMap map[string]string,
) (RestackBranchResult, error) {
	branchName := branch.GetName()
	meta := (*git.Meta)(nil)
	if metaMap != nil {
		meta = metaMap[branchName]
	}
	if meta == nil {
		var err error
		meta, err = e.readMetadata(branchName)
		if err != nil {
			return RestackBranchResult{Result: RestackConflict, RebasedBranchBase: parentRev}, fmt.Errorf("failed to read metadata: %w", err)
		}
	}

	oldBranchSHA, _ := branch.GetRevision()
	oldMetadataSHA, _ := e.git.GetRef(fmt.Sprintf("%s%s", git.MetadataRefPrefix, branchName))

	updatedMeta := meta.WithParentBranchRevision(&parentRev)
	if item.Reparented {
		updatedMeta = updatedMeta.WithParentBranchName(&item.NewParent)
		updatedMeta = e.withMergedDownstack(updatedMeta, item.OldParent, metaMap)
	}
	metadataJSON, err := json.Marshal(updatedMeta)
	if err != nil {
		return RestackBranchResult{Result: RestackConflict, RebasedBranchBase: parentRev}, fmt.Errorf("failed to marshal metadata: %w", err)
	}

	metadataSHA, err := e.git.CreateBlob(string(metadataJSON))
	if err != nil {
		return RestackBranchResult{Result: RestackConflict, RebasedBranchBase: parentRev}, fmt.Errorf("failed to prepare metadata blob: %w", err)
	}

	updates := []git.RefUpdate{
		{RefName: fmt.Sprintf("refs/heads/%s", branchName), NewSHA: newRev, OldSHA: oldBranchSHA},
		{RefName: fmt.Sprintf("%s%s", git.MetadataRefPrefix, branchName), NewSHA: metadataSHA, OldSHA: oldMetadataSHA},
	}
	if err := e.git.UpdateRefsBatch(ctx, updates); err != nil {
		return RestackBranchResult{Result: RestackConflict, RebasedBranchBase: parentRev}, fmt.Errorf("failed to update refs atomically: %w", err)
	}

	if worktreePath, wtErr := e.git.GetWorktreePathForBranch(ctx, branchName); wtErr == nil && worktreePath != "" {
		_ = e.git.ResetWorktreeWorkingDir(ctx, worktreePath) //nolint:errcheck // best-effort
	}

	if metaMap != nil {
		metaMap[branchName] = updatedMeta
	}
	if revMap != nil {
		revMap[branchName] = newRev
	}

	return RestackBranchResult{
		Result:            RestackDone,
		RebasedBranchBase: parentRev,
		Reparented:        item.Reparented,
		OldParent:         item.OldParent,
		NewParent:         item.NewParent,
	}, nil
}

// RestackBranches implements a hybrid batch approach for performance:
// 1. Collect all data required for the restack (in bulk)
// 2. Process branches using individual restackBranch calls with deferred rebuilds
// 3. Final cache rebuild
func (e *engineImpl) RestackBranches(ctx context.Context, branches []Branch) (RestackBatchResult, error) {
	return e.restackBranches(ctx, branches, nil, nil, nil)
}

// RestackBranchesWithProgress mirrors RestackBranches but reports progress
// after each branch is processed.
func (e *engineImpl) RestackBranchesWithProgress(ctx context.Context, branches []Branch, progress RestackBranchProgressFunc) (RestackBatchResult, error) {
	return e.restackBranches(ctx, branches, nil, nil, progress)
}

// RestackBranchesWithValidatedRebases applies successful dry-run validation
// commits directly to branch refs. Validation worktrees share the repository's
// object database, so the rebased commits can be reused instead of replaying
// the same rebase a second time.
func (e *engineImpl) RestackBranchesWithValidatedRebases(ctx context.Context, branches []Branch, validation *RebaseValidation, progress RestackBranchProgressFunc) (RestackBatchResult, error) {
	plan, err := e.PlanRestack(ctx, branches)
	if err != nil {
		return RestackBatchResult{}, err
	}
	return e.restackBranches(ctx, branches, validation, plan, progress)
}

// RestackBranchesWithValidatedPlan applies successful dry-run validation
// commits using the caller's restack plan.
func (e *engineImpl) RestackBranchesWithValidatedPlan(ctx context.Context, branches []Branch, validation *RebaseValidation, plan *RestackPlan, progress RestackBranchProgressFunc) (RestackBatchResult, error) {
	return e.restackBranches(ctx, branches, validation, plan, progress)
}

func (e *engineImpl) restackBranches(ctx context.Context, branches []Branch, validation *RebaseValidation, plan *RestackPlan, progress RestackBranchProgressFunc) (RestackBatchResult, error) {
	// Save current branch to restore after restacking
	originalBranch := e.CurrentBranch()
	var originalRev string
	if originalBranch == nil {
		originalRev, _ = e.git.GetCurrentRevision(ctx)
	}

	defer func() {
		if originalBranch != nil {
			_ = e.CheckoutBranch(ctx, *originalBranch)
		} else if originalRev != "" {
			_ = e.git.CheckoutDetached(ctx, originalRev)
		}
	}()

	// 1. Collect all the data required for the restack (in bulk)
	branchNames := make([]string, len(branches))
	for i, b := range branches {
		branchNames[i] = b.GetName()
	}

	// Identify all potential parents and ancestors to fetch their metadata and revisions too
	e.mu.RLock()
	allInvolvedBranches := make(map[string]bool)
	for _, name := range branchNames {
		allInvolvedBranches[name] = true
		// Crawl up the branch state to find all ancestors
		current := name
		for {
			state := e.readState(current)
			if state == nil || state.Parent == e.trunk || allInvolvedBranches[state.Parent] {
				break
			}
			allInvolvedBranches[state.Parent] = true
			current = state.Parent
		}
	}
	// Also include trunk
	involvedBranchNames := make([]string, 0, len(allInvolvedBranches)+1)
	for name := range allInvolvedBranches {
		involvedBranchNames = append(involvedBranchNames, name)
	}
	involvedBranchNames = append(involvedBranchNames, e.trunk)
	e.mu.RUnlock()

	// Fetch ALL metadata in parallel
	allMeta, _ := e.batchReadMetadata(involvedBranchNames)

	// Fetch ALL revisions in parallel
	allRevisions, _ := e.git.BatchGetRevisions(involvedBranchNames)

	// 2. Apply the restack changes
	results := make(map[string]RestackBranchResult)
	needsRebuild := false

	for i, branch := range branches {
		branchName := branch.GetName()
		var result RestackBranchResult
		var err error
		if validation != nil {
			result, err = e.restackBranchWithValidatedRebase(ctx, branch, validation, plan, allMeta, allRevisions)
		} else {
			result, err = e.restackBranch(ctx, branch, allMeta, allRevisions)
		}
		results[branchName] = result

		if err == nil && progress != nil {
			progress(branch, result)
		}

		if err == nil && (result.Result == RestackDone || result.Result == RestackUnneeded) {
			// Update the revision map with the current SHA of the branch.
			// This is important because subsequent branches in the batch might
			// use this branch as their parent.
			if currentSha, err := e.git.GetRevision(branchName); err == nil {
				if allRevisions == nil {
					allRevisions = make(map[string]string)
				}
				allRevisions[branchName] = currentSha
			}
		}

		if err != nil {
			// Convert remaining branches to []string for RestackBatchResult
			remainingBranchNames := make([]string, len(branches[i+1:]))
			for j, b := range branches[i+1:] {
				remainingBranchNames[j] = b.GetName()
			}
			return RestackBatchResult{
				ConflictBranch:    branchName,
				RebasedBranchBase: result.RebasedBranchBase,
				RemainingBranches: remainingBranchNames,
				Results:           results,
			}, err
		}

		if result.Result == RestackConflict {
			// Convert remaining branches to []string for RestackBatchResult
			remainingBranchNames := make([]string, len(branches[i+1:]))
			for j, b := range branches[i+1:] {
				remainingBranchNames[j] = b.GetName()
			}
			return RestackBatchResult{
				ConflictBranch:    branchName,
				RebasedBranchBase: result.RebasedBranchBase,
				RemainingBranches: remainingBranchNames,
				Results:           results,
			}, nil
		}

		if result.Result == RestackDone {
			needsRebuild = true
		}
	}

	// 3. Collect the results
	// (results map already contains the results)

	// Single rebuild at the end if any branches were restacked
	if needsRebuild {
		if err := e.rebuild(); err != nil {
			return RestackBatchResult{
				Results: results,
			}, fmt.Errorf("failed to rebuild after batch restack: %w", err)
		}
	}

	return RestackBatchResult{
		Results: results,
	}, nil
}

// ContinueRebase continues an in-progress rebase
func (e *engineImpl) ContinueRebase(ctx context.Context, branchName string, rebasedBranchBase string) (ContinueRebaseResult, error) {
	// Call git rebase --continue
	result, err := e.git.RebaseContinue(ctx)
	if err != nil {
		return ContinueRebaseResult{Result: int(git.RebaseConflict), BranchName: branchName, RerereResolvedCount: result.RerereResolvedCount}, err
	}

	if result.Result == git.RebaseConflict {
		return ContinueRebaseResult{Result: int(git.RebaseConflict), BranchName: branchName, RerereResolvedCount: result.RerereResolvedCount}, nil
	}

	// Get the new rebased SHA
	newRev, err := e.git.GetCurrentRevision(ctx)
	if err != nil {
		return ContinueRebaseResult{BranchName: branchName}, fmt.Errorf("failed to get new revision after rebase: %w", err)
	}

	// Update the branch reference to the new rebased commit
	err = e.git.UpdateBranchRef(ctx, branchName, newRev)
	if err != nil {
		return ContinueRebaseResult{BranchName: branchName}, fmt.Errorf("failed to update branch reference %s: %w", branchName, err)
	}

	// Update metadata
	if rebasedBranchBase != "" {
		if err := e.updateParentRevision(ctx, branchName, rebasedBranchBase); err != nil {
			return ContinueRebaseResult{BranchName: branchName}, fmt.Errorf("failed to update metadata: %w", err)
		}
	}

	// Rebuild to refresh cache
	if err := e.rebuild(); err != nil {
		return ContinueRebaseResult{}, fmt.Errorf("failed to rebuild after continue: %w", err)
	}

	return ContinueRebaseResult{
		Result:              int(git.RebaseDone),
		BranchName:          branchName,
		RerereResolvedCount: result.RerereResolvedCount,
	}, nil
}

// Rebase rebases a branch onto another branch
func (e *engineImpl) Rebase(ctx context.Context, branchName, upstream, oldUpstream string) (RestackResult, error) {
	gitResult, err := e.git.Rebase(ctx, branchName, upstream, oldUpstream)
	if err != nil {
		return RestackConflict, err
	}

	if gitResult.Result == git.RebaseConflict {
		return RestackConflict, nil
	}

	return RestackDone, nil
}
