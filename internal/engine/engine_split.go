package engine

import (
	"context"
	"fmt"
	"slices"

	"stackit.dev/stackit/internal/git"
)

// ApplySplitToCommits creates branches at specified commit points
func (e *engineImpl) ApplySplitToCommits(ctx context.Context, opts ApplySplitOptions) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if len(opts.BranchNames) != len(opts.BranchPoints) {
		return fmt.Errorf("invalid number of branch names: got %d names but %d branch points", len(opts.BranchNames), len(opts.BranchPoints))
	}

	// Get metadata for the branch being split
	meta, err := e.readMetadata(opts.BranchToSplit)
	if err != nil {
		return fmt.Errorf("failed to read metadata: %w", err)
	}

	if meta.GetParentBranchName() == nil {
		return fmt.Errorf("branch %s has no parent", opts.BranchToSplit)
	}

	// Capture the original stack ID to preserve it on new branches
	var originalStackID string
	if meta.GetStackID() != nil {
		originalStackID = *meta.GetStackID()
	}

	parentBranchName := *meta.GetParentBranchName()
	parentRevision := *meta.GetParentBranchRevision()
	children := e.state.childrenMap[opts.BranchToSplit]

	// Reverse branch points (newest to oldest -> oldest to newest)
	reversedBranchPoints := make([]int, len(opts.BranchPoints))
	for i, point := range opts.BranchPoints {
		reversedBranchPoints[len(opts.BranchPoints)-1-i] = point
	}

	// Keep track of the last branch's name + SHA for metadata
	// In sibling mode, all branches share the same parent, so we don't update these
	lastBranchName := parentBranchName
	lastBranchRevision := parentRevision

	// Create each branch
	for idx, branchName := range opts.BranchNames {
		// Get commit SHA at the offset
		branchRevision, err := e.git.GetCommitSHA(opts.BranchToSplit, reversedBranchPoints[idx])
		if err != nil {
			return fmt.Errorf("failed to get commit SHA at offset %d: %w", reversedBranchPoints[idx], err)
		}

		// Create branch at that SHA
		err = e.git.CreateBranchForce(ctx, branchName, branchRevision)
		if err != nil {
			return fmt.Errorf("failed to create branch %s: %w", branchName, err)
		}

		// Preserve PR info if branch name matches original
		var prInfo *PrInfo
		if branchName == opts.BranchToSplit {
			branchToSplit := e.GetBranch(opts.BranchToSplit)
			prInfo, _ = e.GetPrInfo(branchToSplit)
		}

		// Determine parent for this branch
		// In sibling mode: all branches share the original parent
		// In chain mode: each branch's parent is the previous branch
		branchParentName := lastBranchName
		branchParentRevision := lastBranchRevision
		if opts.AsSibling {
			branchParentName = parentBranchName
			branchParentRevision = parentRevision
		}

		// Track branch with parent
		newMeta := git.NewMetaFrom(git.MetaFields{
			ParentBranchName:     &branchParentName,
			ParentBranchRevision: &branchParentRevision,
		})

		// Preserve stack ID from original branch
		if originalStackID != "" {
			newMeta = newMeta.WithStackID(&originalStackID)
		}

		// Preserve PR info if applicable
		if prInfo != nil {
			newMeta = newMeta.WithPrInfo(&git.PrInfoPersistence{
				Number:  prInfo.Number(),
				Title:   stringPtr(prInfo.Title()),
				Body:    stringPtr(prInfo.Body()),
				IsDraft: new(prInfo.IsDraft()),
				State:   stringPtr(prInfo.State()),
				Base:    stringPtr(prInfo.Base()),
				URL:     stringPtr(prInfo.URL()),
			})
		}

		if err := e.writeMetadata(branchName, newMeta); err != nil {
			return fmt.Errorf("failed to write metadata for %s: %w", branchName, err)
		}

		// Update in-memory cache
		e.state.branchState.Set(branchName, &BranchState{
			Parent: branchParentName,
		})
		e.state.childrenMap[branchParentName] = append(e.state.childrenMap[branchParentName], branchName)
		slices.Sort(e.state.childrenMap[branchParentName])

		// Update last branch info (only matters for chain mode)
		lastBranchName = branchName
		lastBranchRevision = branchRevision
	}

	// Update children to point to last branch.
	// Note: This applies in both chain and sibling modes. In sibling mode, the split
	// branches are siblings (share the same parent), but children of the original branch
	// still need to be reparented to the branch that continues the stack (the last/newest
	// branch, which is typically the original branch name if it was preserved).
	if lastBranchName != opts.BranchToSplit {
		lastBranch := e.GetBranch(lastBranchName)
		for _, childBranchName := range children {
			if err := e.SetParent(ctx, e.GetBranch(childBranchName), lastBranch); err != nil {
				return fmt.Errorf("failed to update parent for %s: %w", childBranchName, err)
			}
		}
	}

	// Delete original branch if not in branchNames
	if !slices.Contains(opts.BranchNames, opts.BranchToSplit) {
		if err := e.DeleteBranch(ctx, e.GetBranch(opts.BranchToSplit)); err != nil {
			return fmt.Errorf("failed to delete original branch: %w", err)
		}
	}

	// Checkout last branch
	e.currentBranch = lastBranchName
	lastBranch := e.GetBranch(lastBranchName)
	if err := e.git.CheckoutBranch(ctx, lastBranch.GetName()); err != nil {
		return fmt.Errorf("failed to checkout branch %s: %w", lastBranchName, err)
	}

	return nil
}

// Detach detaches HEAD to a specific revision
func (e *engineImpl) Detach(ctx context.Context, revision string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Checkout the revision in detached HEAD state
	err := e.git.CheckoutDetached(ctx, revision)
	if err != nil {
		return fmt.Errorf("failed to detach HEAD: %w", err)
	}

	e.currentBranch = ""
	return nil
}

// DetachAndResetBranchChanges detaches HEAD and soft resets to the parent's merge base,
// leaving the branch's changes as unstaged modifications. This is used by split --by-hunk
// to allow the user to interactively re-stage changes into new branches.
func (e *engineImpl) DetachAndResetBranchChanges(ctx context.Context, branchName string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Get branch revision
	branch := e.GetBranch(branchName)
	branchRevision, err := branch.GetRevision()
	if err != nil {
		return fmt.Errorf("failed to get branch revision: %w", err)
	}

	// Get the parent branch
	parentBranchName := e.trunk
	if state := e.state.branchState.GetByName(branchName); state != nil {
		parentBranchName = state.Parent
	}

	// Get the merge base between this branch and its parent
	mergeBase, err := e.git.GetMergeBase(branchName, parentBranchName)
	if err != nil {
		return fmt.Errorf("failed to get merge base: %w", err)
	}

	// Detach HEAD to the branch revision first
	err = e.git.CheckoutDetached(ctx, branchRevision)
	if err != nil {
		return fmt.Errorf("failed to detach HEAD: %w", err)
	}

	// Soft reset to the merge base - this keeps all the branch's changes
	// but unstages them, allowing the user to re-stage them interactively
	err = e.git.MixedReset(ctx, mergeBase)
	if err != nil {
		return fmt.Errorf("failed to mixed reset: %w", err)
	}

	e.currentBranch = ""
	return nil
}

// ForceCheckoutBranch checks out a branch
func (e *engineImpl) ForceCheckoutBranch(ctx context.Context, branch Branch) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	branchName := branch.GetName()
	err := e.git.CheckoutBranchForce(ctx, branchName)
	if err != nil {
		return fmt.Errorf("failed to force checkout branch: %w", err)
	}

	e.currentBranch = branchName
	return nil
}
