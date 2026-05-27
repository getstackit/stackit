package engine

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/getstackit/stackit/internal/git"
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

// DeleteBranch deletes a branch and its metadata
func (e *engineImpl) DeleteBranch(ctx context.Context, branch Branch) error {
	branchName := branch.GetName()
	if e.IsTrunk(branch) {
		return fmt.Errorf("cannot delete trunk branch")
	}

	// Refresh the actual HEAD branch before deciding whether deletion needs to
	// move the main worktree off the target branch. The git layer refuses to
	// raw-delete any checked-out branch, so relying on stale cached state here
	// would turn a recoverable current-branch delete into an error.
	e.CurrentBranch()

	// Get children and parent info under lock, then release for SetParent calls
	e.mu.Lock()

	// Get children before deletion
	children := make([]string, len(e.state.childrenMap[branchName]))
	copy(children, e.state.childrenMap[branchName])

	// Get parent
	parent := e.trunk
	if state := e.readState(branchName); state != nil {
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
	if err := e.git.DeleteMetadata(ctx, branchName); err != nil {
		_, _ = fmt.Fprintf(e.writer, "Warning: failed to delete metadata ref for %s: %v\n", branchName, err)
	}

	// Delete local metadata
	if err := e.git.DeleteRef(ctx, fmt.Sprintf("%s%s", git.LocalMetadataRefPrefix, branchName)); err != nil {
		_, _ = fmt.Fprintf(e.writer, "Warning: failed to delete local metadata ref for %s: %v\n", branchName, err)
	}

	// Reparent children to grandparent, preserving divergence points so
	// children don't carry the deleted branch's commits after restacking.
	parentBranch := e.GetBranch(parent)
	reparentErr := e.ReparentBranches(ctx, children, parentBranch)

	// Clean up in-memory cache for deleted branch
	e.mu.Lock()
	e.state.removeBranch(branchName)
	if i := slices.Index(e.state.branches, branchName); i >= 0 {
		e.state.setBranches(slices.Delete(e.state.branches, i, i+1))
	}
	e.mu.Unlock()

	// Rebuild engine state from disk so callers don't need to track when to call
	// eng.Rebuild() themselves. After this returns, GetBranch/GetParent/children
	// reflect the post-deletion state across all cached relationships.
	if err := e.rebuild(); err != nil {
		return fmt.Errorf("deleted branch %s but failed to rebuild engine state: %w", branchName, err)
	}

	if reparentErr != nil {
		return fmt.Errorf("deleted branch %s but failed to reparent child branch(es): %w", branchName, reparentErr)
	}

	return nil
}

// DeleteBranches deletes multiple branches and returns the children that need restacking.
//
// All ref deletions (heads, metadata, local-metadata) for every branch in the
// batch go through a single `git update-ref --stdin` invocation. The previous
// implementation looped DeleteBranch per branch, spawning 4+ git subprocesses
// per branch — at ~30-50ms each that dominated cleanup time during sync.
func (e *engineImpl) DeleteBranches(ctx context.Context, branches Branches) ([]string, error) {
	if len(branches) == 0 {
		return nil, nil
	}

	// Validate: no trunk in the batch.
	if slices.ContainsFunc(branches, func(b Branch) bool { return e.IsTrunk(b) }) {
		return nil, fmt.Errorf("cannot delete trunk branch")
	}

	// Snapshot in-memory state under one lock acquisition: children and parent
	// for each branch, and whether the current branch is being deleted.
	toDeleteSet := make(map[string]bool, len(branches))
	childrenByBranch := make(map[string][]string, len(branches))
	parentByBranch := make(map[string]string, len(branches))
	var needCheckoutTrunk bool

	// Keep e.currentBranch aligned with the real repository before deciding
	// whether the batch includes HEAD.
	e.CurrentBranch()

	e.mu.Lock()
	trunkName := e.trunk
	for _, b := range branches {
		name := b.GetName()
		toDeleteSet[name] = true
		if name == e.currentBranch {
			needCheckoutTrunk = true
		}
		childrenByBranch[name] = slices.Clone(e.state.childrenMap[name])
		parent := trunkName
		if state := e.readState(name); state != nil {
			parent = state.Parent
		}
		parentByBranch[name] = parent
	}
	e.mu.Unlock()

	// If the current branch is being deleted, switch to trunk first so we
	// don't end up detached after `update-ref -d refs/heads/<current>`.
	if needCheckoutTrunk {
		if err := e.git.CheckoutBranch(ctx, trunkName); err != nil {
			return nil, fmt.Errorf("failed to switch to trunk before deleting current branch: %w", err)
		}
		e.mu.Lock()
		e.currentBranch = e.trunk
		e.mu.Unlock()
	}

	// One ref-deletion batch covers heads + metadata + local-metadata for every
	// branch. update-ref --stdin tolerates missing refs (no oldvalue), so an
	// already-deleted head or absent local-metadata won't fail the batch.
	refsToDelete := make([]string, 0, 3*len(branches))
	for _, b := range branches {
		name := b.GetName()
		refsToDelete = append(refsToDelete,
			"refs/heads/"+name,
			git.MetadataRefPrefix+name,
			git.LocalMetadataRefPrefix+name,
		)
	}
	if err := e.git.DeleteRefsBatch(ctx, refsToDelete); err != nil {
		return nil, fmt.Errorf("failed to delete branch refs: %w", err)
	}

	// Reparent surviving children of each deleted branch to that branch's
	// parent. Children that are themselves being deleted in this batch are
	// skipped — callers are expected to pass branches in a topological order
	// (children before parents) so the parent already reflects the right
	// grandparent by the time we get here.
	allSurvivingChildren := make(map[string]bool)
	var reparentErr error
	for _, b := range branches {
		name := b.GetName()
		var surviving []string
		for _, child := range childrenByBranch[name] {
			if !toDeleteSet[child] {
				surviving = append(surviving, child)
				allSurvivingChildren[child] = true
			}
		}
		if len(surviving) == 0 {
			continue
		}
		parentBranch := e.GetBranch(parentByBranch[name])
		if err := e.ReparentBranches(ctx, surviving, parentBranch); err != nil {
			reparentErr = fmt.Errorf("failed to reparent children of %s: %w", name, err)
			break
		}
	}

	// Clean up in-memory state for the deleted branches.
	e.mu.Lock()
	for name := range toDeleteSet {
		e.state.removeBranch(name)
		if i := slices.Index(e.state.branches, name); i >= 0 {
			e.state.setBranches(slices.Delete(e.state.branches, i, i+1))
		}
	}
	e.mu.Unlock()

	childrenToRestack := make([]string, 0, len(allSurvivingChildren))
	for child := range allSurvivingChildren {
		childrenToRestack = append(childrenToRestack, child)
	}

	// Rebuild engine state from disk so callers don't need to track when to
	// call eng.Rebuild() themselves. One rebuild covers the entire batch.
	if rebuildErr := e.rebuild(); rebuildErr != nil {
		return childrenToRestack, fmt.Errorf("deleted branches but failed to rebuild engine state: %w", rebuildErr)
	}

	if reparentErr != nil {
		return childrenToRestack, reparentErr
	}
	return childrenToRestack, nil
}

// CheckoutBranch checks out an existing branch
func (e *engineImpl) CheckoutBranch(ctx context.Context, branch Branch) error {
	branchName := branch.GetName()
	if err := e.git.CheckoutBranch(ctx, branchName); err != nil {
		// If it's already used by another worktree, try checking out detached
		if branchCheckedOutInAnotherWorktree(err) {
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

func branchCheckedOutInAnotherWorktree(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "already used by worktree") ||
		strings.Contains(msg, "is already checked out")
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
	if !slices.Contains(e.state.branches, branchName) {
		e.state.setBranches(append(e.state.branches, branchName))
	}

	return nil
}

// RenameBranch renames a branch and its metadata
func (e *engineImpl) RenameBranch(ctx context.Context, oldBranch, newBranch Branch) error {
	oldName := oldBranch.GetName()
	newName := newBranch.GetName()

	e.mu.RLock()
	// Get children before renaming anything
	children := make([]string, len(e.state.childrenMap[oldName]))
	copy(children, e.state.childrenMap[oldName])
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
		if err := e.git.UpdateRef(newLocalRef, sha); err != nil {
			_, _ = fmt.Fprintf(e.writer, "Warning: failed to update new local metadata ref: %v\n", err)
		} else if err := e.git.DeleteRef(ctx, oldLocalRef); err != nil {
			_, _ = fmt.Fprintf(e.writer, "Warning: failed to delete old local metadata ref: %v\n", err)
		}
	}

	// Update children to point to the new branch name
	for _, child := range children {
		childMeta, err := e.readMetadata(child)
		if err != nil {
			continue
		}
		childMeta = childMeta.WithParentBranchName(&newName)
		if err := e.writeMetadata(child, childMeta); err != nil {
			continue
		}
	}

	// Rebuild in-memory state to be safe
	return e.rebuild()
}
