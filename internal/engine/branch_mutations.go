package engine

import (
	"context"
	"fmt"
	"slices"

	"github.com/getstackit/stackit/internal/git"
)

// CreateBranch creates a new branch at the given start point
func (e *engineImpl) CreateBranch(ctx context.Context, branchName string, startPoint string) error {
	if err := e.git.CreateBranch(ctx, branchName, startPoint); err != nil {
		return err
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	e.addKnownBranchLocked(branchName)
	return nil
}

// ResetHard performs a hard reset to the given revision
func (e *engineImpl) ResetHard(ctx context.Context, revision string) error {
	return e.git.HardReset(ctx, revision)
}

// ResetMerge performs a merge reset to the given revision
func (e *engineImpl) ResetMerge(ctx context.Context, revision string) error {
	return e.git.ResetMerge(ctx, revision)
}

// SoftReset moves HEAD to the given revision, keeping the index and working
// tree intact (git reset --soft).
func (e *engineImpl) SoftReset(ctx context.Context, revision string) error {
	return e.git.SoftReset(ctx, revision)
}

// RebaseAbort aborts an in-progress rebase, restoring the pre-rebase HEAD.
func (e *engineImpl) RebaseAbort(ctx context.Context) error {
	return e.git.RebaseAbort(ctx)
}

// MergeAbort aborts an in-progress merge, restoring the pre-merge state.
func (e *engineImpl) MergeAbort(ctx context.Context) error {
	return e.git.MergeAbort(ctx)
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
	return e.FetchRemote(ctx, RemoteFetchRequest{
		Remote:   remote,
		Branches: []string{branch},
	})
}

// FetchRemote fetches branches and Stackit metadata refs from a remote in a
// single git fetch invocation.
func (e *engineImpl) FetchRemote(ctx context.Context, req RemoteFetchRequest) error {
	remote := req.Remote
	if remote == "" {
		remote = e.GetRemote()
	}

	refspecs := make([]string, 0, len(req.Branches)+2)
	seen := make(map[string]bool, len(req.Branches)+2)
	add := func(refspec string) {
		if refspec == "" || seen[refspec] {
			return
		}
		seen[refspec] = true
		refspecs = append(refspecs, refspec)
	}

	for _, branch := range req.Branches {
		if branch == "" {
			continue
		}
		add(git.BranchFetchRefspec(remote, branch))
	}
	if req.IncludeMetadata {
		add("+refs/stackit/metadata/*:refs/stackit/remote-metadata/*")
	}
	if req.IncludeStackMetadata {
		add("+refs/stackit/stacks/*:refs/stackit/remote-stacks/*")
	}

	return e.git.FetchRefSpecs(ctx, remote, refspecs)
}

// InteractiveRebase starts an interactive rebase
func (e *engineImpl) InteractiveRebase(ctx context.Context, onto string) error {
	return e.git.InteractiveRebase(ctx, onto)
}

// PushBranch pushes a branch to the remote
func (e *engineImpl) PushBranch(ctx context.Context, branch Branch, remote string, opts git.PushOptions) error {
	return e.git.PushBranch(ctx, branch.GetName(), remote, opts)
}

// PushBranches pushes multiple branches to the remote in a single git
// invocation, returning a per-branch result map (nil entry = success).
func (e *engineImpl) PushBranches(ctx context.Context, remote string, specs []git.PushSpec, opts git.PushOptions) map[string]error {
	return e.git.PushBranches(ctx, remote, specs, opts)
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

	moves := make([]BranchParentMove, len(children))
	for i, child := range children {
		moves[i] = BranchParentMove{Branch: child, NewParent: parent}
	}
	if err := e.validateLinearParentMovesAfterRemovals(moves, []string{branchName}); err != nil {
		return err
	}

	// Delete the head ref and both metadata refs atomically in one git invocation.
	// update-ref --stdin tolerates missing refs, so this is safe even if the
	// branch or its metadata refs were already absent.
	if err := e.git.DeleteRefsBatch(ctx, []string{
		"refs/heads/" + branchName,
		git.MetadataRefPrefix + branchName,
		git.LocalMetadataRefPrefix + branchName,
	}); err != nil {
		return fmt.Errorf("failed to delete branch refs: %w", err)
	}

	// Remove the deleted branch before reparenting so the regular linear guard
	// sees the same final graph the operation will leave on disk.
	e.mu.Lock()
	e.state.removeBranch(branchName)
	if i := slices.Index(e.state.branches, branchName); i >= 0 {
		e.state.setBranches(slices.Delete(e.state.branches, i, i+1))
	}
	e.mu.Unlock()

	// Reparent children to grandparent, preserving divergence points so
	// children don't carry the deleted branch's commits after restacking.
	parentBranch := e.GetBranch(parent)
	reparentErr := e.ReparentBranches(ctx, children, parentBranch)

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
// nearestSurvivingParent walks up from start through branches that are being
// deleted and returns the first one that will still exist afterwards.
//
// The walk only passes through members of toDelete, so it terminates in at most
// len(toDelete) steps. It is capped anyway: a parent cycle in malformed or
// concurrently rewritten metadata would otherwise spin forever and hang the
// whole deletion. GetStackRootForBranch and branchHeldBack guard their walks
// the same way. When a cycle leaves no surviving ancestor reachable, the child
// falls back to trunk rather than to a branch about to vanish.
func nearestSurvivingParent(start string, parentOf map[string]string, toDelete map[string]bool, trunk string) string {
	parent := start
	for range len(toDelete) + 1 {
		if !toDelete[parent] {
			return parent
		}
		parent = parentOf[parent]
	}
	return trunk
}

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

	// Resolve every surviving child directly to the nearest parent that will
	// remain after the batch deletion. This models the final topology in one
	// validation, rather than an intermediate graph containing deleted nodes.
	moves := make([]BranchParentMove, 0)
	allSurvivingChildren := make(map[string]bool)
	for _, b := range branches {
		name := b.GetName()
		for _, child := range childrenByBranch[name] {
			if toDeleteSet[child] {
				continue
			}
			newParent := nearestSurvivingParent(parentByBranch[name], parentByBranch, toDeleteSet, trunkName)
			moves = append(moves, BranchParentMove{Branch: child, NewParent: newParent})
			allSurvivingChildren[child] = true
		}
	}
	if err := e.validateLinearParentMovesAfterRemovals(moves, branches.Names()); err != nil {
		return nil, err
	}

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

	// Remove all deleted nodes before applying the moves, so the regular linear
	// validator sees the final graph rather than the just-deleted branches.
	e.mu.Lock()
	for name := range toDeleteSet {
		e.state.removeBranch(name)
		if i := slices.Index(e.state.branches, name); i >= 0 {
			e.state.setBranches(slices.Delete(e.state.branches, i, i+1))
		}
	}
	e.mu.Unlock()

	var reparentErr error
	if len(moves) > 0 {
		if err := e.ReparentBranchesToParents(ctx, moves); err != nil {
			reparentErr = fmt.Errorf("failed to reparent surviving children: %w", err)
		}
	}

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
	e.addKnownBranchLocked(branchName)

	return nil
}

// addKnownBranchLocked records a branch ref that was created through the
// engine. Callers must hold e.mu.
func (e *engineImpl) addKnownBranchLocked(branchName string) {
	if branchName == "" || slices.Contains(e.state.branches, branchName) {
		return
	}
	branches := append(slices.Clone(e.state.branches), branchName)
	slices.Sort(branches)
	e.state.setBranches(branches)
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

	// Best-effort metadata-ref rename. The branch rename already succeeded;
	// orphaned refs are reconciled by `sync`.
	_ = e.git.RenameMetadata(oldName, newName)

	oldLocalRef := fmt.Sprintf("%s%s", git.LocalMetadataRefPrefix, oldName)
	newLocalRef := fmt.Sprintf("%s%s", git.LocalMetadataRefPrefix, newName)
	if sha, err := e.git.GetRef(oldLocalRef); err == nil {
		if updateErr := e.git.UpdateRef(newLocalRef, sha); updateErr == nil {
			_ = e.git.DeleteRef(ctx, oldLocalRef)
		}
	}

	// Update children to point to the new branch name. Batch the reads and
	// stage every write in a single metadata transaction so a wide root costs
	// one atomic ref update instead of two git processes per child.
	if len(children) > 0 {
		childMetas, _ := e.batchReadMetadata(children)
		if err := e.withMetadataTx(ctx, fmt.Sprintf("rename %s to %s: reparent children", oldName, newName), func(tx *MetadataTx) error {
			for _, child := range children {
				childMeta := childMetas[child]
				if childMeta == nil {
					continue
				}
				if err := tx.UpdateMeta(child, childMeta.WithParentBranchName(&newName)); err != nil {
					return fmt.Errorf("update parent metadata for child %s: %w", child, err)
				}
			}
			return nil
		}); err != nil {
			return err
		}
	}

	// Rebuild in-memory state to be safe
	return e.rebuild()
}
