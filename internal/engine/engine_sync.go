package engine

import (
	"context"
	"encoding/json"
	"fmt"

	"stackit.dev/stackit/internal/git"
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
		remoteShas, fetchErr := e.git.FetchRemoteShas(remote)
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
	rebuildAfterRestack bool,
) (RestackBranchResult, error) {
	branchName := branch.GetName()
	if e.IsTrunk(branch) {
		return RestackBranchResult{Result: RestackUnneeded}, nil
	}

	e.mu.RLock()
	parent, ok := e.parentMap[branchName]
	e.mu.RUnlock()

	if !ok {
		// RESILIENCY: Try to auto-discover parent if branch is not tracked
		ancestors, err := e.FindMostRecentTrackedAncestors(ctx, branchName)
		if err == nil && len(ancestors) > 0 {
			parent = ancestors[0]
			// Auto-track the branch
			if err := e.TrackBranch(ctx, branchName, parent); err == nil {
				ok = true
			}
		}

		if !ok {
			return RestackBranchResult{Result: RestackUnneeded}, fmt.Errorf("branch %s is not tracked", branchName)
		}
	}

	if e.IsLocked(branch) {
		return RestackBranchResult{Result: RestackUnneeded, LockReason: e.GetLockReason(branch)}, nil
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
			meta, err := e.git.ReadMetadata(branchName)
			if err != nil {
				meta = &git.Meta{}
			}
			if parentRev != "" {
				meta.ParentBranchRevision = &parentRev
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
				// If the branch is checked out in a different worktree, reset that worktree
				if worktreePath, err := e.git.GetWorktreePathForBranch(ctx, branchName); err == nil && worktreePath != "" {
					_ = e.git.ResetWorktreeWorkingDir(ctx, worktreePath)
				}
			}

			return RestackBranchResult{
				Result:            RestackDone,
				RebasedBranchBase: remoteSha,
			}, nil
		}

		return RestackBranchResult{Result: RestackUnneeded, Frozen: true}, nil
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

		// Reparent to the nearest valid ancestor
		if err := e.SetParent(ctx, e.GetBranch(branchName), e.GetBranch(newParent)); err != nil {
			return RestackBranchResult{Result: RestackConflict}, fmt.Errorf("failed to reparent %s to %s: %w", branchName, newParent, err)
		}
		parent = newParent
		reparented = true

		// Update the cached metadata if we're using a metaMap, otherwise the subsequent
		// write will overwrite the parent change.
		if metaMap != nil {
			if updatedMeta, err := e.git.ReadMetadata(branchName); err == nil {
				metaMap[branchName] = updatedMeta
			}
		}
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
		meta, err = e.git.ReadMetadata(branchName)
		if err != nil {
			return RestackBranchResult{Result: RestackConflict, RebasedBranchBase: parentRev}, fmt.Errorf("failed to read metadata: %w", err)
		}
	}

	// Check if branch needs restacking using cached metadata
	if meta.ParentBranchRevision != nil && *meta.ParentBranchRevision == parentRev {
		return RestackBranchResult{
			Result:            RestackUnneeded,
			RebasedBranchBase: parentRev,
			Reparented:        reparented,
			OldParent:         oldParent,
			NewParent:         parent,
		}, nil
	}

	oldParentRev := parentRev
	if meta.ParentBranchRevision != nil {
		oldParentRev = *meta.ParentBranchRevision
	}

	// If parent hasn't changed, no need to restack (early exit before expensive operations)
	if parentRev == oldParentRev {
		return RestackBranchResult{
			Result:            RestackUnneeded,
			RebasedBranchBase: parentRev,
			Reparented:        reparented,
			OldParent:         oldParent,
			NewParent:         parent,
		}, nil
	}

	// RESILIENCY: If oldParentRev is no longer an ancestor of branchName,
	// or if it's empty, find the actual merge base. This handles cases where
	// the parent was amended or rebased outside of stackit.
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

	// Check again after resiliency logic - parent might still be unchanged
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
			Result:            RestackConflict,
			RebasedBranchBase: parentRev,
			Reparented:        reparented,
			OldParent:         oldParent,
			NewParent:         parent,
		}, fmt.Errorf("rebase failed for %s onto %s (old base %s): %w", branchName, parent, oldParentRev, err)
	}

	if gitResult == git.RebaseConflict {
		return RestackBranchResult{
			Result:            RestackConflict,
			RebasedBranchBase: parentRev,
			Reparented:        reparented,
			OldParent:         oldParent,
			NewParent:         parent,
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
	meta, metaReadErr := e.git.ReadMetadata(branchName)
	if metaReadErr != nil {
		meta = &git.Meta{}
	}
	meta.ParentBranchRevision = &parentRev
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
	if worktreePath, err := e.git.GetWorktreePathForBranch(ctx, branchName); err == nil && worktreePath != "" {
		// Best-effort: don't fail restack if worktree reset fails
		_ = e.git.ResetWorktreeWorkingDir(ctx, worktreePath)
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
		if updatedMeta, err := e.git.ReadMetadata(branchName); err == nil {
			metaMap[branchName] = updatedMeta
		}
	}

	// Update cache incrementally if requested (much faster than full rebuild)
	if rebuildAfterRestack {
		e.updateBranchInCache(branchName)
	}

	return RestackBranchResult{
		Result:            RestackDone,
		RebasedBranchBase: parentRev,
		Reparented:        reparented,
		OldParent:         oldParent,
		NewParent:         parent,
	}, nil
}

// RestackBranches implements a hybrid batch approach for performance:
// 1. Collect all data required for the restack (in bulk)
// 2. Process branches using individual restackBranch calls with deferred rebuilds
// 3. Final cache rebuild
func (e *engineImpl) RestackBranches(ctx context.Context, branches []Branch) (RestackBatchResult, error) {
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
		// Crawl up the parent map to find all ancestors
		current := name
		for {
			parent, ok := e.parentMap[current]
			if !ok || parent == e.trunk || allInvolvedBranches[parent] {
				break
			}
			allInvolvedBranches[parent] = true
			current = parent
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
	allMeta, _ := e.git.BatchReadMetadata(involvedBranchNames)

	// Fetch ALL revisions in parallel
	allRevisions, _ := e.git.BatchGetRevisions(involvedBranchNames)

	// 2. Apply the restack changes
	results := make(map[string]RestackBranchResult)
	needsRebuild := false

	for i, branch := range branches {
		branchName := branch.GetName()
		result, err := e.restackBranch(ctx, branch, allMeta, allRevisions, false) // Don't rebuild after each branch
		results[branchName] = result

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
		return ContinueRebaseResult{Result: int(git.RebaseConflict), BranchName: branchName}, err
	}

	if result == git.RebaseConflict {
		return ContinueRebaseResult{Result: int(git.RebaseConflict), BranchName: branchName}, nil
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
		if err := e.UpdateParentRevision(branchName, rebasedBranchBase); err != nil {
			return ContinueRebaseResult{BranchName: branchName}, fmt.Errorf("failed to update metadata: %w", err)
		}
	}

	// Rebuild to refresh cache
	if err := e.rebuild(); err != nil {
		return ContinueRebaseResult{}, fmt.Errorf("failed to rebuild after continue: %w", err)
	}

	return ContinueRebaseResult{
		Result:     int(git.RebaseDone),
		BranchName: branchName,
	}, nil
}

// Rebase rebases a branch onto another branch
func (e *engineImpl) Rebase(ctx context.Context, branchName, upstream, oldUpstream string) (RestackResult, error) {
	gitResult, err := e.git.Rebase(ctx, branchName, upstream, oldUpstream)
	if err != nil {
		return RestackConflict, err
	}

	if gitResult == git.RebaseConflict {
		return RestackConflict, nil
	}

	return RestackDone, nil
}
