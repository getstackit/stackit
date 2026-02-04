package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"stackit.dev/stackit/internal/git"
)

// timeNow is a variable for time.Now to allow mocking in tests
var timeNow = time.Now

// WorktreeCheckoutMode specifies how files are checked out in a worktree.
type WorktreeCheckoutMode int

const (
	// WorktreeCheckoutFull checks out all files (default behavior).
	WorktreeCheckoutFull WorktreeCheckoutMode = iota
	// WorktreeCheckoutShallow creates a worktree without checking out files.
	// This is faster for validation-only operations that don't need actual files.
	WorktreeCheckoutShallow
)

// WorktreePruneMode specifies whether to prune stale worktrees before creation.
type WorktreePruneMode int

const (
	// WorktreePruneAuto prunes stale worktrees before creating a new one (default).
	WorktreePruneAuto WorktreePruneMode = iota
	// WorktreePruneSkip skips pruning. Use when the caller has already pruned
	// (e.g., parallel worktree creation).
	WorktreePruneSkip
)

// CreateBranch creates a new branch at the given start point
func (e *engineImpl) CreateBranch(ctx context.Context, branchName string, startPoint string) error {
	return e.git.CreateBranch(ctx, branchName, startPoint)
}

// ResetHard performs a hard reset to the given revision
func (e *engineImpl) ResetHard(ctx context.Context, revision string) error {
	return e.git.HardReset(ctx, revision)
}

// ResetMerge performs a merge reset to the given revision
func (e *engineImpl) ResetMerge(ctx context.Context, revision string) error {
	return e.git.ResetMerge(ctx, revision)
}

// Merge merges a revision into the current branch
func (e *engineImpl) Merge(ctx context.Context, revision string, opts MergeOptions) error {
	return e.git.Merge(ctx, revision, git.MergeOptions{
		FFOnly:  opts.FFOnly,
		NoEdit:  opts.NoEdit,
		NoFF:    opts.NoFF,
		Message: opts.Message,
	})
}

// MergeMultiple performs an octopus merge of multiple branches into the current branch
func (e *engineImpl) MergeMultiple(ctx context.Context, branches []string, opts MergeOptions) error {
	return e.git.MergeMultiple(ctx, branches, git.MergeOptions{
		NoEdit:  opts.NoEdit,
		NoFF:    opts.NoFF,
		Message: opts.Message,
	})
}

// Fetch fetches from a remote
func (e *engineImpl) Fetch(ctx context.Context, remote string, branch string) error {
	return e.git.Fetch(ctx, remote, branch)
}

// InteractiveRebase starts an interactive rebase
func (e *engineImpl) InteractiveRebase(ctx context.Context, onto string) error {
	return e.git.InteractiveRebase(ctx, onto)
}

// PushBranch pushes a branch to the remote
func (e *engineImpl) PushBranch(ctx context.Context, branch Branch, remote string, opts git.PushOptions) error {
	return e.git.PushBranch(ctx, branch.GetName(), remote, opts)
}

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
	branchExists := false
	for _, name := range e.branches {
		if name == branchName {
			branchExists = true
			break
		}
	}
	if !branchExists {
		// Refresh branches list
		branches, err := e.git.GetAllBranchNames()
		if err != nil {
			e.mu.Unlock()
			return fmt.Errorf("failed to get branches: %w", err)
		}
		e.branches = branches
		e.branchNamesSet = nil // invalidate cache
		branchExists = false
		for _, name := range e.branches {
			if name == branchName {
				branchExists = true
				break
			}
		}
		if !branchExists {
			e.mu.Unlock()
			return fmt.Errorf("branch %s does not exist", branchName)
		}
	}

	// Validate parent exists (or is trunk)
	if parentBranchName != e.trunk {
		parentExists := false
		for _, name := range e.branches {
			if name == parentBranchName {
				parentExists = true
				break
			}
		}
		if !parentExists {
			// Refresh branches list to check again
			branches, err := e.git.GetAllBranchNames()
			if err != nil {
				e.mu.Unlock()
				return fmt.Errorf("failed to get branches: %w", err)
			}
			e.branches = branches
			e.branchNamesSet = nil // invalidate cache
			parentExists = false
			for _, name := range e.branches {
				if name == parentBranchName {
					parentExists = true
					break
				}
			}
			if !parentExists {
				e.mu.Unlock()
				return fmt.Errorf("parent branch %s does not exist", parentBranchName)
			}
		}
	}
	e.mu.Unlock()

	// SetParent handles its own transaction and locking
	if err := e.SetParent(ctx, e.GetBranch(branchName), e.GetBranch(parentBranchName)); err != nil {
		return err
	}

	// Assign stack ID based on parent
	return e.assignStackID(ctx, branchName, parentBranchName)
}

// assignStackID assigns a stack ID to a branch based on its parent.
// Stack IDs propagate through the branch tree:
//   - A new stack (branch off trunk) gets a fresh, unique ID
//   - A branch stacked on a tracked branch inherits the parent's stack ID,
//     binding all branches in a stack to the same identifier
//   - Legacy branches without StackID are left unchanged for gradual migration
func (e *engineImpl) assignStackID(ctx context.Context, branchName string, parentBranchName string) error {
	var stackID string

	// If parent is trunk, generate a new stack ID
	if parentBranchName == e.trunk {
		stackID = e.GenerateStackID(branchName)

		// Create stack ref with initial metadata
		stackMeta := &git.StackMeta{
			ID:        stackID,
			CreatedAt: timeNow(),
		}

		// Try to get user name for CreatedBy
		if userName, err := e.git.GetUserName(ctx); err == nil {
			stackMeta.CreatedBy = userName
		}

		if err := e.git.WriteStackMeta(stackID, stackMeta); err != nil {
			return fmt.Errorf("failed to create stack ref: %w", err)
		}
	} else {
		// Inherit stack ID from parent
		parentBranch := e.GetBranch(parentBranchName)
		stackID = e.GetStackID(parentBranch)

		// If parent has no stack ID, this is a legacy branch - no action needed
		if stackID == "" {
			return nil
		}
	}

	// Set the stack ID on the branch
	return e.SetStackID(ctx, e.GetBranch(branchName), stackID)
}

// UntrackBranch stops tracking a branch by deleting its metadata
func (e *engineImpl) UntrackBranch(branchName string) error {
	// Delete metadata
	if err := e.git.DeleteMetadata(branchName); err != nil {
		return fmt.Errorf("failed to delete metadata ref: %w", err)
	}

	// Rebuild cache
	return e.rebuild()
}

// DeleteBranch deletes a branch and its metadata
func (e *engineImpl) DeleteBranch(ctx context.Context, branch Branch) error {
	branchName := branch.GetName()
	if e.IsTrunk(branch) {
		return fmt.Errorf("cannot delete trunk branch")
	}

	// Get children and parent info under lock, then release for SetParent calls
	e.mu.Lock()

	// Get children before deletion
	children := make([]string, len(e.childrenMap[branchName]))
	copy(children, e.childrenMap[branchName])

	// Get parent
	parent := e.trunk
	if state := e.branchState.GetByName(branchName); state != nil {
		parent = state.Parent
	}

	// If deleting current branch, switch to trunk first
	if branchName == e.currentBranch {
		// Access trunk directly while holding the lock (avoid deadlock from e.Trunk() trying to acquire RLock)
		trunkBranch := NewBranch(e.trunk, e)
		if err := e.git.CheckoutBranch(ctx, trunkBranch.GetName()); err != nil {
			e.mu.Unlock()
			return fmt.Errorf("failed to switch to trunk before deleting current branch: %w", err)
		}
		e.currentBranch = e.trunk
	}
	e.mu.Unlock()

	// Delete git branch (no lock needed for git operations)
	if err := e.git.DeleteBranch(ctx, branch.GetName()); err != nil {
		if !git.IsBranchNotFoundError(err) {
			return fmt.Errorf("failed to delete branch: %w", err)
		}
	}

	// Delete metadata
	if err := e.git.DeleteMetadata(branchName); err != nil {
		_, _ = fmt.Fprintf(e.writer, "Warning: failed to delete metadata ref for %s: %v\n", branchName, err)
	}

	// Delete local metadata
	if err := e.git.DeleteRef(fmt.Sprintf("%s%s", git.LocalMetadataRefPrefix, branchName)); err != nil {
		_, _ = fmt.Fprintf(e.writer, "Warning: failed to delete local metadata ref for %s: %v\n", branchName, err)
	}

	// Update children to point to parent (SetParent handles its own transactions)
	parentBranch := e.GetBranch(parent)
	for _, child := range children {
		if err := e.SetParent(ctx, e.GetBranch(child), parentBranch); err != nil {
			continue
		}
	}

	// Clean up in-memory cache for deleted branch
	e.mu.Lock()
	defer e.mu.Unlock()

	// Remove from parent's children list
	if parent != "" {
		parentChildren := e.childrenMap[parent]
		if i := slices.Index(parentChildren, branchName); i >= 0 {
			e.childrenMap[parent] = slices.Delete(parentChildren, i, i+1)
		}
	}

	// Remove from maps
	e.branchState.Delete(branchName)
	delete(e.childrenMap, branchName)

	// Remove from branches list
	if i := slices.Index(e.branches, branchName); i >= 0 {
		e.branches = slices.Delete(e.branches, i, i+1)
		e.branchNamesSet = nil // invalidate cache
	}

	return nil
}

// DeleteBranches deletes multiple branches and returns the children that need restacking
func (e *engineImpl) DeleteBranches(ctx context.Context, branches []Branch) ([]string, error) {
	// Identify all children of all branches to be deleted
	allChildren := make(map[string]bool)
	toDeleteSet := make(map[string]bool)
	for _, b := range branches {
		branchName := b.GetName()
		toDeleteSet[branchName] = true
		e.mu.RLock()
		children := e.childrenMap[branchName]
		e.mu.RUnlock()
		for _, child := range children {
			allChildren[child] = true
		}
	}

	// Remove branches that are also being deleted from the children set
	for _, b := range branches {
		delete(allChildren, b.GetName())
	}

	// Delete branches
	for _, b := range branches {
		if err := e.DeleteBranch(ctx, b); err != nil {
			return nil, fmt.Errorf("failed to delete branch %s: %w", b.GetName(), err)
		}
	}

	// Convert children map to slice
	childrenToRestack := make([]string, 0, len(allChildren))
	for child := range allChildren {
		childrenToRestack = append(childrenToRestack, child)
	}

	return childrenToRestack, nil
}

// CheckoutBranch checks out an existing branch
func (e *engineImpl) CheckoutBranch(ctx context.Context, branch Branch) error {
	branchName := branch.GetName()
	if err := e.git.CheckoutBranch(ctx, branchName); err != nil {
		// If it's already used by another worktree, try checking out detached
		if strings.Contains(err.Error(), "already used by worktree") {
			if err := e.git.CheckoutDetached(ctx, branchName); err != nil {
				return err
			}
			e.mu.Lock()
			e.currentBranch = "" // Detached HEAD
			e.mu.Unlock()
			return nil
		}
		return err
	}

	e.mu.Lock()
	e.currentBranch = branchName
	e.mu.Unlock()
	return nil
}

// UpdateBranchRef updates a branch reference to point to a new revision
func (e *engineImpl) UpdateBranchRef(ctx context.Context, branchName, revision string) error {
	return e.git.UpdateBranchRef(ctx, branchName, revision)
}

// CreateAndCheckoutBranch creates and checks out a new branch
func (e *engineImpl) CreateAndCheckoutBranch(ctx context.Context, branch Branch) error {
	branchName := branch.GetName()
	if err := e.git.CreateAndCheckoutBranch(ctx, branchName); err != nil {
		return err
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	e.currentBranch = branchName
	// Add to branches list if not already there
	if !slices.Contains(e.branches, branchName) {
		e.branches = append(e.branches, branchName)
		e.branchNamesSet = nil // invalidate cache
	}

	return nil
}

// SetParent updates a branch's parent using transaction API with retry logic
// for concurrent modification resilience.
func (e *engineImpl) SetParent(ctx context.Context, branch Branch, parentBranch Branch) error {
	branchName := branch.GetName()
	parentBranchName := parentBranch.GetName()

	if branchName == parentBranchName {
		return fmt.Errorf("branch %s cannot be its own parent", branchName)
	}

	return e.WithRetry(ctx, func() error {
		// Get new parent revision (may run multiple times on retry)
		parentRev, err := e.git.GetMergeBase(branchName, parentBranchName)
		if err != nil {
			return fmt.Errorf("failed to get merge base: %w", err)
		}

		// Read existing metadata
		meta, err := e.git.ReadMetadata(branchName)
		if err != nil {
			return fmt.Errorf("failed to read metadata: %w", err)
		}

		// Get old parent
		oldParent := ""
		if meta.ParentBranchName != nil {
			oldParent = *meta.ParentBranchName
		}

		// Only update ParentBranchRevision if it's currently nil, invalid, or if we're not
		// in a "parent merged into trunk" situation.
		shouldUpdateRevision := true
		if oldParent != "" && oldParent != parentBranchName && meta.ParentBranchRevision != nil && *meta.ParentBranchRevision != "" {
			// Check if existing revision is still a valid ancestor of the branch
			if isAncestor, _ := e.git.IsAncestor(*meta.ParentBranchRevision, branchName); isAncestor {
				// Check if the old parent was merged into the new parent (the "merge" case)
				if merged, _ := e.git.IsMerged(ctx, oldParent, parentBranchName); merged {
					shouldUpdateRevision = false
				}
			}
		}

		meta.ParentBranchName = &parentBranchName
		if shouldUpdateRevision {
			meta.ParentBranchRevision = &parentRev
		}

		// Use transaction for atomic update (Commit handles in-memory cache updates)
		tx := e.BeginTx(fmt.Sprintf("set parent: %s -> %s", branchName, parentBranchName))
		if err := tx.UpdateMeta(branchName, meta); err != nil {
			return err
		}
		return tx.Commit(ctx)
	})
}

// SetParentPreservingDivergence updates a branch's parent while preserving
// the divergence point if it remains a valid ancestor. This is useful when
// moving a branch to a new parent without changing which commits belong to it.
func (e *engineImpl) SetParentPreservingDivergence(ctx context.Context, branch Branch, newParent Branch, oldDivergencePoint string) error {
	if err := e.SetParent(ctx, branch, newParent); err != nil {
		return err
	}

	// If we have an old divergence point and it's still a valid ancestor,
	// restore it to preserve which commits belong to this branch
	if oldDivergencePoint != "" {
		isAncestor, err := e.git.IsAncestor(oldDivergencePoint, branch.GetName())
		if err == nil && isAncestor {
			return e.UpdateParentRevision(ctx, branch.GetName(), oldDivergencePoint)
		}
	}

	return nil
}

// UpdateParentRevision updates the parent revision in metadata using transaction API
// with retry logic for concurrent modification resilience.
func (e *engineImpl) UpdateParentRevision(ctx context.Context, branchName string, parentRev string) error {
	return e.WithRetry(ctx, func() error {
		// Read existing metadata (outside lock for performance)
		meta, err := e.git.ReadMetadata(branchName)
		if err != nil {
			return fmt.Errorf("failed to read metadata: %w", err)
		}

		meta.ParentBranchRevision = &parentRev

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
		meta, err := e.git.ReadMetadata(branchName)
		if err != nil {
			return fmt.Errorf("failed to read metadata: %w", err)
		}

		// Update scope
		if scope.IsEmpty() {
			meta.Scope = nil
		} else {
			scopeStr := scope.String()
			meta.Scope = &scopeStr
		}

		// Use transaction for atomic update
		tx := e.BeginTx(fmt.Sprintf("set scope: %s", branchName))
		if err := tx.UpdateMeta(branchName, meta); err != nil {
			return err
		}
		return tx.Commit(ctx)
	})
}

// SetLocked updates multiple branches' locked status atomically using transactions.
// It retries on concurrent modification errors with exponential backoff.
func (e *engineImpl) SetLocked(ctx context.Context, branches []Branch, reason LockReason) (BatchLockResult, error) {
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
		metas, readErrs := e.git.BatchReadMetadata(branchNames)

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
				meta = &git.Meta{}
			}
			meta.LockReason = reason
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
func (e *engineImpl) SetFrozen(ctx context.Context, branches []Branch, frozen bool) (BatchFreezeResult, error) {
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
		metas := e.git.BatchReadLocalMetadata(branchNames)

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

// RenameBranch renames a branch and its metadata
func (e *engineImpl) RenameBranch(ctx context.Context, oldBranch, newBranch Branch) error {
	oldName := oldBranch.GetName()
	newName := newBranch.GetName()

	e.mu.RLock()
	// Get children before renaming anything
	children := make([]string, len(e.childrenMap[oldName]))
	copy(children, e.childrenMap[oldName])
	e.mu.RUnlock()

	// Rename git branch
	if err := e.git.RenameBranch(ctx, oldName, newName); err != nil {
		return err
	}

	// Rename metadata ref
	if err := e.git.RenameMetadata(oldName, newName); err != nil {
		// Log but continue if metadata rename fails
		_, _ = fmt.Fprintf(e.writer, "Warning: failed to rename metadata ref: %v\n", err)
	}

	// Rename local metadata ref
	oldLocalRef := fmt.Sprintf("%s%s", git.LocalMetadataRefPrefix, oldName)
	newLocalRef := fmt.Sprintf("%s%s", git.LocalMetadataRefPrefix, newName)
	if sha, err := e.git.GetRef(oldLocalRef); err == nil {
		if err := e.git.UpdateRef(newLocalRef, sha); err == nil {
			if err := e.git.DeleteRef(oldLocalRef); err != nil {
				_, _ = fmt.Fprintf(e.writer, "Warning: failed to delete old local metadata ref: %v\n", err)
			}
		} else {
			_, _ = fmt.Fprintf(e.writer, "Warning: failed to update new local metadata ref: %v\n", err)
		}
	}

	// Update children to point to the new branch name
	for _, child := range children {
		childMeta, err := e.git.ReadMetadata(child)
		if err != nil {
			continue
		}
		childMeta.ParentBranchName = &newName
		if err := e.git.WriteMetadata(child, childMeta); err != nil {
			continue
		}
	}

	// Rebuild in-memory state to be safe
	return e.rebuild()
}

// Commit creates a new commit
func (e *engineImpl) Commit(_ context.Context, message string, verbose int, noVerify bool) error {
	return e.git.CommitWithOptions(git.CommitOptions{
		Message:  message,
		Verbose:  verbose,
		NoVerify: noVerify,
	})
}

// CommitWithOptions creates a new commit with the given options
func (e *engineImpl) CommitWithOptions(_ context.Context, opts git.CommitOptions) error {
	return e.git.CommitWithOptions(opts)
}

// StageAll stages all changes
func (e *engineImpl) StageAll(ctx context.Context) error {
	return e.git.StageAll(ctx)
}

// StagePatch stages changes interactively
func (e *engineImpl) StagePatch(ctx context.Context) error {
	return e.git.StagePatch(ctx)
}

// StageHunks stages specific hunks by applying them as patches
func (e *engineImpl) StageHunks(ctx context.Context, hunks []git.Hunk) error {
	return e.git.StageHunks(ctx, hunks)
}

// StashPush pushes current changes to the stash
func (e *engineImpl) StashPush(ctx context.Context, message string) (string, error) {
	return e.git.StashPush(ctx, message)
}

// StashPushStaged pushes only staged changes to the stash
func (e *engineImpl) StashPushStaged(ctx context.Context, message string) (string, error) {
	return e.git.StashPushStaged(ctx, message)
}

// StashPop pops the most recent stash
func (e *engineImpl) StashPop(ctx context.Context) error {
	return e.git.StashPop(ctx)
}

// AddWorktree adds a new worktree
func (e *engineImpl) AddWorktree(ctx context.Context, path string, branch string, detach bool) error {
	return e.git.AddWorktree(ctx, path, branch, detach)
}

// RemoveWorktree removes a worktree
func (e *engineImpl) RemoveWorktree(ctx context.Context, path string) error {
	return e.git.RemoveWorktree(ctx, path)
}

// PruneWorktrees removes stale worktree entries from .git/worktrees.
// This cleans up worktree information for worktrees whose working directory
// has been deleted or is otherwise unavailable.
func (e *engineImpl) PruneWorktrees(ctx context.Context) error {
	return e.git.PruneWorktrees(ctx)
}

// CreateTemporaryWorktree creates a temporary directory and adds a detached worktree
func (e *engineImpl) CreateTemporaryWorktree(ctx context.Context, branch string, prefix string) (string, func(), error) {
	return e.CreateTemporaryWorktreeWithOptions(ctx, branch, prefix, WorktreeCheckoutFull, WorktreePruneAuto)
}

// CreateTemporaryWorktreeSkipPrune is like CreateTemporaryWorktree but skips the automatic
// PruneWorktrees() call. Use this when creating multiple worktrees in parallel after
// manually calling PruneWorktrees() once, to avoid race conditions.
func (e *engineImpl) CreateTemporaryWorktreeSkipPrune(ctx context.Context, branch string, prefix string) (string, func(), error) {
	return e.CreateTemporaryWorktreeWithOptions(ctx, branch, prefix, WorktreeCheckoutFull, WorktreePruneSkip)
}

// CreateTemporaryWorktreeWithOptions creates a temporary directory and adds a detached worktree with options.
//
// checkout controls whether files are checked out:
//   - WorktreeCheckoutFull: checks out all files (default behavior)
//   - WorktreeCheckoutShallow: creates worktree without checking out files (faster for validation)
//
// prune controls whether stale worktrees are pruned before creation:
//   - WorktreePruneAuto: prunes stale worktrees first (default behavior)
//   - WorktreePruneSkip: skips pruning (use when caller has already pruned for parallel creation)
//
// Note: Callers that create multiple worktrees in parallel (like ValidateRebasesParallel) should call
// PruneWorktrees() once before starting parallel worktree creation and pass WorktreePruneSkip to avoid race conditions.
func (e *engineImpl) CreateTemporaryWorktreeWithOptions(ctx context.Context, branch string, prefix string, checkout WorktreeCheckoutMode, prune WorktreePruneMode) (string, func(), error) {
	// Prune stale worktree entries before creating new ones.
	// This cleans up entries in .git/worktrees/ that may be left behind from
	// incomplete cleanup after failed or canceled operations.
	// Skip if caller has already pruned (e.g., parallel worktree creation).
	if prune == WorktreePruneAuto {
		_ = e.git.PruneWorktrees(ctx)
	}

	tmpDir, err := os.MkdirTemp("", prefix)
	if err != nil {
		return "", nil, fmt.Errorf("failed to create temporary directory: %w", err)
	}

	// Use the unique temp directory basename as the worktree name to avoid collisions
	// in Git's .git/worktrees/ registry. Previously using a fixed "worktree" name caused
	// intermittent failures when stale entries remained after incomplete cleanup.
	worktreePath := filepath.Join(tmpDir, filepath.Base(tmpDir))

	// Serialize worktree creation to prevent races on .git/worktrees/ directory.
	// Git's `worktree add` command is not concurrency-safe - when multiple goroutines
	// run it simultaneously on the same repo, they can race on reading/writing the
	// .git/worktrees/ directory, causing "failed to read commondir" errors.
	//
	// The mutex ensures only one worktree is being created at a time per engine (repo).
	// This is acceptable because:
	// 1. Temp directory creation (above) is still parallel
	// 2. The actual rebase validation (after worktree creation) is still parallel
	// 3. Only the brief `git worktree add` command is serialized
	e.worktreeMu.Lock()
	err = e.git.AddWorktreeWithOptions(ctx, worktreePath, branch, true, checkout == WorktreeCheckoutShallow)
	e.worktreeMu.Unlock()

	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return "", nil, fmt.Errorf("failed to add worktree: %w", err)
	}

	cleanup := func() {
		// Use context.Background for cleanup to ensure it runs even if ctx is canceled
		cleanupCtx := context.Background()
		removeErr := e.RemoveWorktree(cleanupCtx, worktreePath)
		_ = os.RemoveAll(tmpDir)
		// If worktree removal failed, prune to clean up any dangling entries.
		// This prevents stale entries from accumulating and causing "commondir" errors
		// in subsequent worktree operations, especially on busy build servers.
		if removeErr != nil {
			_ = e.git.PruneWorktrees(cleanupCtx)
		}
	}

	return worktreePath, cleanup, nil
}

// RegisterWorktree registers a worktree for a stack root in local git refs
func (e *engineImpl) RegisterWorktree(stackRoot string, path string) error {
	return e.RegisterWorktreeWithName(stackRoot, path, "")
}

// RegisterWorktreeWithName registers a worktree with a user-friendly name
func (e *engineImpl) RegisterWorktreeWithName(anchorBranch string, path string, name string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	meta := &git.WorktreeMeta{
		Name:         name,
		Path:         absPath,
		AnchorBranch: anchorBranch,
		CreatedAt:    timeNow(),
		MainRepoDir:  e.repoRoot,
	}

	return e.git.WriteWorktreeMeta(anchorBranch, meta)
}

// UnregisterWorktree removes worktree registration for a stack root
func (e *engineImpl) UnregisterWorktree(stackRoot string) error {
	return e.git.DeleteWorktreeMeta(stackRoot)
}

// GetWorktreeForStack returns worktree info for a stack root, or nil if none
func (e *engineImpl) GetWorktreeForStack(stackRoot string) (*WorktreeInfo, error) {
	meta, err := e.git.ReadWorktreeMeta(stackRoot)
	if err != nil {
		return nil, err
	}
	if meta == nil {
		return nil, nil
	}

	return &WorktreeInfo{
		Name:         meta.Name,
		Path:         meta.Path,
		AnchorBranch: meta.AnchorBranch,
		CreatedAt:    meta.CreatedAt,
		MainRepoDir:  meta.MainRepoDir,
	}, nil
}

// ListManagedWorktrees returns all stackit-managed worktrees, sorted by stack root name
func (e *engineImpl) ListManagedWorktrees() ([]WorktreeInfo, error) {
	metas, err := e.git.ListWorktreeMetas()
	if err != nil {
		return nil, err
	}

	// Sort keys for deterministic output
	keys := make([]string, 0, len(metas))
	for k := range metas {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	result := make([]WorktreeInfo, 0, len(metas))
	for _, k := range keys {
		meta := metas[k]
		result = append(result, WorktreeInfo{
			Name:         meta.Name,
			Path:         meta.Path,
			AnchorBranch: meta.AnchorBranch,
			CreatedAt:    meta.CreatedAt,
			MainRepoDir:  meta.MainRepoDir,
		})
	}

	return result, nil
}

// GetStackRootForBranch returns the stack root for a given branch.
// The stack root is the first ancestor branch whose parent is trunk.
// Returns empty string for trunk or untracked branches.
func (e *engineImpl) GetStackRootForBranch(branch Branch) string {
	e.mu.RLock()
	defer e.mu.RUnlock()

	branchName := branch.GetName()

	// Trunk has no stack root
	if branchName == e.trunk {
		return ""
	}

	// Check if branch is tracked at all
	if !e.branchState.HasByName(branchName) {
		return "" // Untracked branch has no stack root
	}

	current := branchName
	for {
		state := e.branchState.GetByName(current)
		if state == nil {
			// Should not happen since we checked above, but handle gracefully
			return ""
		}

		// If parent is trunk, current is the stack root
		if state.Parent == e.trunk {
			return current
		}

		current = state.Parent
	}
}

// IsInManagedWorktree checks if the current directory is a stackit-managed worktree.
// Returns true and worktree info if in a managed worktree, false otherwise.
func (e *engineImpl) IsInManagedWorktree() (bool, *WorktreeInfo, error) {
	// Check if .git is a file (worktree) vs directory (main repo)
	gitPath := filepath.Join(e.repoRoot, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil, nil // Not in a git repo
		}
		return false, nil, fmt.Errorf("failed to stat .git: %w", err)
	}

	// If .git is a directory, we're in the main repo, not a worktree
	if info.IsDir() {
		return false, nil, nil
	}

	// .git is a file - we're in a worktree. Now check if it's stackit-managed.
	// Get the current working directory (worktree path)
	currentPath, err := filepath.Abs(e.repoRoot)
	if err != nil {
		return false, nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	// List all managed worktrees and check if current path matches
	worktrees, err := e.ListManagedWorktrees()
	if err != nil {
		return false, nil, fmt.Errorf("failed to list managed worktrees: %w", err)
	}

	for _, wt := range worktrees {
		// Compare paths (normalize both)
		wtPath, err := filepath.Abs(wt.Path)
		if err != nil {
			continue
		}
		if wtPath == currentPath {
			return true, &WorktreeInfo{
				Name:         wt.Name,
				Path:         wt.Path,
				AnchorBranch: wt.AnchorBranch,
				CreatedAt:    wt.CreatedAt,
				MainRepoDir:  wt.MainRepoDir,
			}, nil
		}
	}

	// It's a worktree but not managed by stackit
	return false, nil, nil
}

// MarkNeedsPRBodyUpdate marks a branch as needing PR body update during next sync
func (e *engineImpl) MarkNeedsPRBodyUpdate(branchName string) error {
	localMeta, err := e.git.ReadLocalMetadata(branchName)
	if err != nil {
		localMeta = &git.LocalMeta{}
	}
	localMeta.NeedsPRBodyUpdate = true
	return e.git.WriteLocalMetadata(branchName, localMeta)
}

// ClearNeedsPRBodyUpdate clears the PR body update flag for a branch
func (e *engineImpl) ClearNeedsPRBodyUpdate(branchName string) error {
	localMeta, err := e.git.ReadLocalMetadata(branchName)
	if err != nil {
		// Best effort - if we can't read metadata, nothing to clear
		return nil //nolint:nilerr
	}
	if localMeta == nil || !localMeta.NeedsPRBodyUpdate {
		return nil // Nothing to clear
	}
	localMeta.NeedsPRBodyUpdate = false
	return e.git.WriteLocalMetadata(branchName, localMeta)
}

// GetBranchesNeedingPRBodyUpdate returns all branches that need PR body updates
func (e *engineImpl) GetBranchesNeedingPRBodyUpdate() []string {
	allBranches := e.AllBranches()
	branchNames := make([]string, len(allBranches))
	for i, b := range allBranches {
		branchNames[i] = b.GetName()
	}

	localMetas := e.git.BatchReadLocalMetadata(branchNames)
	var result []string
	for name, meta := range localMetas {
		if meta != nil && meta.NeedsPRBodyUpdate {
			result = append(result, name)
		}
	}
	return result
}

// GetStackDescription returns the stack description for a branch's stack.
// It reads from the stack ref using the branch's StackID.
// Returns nil for untracked branches or if the stack has no description.
func (e *engineImpl) GetStackDescription(branch Branch) *git.StackDescription {
	stackID := e.GetStackID(branch)
	if stackID == "" {
		return nil
	}

	stackMeta, err := e.git.ReadStackMeta(stackID)
	if err != nil || stackMeta == nil {
		return nil
	}

	return stackMeta.StackDescription()
}

// SetStackDescription sets the stack description in the stack ref for a branch.
// Returns an error if the branch is not part of a tracked stack.
func (e *engineImpl) SetStackDescription(_ context.Context, branch Branch, desc *git.StackDescription) error {
	stackID := e.GetStackID(branch)
	if stackID == "" {
		return fmt.Errorf("branch %s is not part of a tracked stack", branch.GetName())
	}

	// Read existing stack meta or create new one
	stackMeta, err := e.git.ReadStackMeta(stackID)
	if err != nil {
		return fmt.Errorf("failed to read stack metadata for %s: %w", stackID, err)
	}

	if stackMeta == nil {
		stackMeta = &git.StackMeta{
			ID:        stackID,
			CreatedAt: timeNow(),
		}
	}

	// Update title and description
	if desc != nil {
		stackMeta.Title = desc.Title
		stackMeta.Description = desc.Description
	} else {
		stackMeta.Title = ""
		stackMeta.Description = ""
	}

	if err := e.git.WriteStackMeta(stackID, stackMeta); err != nil {
		return fmt.Errorf("failed to write stack metadata for %s: %w", stackID, err)
	}

	return nil
}

// ClearStackDescription removes the stack description from the stack ref.
func (e *engineImpl) ClearStackDescription(ctx context.Context, branch Branch) error {
	return e.SetStackDescription(ctx, branch, nil)
}

// GenerateStackID creates a new stack ID for a new stack.
// Format: {timestamp-nanos}-{sanitized-root-branch}
func (e *engineImpl) GenerateStackID(rootBranch string) string {
	timestamp := timeNow().UnixNano()
	sanitized := sanitizeBranchNameForStackID(rootBranch)
	return fmt.Sprintf("%d-%s", timestamp, sanitized)
}

// sanitizeBranchNameForStackID converts a branch name into a safe suffix for stack IDs.
// Only allows alphanumeric characters and hyphens to ensure cross-platform compatibility
// with Git ref names. Limited to 50 chars to keep ref names reasonable and avoid
// filesystem path length issues on Windows.
func sanitizeBranchNameForStackID(branchName string) string {
	// Single-pass: replace unsafe chars with hyphens and collapse consecutive hyphens
	var b strings.Builder
	b.Grow(len(branchName))
	prevHyphen := true // Start true to skip leading hyphens

	for _, r := range branchName {
		isAlphaNum := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if isAlphaNum {
			b.WriteRune(r)
			prevHyphen = false
		} else if !prevHyphen {
			b.WriteRune('-')
			prevHyphen = true
		}
	}

	result := b.String()

	// Limit length first, then trim trailing hyphen once
	if len(result) > 50 {
		result = result[:50]
	}
	result = strings.TrimSuffix(result, "-")

	// Fallback for empty result (branch name was all special chars)
	if result == "" {
		result = "stack"
	}

	return result
}

// GetStackID returns the stack ID for a branch.
// Returns empty string for untracked branches or trunk.
// For legacy branches without StackID, derives it from the stack root.
func (e *engineImpl) GetStackID(branch Branch) string {
	branchName := branch.GetName()

	// Trunk has no stack ID
	if branchName == e.trunk {
		return ""
	}

	// Check if branch is tracked
	if !e.IsTracked(branch) {
		return ""
	}

	// Read branch metadata
	meta, err := e.git.ReadMetadata(branchName)
	if err != nil {
		return ""
	}

	// Return explicit StackID if present
	if meta.StackID != nil && *meta.StackID != "" {
		return *meta.StackID
	}

	// Legacy fallback: derive stack ID from stack root
	rootName := e.GetStackRootForBranch(branch)
	if rootName == "" {
		return ""
	}

	rootMeta, err := e.git.ReadMetadata(rootName)
	if err != nil || rootMeta == nil || rootMeta.StackID == nil {
		return ""
	}

	return *rootMeta.StackID
}

// SetStackID sets the stack ID on a branch's metadata.
func (e *engineImpl) SetStackID(ctx context.Context, branch Branch, stackID string) error {
	branchName := branch.GetName()

	return e.WithRetry(ctx, func() error {
		meta, err := e.git.ReadMetadata(branchName)
		if err != nil {
			return fmt.Errorf("failed to read metadata: %w", err)
		}
		if meta == nil {
			meta = &git.Meta{}
		}

		meta.StackID = &stackID

		tx := e.BeginTx(fmt.Sprintf("set stack ID: %s -> %s", branchName, stackID))
		if err := tx.UpdateMeta(branchName, meta); err != nil {
			return err
		}
		return tx.Commit(ctx)
	})
}

// CreateStackRef creates a new stack ref with the given metadata.
func (e *engineImpl) CreateStackRef(stackID string, meta *git.StackMeta) error {
	if meta == nil {
		meta = &git.StackMeta{
			ID:        stackID,
			CreatedAt: timeNow(),
		}
	}
	return e.git.WriteStackMeta(stackID, meta)
}

// GetStackMeta returns the stack metadata for a stack ID.
func (e *engineImpl) GetStackMeta(stackID string) (*git.StackMeta, error) {
	return e.git.ReadStackMeta(stackID)
}
