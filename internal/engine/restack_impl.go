package engine

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/getstackit/stackit/internal/git"
)

const (
	reflogRestack       = "stackit: restack"
	reflogRestackFrozen = "stackit: restack (frozen)"
	reflogRestackAnchor = "stackit: restack (anchor)"
)

// resetWorktreeIfClean resets a worktree's working directory to match its
// branch ref's new content, unless the worktree had local changes
// before this restack pass touched anything. dirtyWorktrees is captured once
// up front (see restackSnapshot.dirtyWorktrees) rather than checked here,
// because by the time any branch's reset runs, restack has already moved that
// branch's ref — checking dirtiness at that point would compare the
// worktree's now-stale content against its own new ref and always read as
// dirty, silently disabling the reset for every branch, not just genuinely
// dirty ones.
//
// Skipping the reset on a worktree with changes leaves it out of
// sync with its ref rather than discarding the user's work. That state is still
// a footgun — `git status` shows the new commit's files as deleted — so callers
// should avoid moving the ref at all in that case; see skipDirtyWorktreeStacks.
func (e *engineImpl) resetWorktreeIfClean(ctx context.Context, worktreePath string, snap *restackSnapshot) {
	if snap.dirtyWorktrees[worktreePath] {
		return
	}
	_ = e.git.ResetWorktreeWorkingDir(ctx, worktreePath) //nolint:errcheck // best-effort
}

// branchWorktreeResetTarget resolves the worktree holding branchName and
// reports whether it is safe to reset once the branch's ref moves. It is the
// one-off equivalent of the restack snapshot's dirtyWorktrees for paths that
// move a single ref outside a restack pass.
//
// This MUST be called before the ref moves. Asking afterwards compares the
// worktree's now-stale content against its own new ref, which always reads as
// dirty and silently disables the reset — the same trap resetWorktreeIfClean
// documents for the batch path.
//
// Best-effort: a worktree that cannot be inspected is left alone, since a reset
// is what would discard uncommitted work.
//
// Declining to reset is not a silent outcome. The ref still moves, so that
// worktree is left holding pre-move content under a moved ref — the divergence
// the user's next `stackit modify -a` commits as a deletion. The reason is
// returned so the caller can say so; see worktreeResetDecision.
func (e *engineImpl) branchWorktreeResetTarget(ctx context.Context, branchName string) worktreeResetDecision {
	worktrees, err := e.git.ListWorktrees(ctx)
	if err != nil {
		return worktreeResetDecision{}
	}
	worktreePath := worktrees.PathForBranch(branchName)
	if worktreePath == "" {
		return worktreeResetDecision{}
	}
	dirty, err := e.git.WorktreeHasTrackedChanges(ctx, worktreePath)
	switch {
	case err != nil:
		return worktreeResetDecision{HeldPath: worktreePath, HeldReason: fmt.Sprintf("it could not be inspected: %v", err)}
	case dirty:
		return worktreeResetDecision{HeldPath: worktreePath, HeldReason: "it has uncommitted changes"}
	}
	// Tracked changes are not the only thing a reset destroys.
	// WorktreeHasTrackedChanges deliberately ignores untracked files, so it
	// cannot see the one case `git reset --hard` does overwrite them: an
	// untracked file sitting at a path the incoming tree also contains. That
	// file was never in git, so there is no reflog or stash to recover it from.
	// The batch path guards this through untrackedCollisionHold; this one-off
	// path has to ask the same question.
	switch collides, known := e.UntrackedCollision(ctx, branchName); {
	case !known:
		return worktreeResetDecision{HeldPath: worktreePath, HeldReason: "its untracked files could not be inspected"}
	case collides:
		return worktreeResetDecision{HeldPath: worktreePath, HeldReason: "an untracked file there would be overwritten"}
	}
	return worktreeResetDecision{ResetPath: worktreePath}
}

// worktreeResetDecision is the outcome of branchWorktreeResetTarget: either a
// worktree that is safe to reset once the ref moves, or one that was left
// diverged and why. Both are empty when no worktree holds the branch.
type worktreeResetDecision struct {
	ResetPath  string
	HeldPath   string
	HeldReason string
}

// restackSnapshot bundles the branch-keyed state captured once per restack
// pass so per-branch steps don't re-read metadata, revisions, worktrees, or
// metadata-ref SHAs from git. It is mutated in place as branches are rebased
// so later branches in the same pass observe the updated values.
type restackSnapshot struct {
	meta MetaMap
	// revs is a read-through cache, not a source of truth. Read it via
	// engineImpl.branchRev rather than indexing it: a miss yields "", and an
	// empty SHA passed as a RefUpdate's OldSHA silently disables the
	// compare-and-swap in UpdateRefsBatch, turning a lost-update guard into a
	// blind overwrite. Never use a cached revision as a RefUpdate's *NewSHA* —
	// staleness there is not caught by the compare-and-swap and writes a wrong
	// value rather than failing.
	revs        RevisionMap
	worktrees   git.WorktreeList
	metaRefSHAs map[string]string
	// dirtyWorktrees records, per worktree path, whether it had changes to
	// local changes before this restack pass began. This cannot be checked
	// lazily; see resetWorktreeIfClean for why.
	dirtyWorktrees map[string]bool
	// untrackedByWorktree holds the untracked (non-ignored) paths found in
	// worktrees that had no tracked changes. Whether those hold their branch
	// depends on the incoming tree, which is only known per branch.
	untrackedByWorktree map[string][]string
	// heldReasons maps every branch whose checked-out worktree was dirty or
	// could not be inspected before this pass to the reason, phrased for the
	// user and naming the worktree. A held branch reports RestackUnneeded —
	// the same status as one that needed no work — so the reason is what lets
	// callers tell "I protected your work" from "there was nothing to do".
	heldReasons map[string]string
	// applyingBranches is the subset this restack pass will actually move. A
	// held ancestor only blocks a validated child when it is in this set: then
	// the child's plan may name the ancestor's would-be target SHA even though
	// its ref was held back. A dirty current branch already amended by modify or
	// squash is not being applied here, so descendants may safely use its
	// current ref.
	applyingBranches BranchNameSet
}

// untrackedCollisionHold decides whether a worktree holding only untracked
// files must still hold its branch back.
//
// `git reset --hard` silently overwrites an untracked file whose path the
// incoming commit also contains, and leaves every other untracked file alone.
// So the question is not "are there untracked files" — which is the ordinary
// state of having written a new file and not staged it — but "does any of them
// collide with what is about to land".
//
// The incoming tree is the branch's new base. A branch's own commits cannot
// introduce a colliding path: if they did, the file would be tracked rather
// than untracked. Parents are restacked before their children, so by the time
// this runs the parent ref already holds the tree that is about to arrive.
//
// The decision is recorded in the snapshot. If this branch is also being
// applied in the validated plan, its descendants must be held; its worktree
// must not be reset in either case.
func (e *engineImpl) untrackedCollisionHold(ctx context.Context, branch Branch, snap *restackSnapshot) string {
	branchName := branch.GetName()
	worktreePath := snap.worktrees.PathForBranch(branchName)
	if worktreePath == "" {
		return ""
	}
	untracked := snap.untrackedByWorktree[worktreePath]
	if len(untracked) == 0 {
		return ""
	}

	hold := func(reason string) string {
		snap.dirtyWorktrees[worktreePath] = true
		snap.heldReasons[branchName] = reason
		return reason
	}

	e.mu.RLock()
	state := e.readState(branchName)
	parent := e.trunk
	e.mu.RUnlock()
	if state != nil && state.Parent != "" {
		parent = state.Parent
	}
	baseRev, err := e.GetBranch(parent).GetRevision()
	if err != nil || baseRev == "" {
		// Cannot see what is about to land, so cannot rule out a collision.
		return hold(fmt.Sprintf("untracked files in worktree %s could not be checked against the incoming commit", worktreePath))
	}

	collides, known := e.git.TreeContainsAnyPath(ctx, baseRev, untracked)
	switch {
	case !known:
		return hold(fmt.Sprintf("untracked files in worktree %s could not be checked against the incoming commit", worktreePath))
	case collides:
		return hold(fmt.Sprintf("worktree %s has untracked files the incoming commit would overwrite", worktreePath))
	}
	return ""
}

// holdBranchInOwnWorktree is the hold decision for the ordinary rebase path.
// That path rebases onto the parent's current ref, so a dirty ancestor does not
// make this branch unsafe to move. Only this branch's own worktree can need
// protection from the reset that follows its ref update.
// It returns the reason the branch is held, or "" when it is free to move.
func (e *engineImpl) holdBranchInOwnWorktree(ctx context.Context, branch Branch, snap *restackSnapshot) string {
	if reason := snap.heldReasons[branch.GetName()]; reason != "" {
		return reason
	}
	return e.untrackedCollisionHold(ctx, branch, snap)
}

// holdBranch is the hold decision for the validated-plan application path: an
// ancestor already held, or this branch's own worktree holding something the
// incoming tree would destroy. A validated plan applies a parent's would-be
// target SHA, so applying a child after holding its parent would record a
// parent revision the child cannot actually be based on.
// It returns the reason the branch is held, or "" when it is free to move.
func (e *engineImpl) holdBranch(ctx context.Context, branch Branch, snap *restackSnapshot) string {
	if reason := e.branchHeldBack(branch, snap); reason != "" {
		return reason
	}
	return e.untrackedCollisionHold(ctx, branch, snap)
}

// branchHeldBack returns why branch cannot move — its own worktree, or a held
// ancestor this pass is also applying — or "" when nothing holds it.
func (e *engineImpl) branchHeldBack(branch Branch, snap *restackSnapshot) string {
	start := branch.GetName()
	current := start
	visited := make(map[string]bool)
	// Metadata should be acyclic, but cap the walk as a second line of defense
	// against malformed or concurrently changing parent metadata — a parent
	// loop here would hang the whole restack pass. See GetStackRootForBranch,
	// which guards its own walk the same way.
	maxSteps := e.BranchNames().Len() + 1
	for range maxSteps {
		if current == "" {
			return ""
		}
		if visited[current] {
			// A cycle means the metadata cannot be trusted to say what this
			// branch is stacked on, so no ref move is safe: hold it.
			return "its recorded parent chain contains a cycle, so what it is stacked on cannot be determined"
		}
		visited[current] = true
		if reason := snap.heldReasons[current]; reason != "" && snap.applyingBranches.Contains(current) {
			if current == start {
				return reason
			}
			return fmt.Sprintf("its ancestor %s is held back: %s", current, reason)
		}
		e.mu.RLock()
		state := e.readState(current)
		trunk := e.trunk
		e.mu.RUnlock()
		if state == nil || state.Parent == "" || current == trunk {
			return ""
		}
		current = state.Parent
	}
	// Ran out of steps without reaching trunk: the chain is longer than the
	// branch count allows, so treat it as untrustworthy too.
	return "its recorded parent chain never reaches trunk, so what it is stacked on cannot be determined"
}

// branchRev returns branch's revision from the pass snapshot, falling back to a
// live read when it isn't cached.
//
// This exists so the fallback can't be forgotten. Indexing snap.revs directly
// yields "" for an absent branch, and an empty OldSHA makes UpdateRefsBatch skip
// its compare-and-swap entirely — the lost-update hole #1487 closed. Routing
// every cached revision read through here keeps that a single decision rather
// than one repeated at each call site.
//
// Only safe for values used as a compare-and-swap operand or a comparison: if
// the cached SHA is stale the swap fails, which is noisy but correct. A value
// being *written* (a RefUpdate's NewSHA) must be read live — see
// restackWorktreeAnchor.
func (e *engineImpl) branchRev(snap *restackSnapshot, branch Branch) (string, error) {
	if rev, ok := snap.revs.Rev(branch.GetName()); ok && rev != "" {
		return rev, nil
	}
	return branch.GetRevision()
}

// restackWorktreeAnchor fast-forwards a worktree anchor to trunk instead of
// rebasing it. handled is false for any branch that isn't an anchor, so callers
// fall through to the normal path.
//
// An anchor carries no commits of its own, so there is nothing to replay: the
// ref and its recorded parent revision are moved to trunk in one atomic update.
func (e *engineImpl) restackWorktreeAnchor(
	ctx context.Context,
	branch Branch,
	snap *restackSnapshot,
) (result RestackBranchResult, handled bool, err error) {
	if !e.IsWorktreeAnchor(branch) {
		return RestackBranchResult{}, false, nil
	}
	branchName := branch.GetName()

	// Read live, not from snap.revs: trunkRev is written as the anchor ref's
	// NewSHA below. A stale cached value would point the anchor at an old trunk
	// commit, and the compare-and-swap guards the anchor's own ref rather than
	// the value going into it, so nothing would catch it. One read per anchor,
	// and there is one anchor per worktree.
	trunkRev, err := e.Trunk().GetRevision()
	if err != nil {
		return RestackBranchResult{Result: RestackConflict}, true, fmt.Errorf("failed to get trunk revision for anchor %s: %w", branchName, err)
	}

	// Safe to read cached: only compared against trunkRev and used as OldSHA.
	anchorRev, err := e.branchRev(snap, branch)
	if err != nil {
		return RestackBranchResult{Result: RestackConflict}, true, fmt.Errorf("failed to get anchor revision for %s: %w", branchName, err)
	}

	// If already at trunk, nothing to do
	if anchorRev == trunkRev {
		return RestackBranchResult{Result: RestackUnneeded}, true, nil
	}

	// Get current metadata SHA for optimistic locking
	oldMetadataSHA := snap.metaRefSHAs[branchName]

	// Prepare metadata update with new parent revision
	meta, err := e.readMetadata(branchName)
	if err != nil {
		meta = git.NewMeta()
	}
	meta = meta.WithParentBranchRevision(&trunkRev)
	metadataJSON, err := json.Marshal(meta)
	if err != nil {
		return RestackBranchResult{Result: RestackConflict}, true, fmt.Errorf("failed to marshal metadata for anchor %s: %w", branchName, err)
	}
	metadataSHA, err := e.git.CreateBlob(string(metadataJSON))
	if err != nil {
		return RestackBranchResult{Result: RestackConflict}, true, fmt.Errorf("failed to prepare metadata blob for anchor %s: %w", branchName, err)
	}

	// Atomic update of both branch ref and metadata ref
	updates := []git.RefUpdate{
		{RefName: fmt.Sprintf("refs/heads/%s", branchName), NewSHA: trunkRev, OldSHA: anchorRev},
		{RefName: fmt.Sprintf("%s%s", git.MetadataRefPrefix, branchName), NewSHA: metadataSHA, OldSHA: oldMetadataSHA},
	}
	if err := e.git.UpdateRefsBatchWithLog(ctx, updates, reflogRestackAnchor); err != nil {
		return RestackBranchResult{Result: RestackConflict}, true, fmt.Errorf("failed to update refs atomically for anchor %s: %w", branchName, err)
	}

	// If the anchor branch is currently checked out in this context, reset the working tree
	current := e.CurrentBranch()
	if current != nil && current.GetName() == branchName {
		if err := e.git.HardReset(ctx, "HEAD"); err != nil {
			return RestackBranchResult{Result: RestackConflict}, true, fmt.Errorf("failed to reset working tree for anchor %s: %w", branchName, err)
		}
	} else {
		// If the branch is checked out in a different worktree, reset that worktree.
		if worktreePath := snap.worktrees.PathForBranch(branchName); worktreePath != "" {
			e.resetWorktreeIfClean(ctx, worktreePath, snap)
		}
	}

	return RestackBranchResult{
		Result:            RestackDone,
		RebasedBranchBase: trunkRev,
		NewRev:            trunkRev,
	}, true, nil
}

// restackBranch rebases a branch onto its parent
// If the parent has been merged/deleted, it will automatically reparent to the nearest valid ancestor.
// snap is the point-in-time state captured by the caller — passing it avoids
// spawning `git worktree list` and `git rev-parse <metadata ref>` per branch.
func (e *engineImpl) restackBranch(
	ctx context.Context,
	branch Branch,
	snap *restackSnapshot,
	squashCache *git.SquashMergeCache,
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

	// Hold the branch back entirely when its worktree has uncommitted tracked
	// changes. Moving the ref is only half the operation: the worktree has to be
	// reset to match, and resetWorktreeIfClean deliberately refuses to do that
	// while there is work to lose. Doing the first half without the second
	// leaves the worktree on the old commit's content under a new ref, so
	// `git status` there reports the new commit's files as deleted and the next
	// `stackit modify -a` commits that deletion — silently reverting those files
	// inside the branch.
	//
	// The validated-plan path has a matching guard that also holds descendants:
	// it applies planned parent targets rather than the parent's current ref.
	// The ordinary rebase path only needs this branch's own hold, which keeps
	// modify and squash able to restack clean children after moving their dirty
	// current branch's ref.
	if reason := e.holdBranchInOwnWorktree(ctx, branch, snap); reason != "" {
		return RestackBranchResult{Result: RestackUnneeded, HeldBy: reason}, nil
	}

	// Worktree anchors are handled before the landed check, mirroring
	// planRestackBranch. An anchor holds no commits of its own -- it marks where
	// trunk was when the worktree was created -- so it is always an ancestor of
	// trunk and branchLanded always reports it as landed. Checking that first
	// meant the anchor returned "unneeded" before ever reaching the
	// fast-forward below, so it never moved, and every consumer that measures a
	// child against its recorded parent drifted further with each trunk advance.
	if anchorResult, handled, err := e.restackWorktreeAnchor(ctx, branch, snap); handled {
		return anchorResult, err
	}

	if e.branchLanded(ctx, branchName, parent, squashCache) {
		return RestackBranchResult{Result: RestackUnneeded}, nil
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

		localSha, err := e.branchRev(snap, branch)
		if err != nil {
			return RestackBranchResult{Result: RestackConflict}, fmt.Errorf("failed to get local revision for frozen branch %s: %w", branchName, err)
		}
		if localSha != remoteSha {
			// Get parent revision for metadata update
			parentBranch := e.GetBranch(parent)
			parentRev, err := e.branchRev(snap, parentBranch)
			if err != nil {
				return RestackBranchResult{Result: RestackConflict}, fmt.Errorf("failed to get parent revision for %s: %w", branchName, err)
			}

			// Get current metadata SHA for optimistic locking
			oldMetadataSHA := snap.metaRefSHAs[branchName]

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
			if err := e.git.UpdateRefsBatchWithLog(ctx, updates, reflogRestackFrozen); err != nil {
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
				if worktreePath := snap.worktrees.PathForBranch(branchName); worktreePath != "" {
					e.resetWorktreeIfClean(ctx, worktreePath, snap)
				}
			}

			return RestackBranchResult{
				Result:            RestackDone,
				RebasedBranchBase: remoteSha,
				NewRev:            remoteSha,
			}, nil
		}

		return RestackBranchResult{Result: RestackUnneeded, Frozen: true}, nil
	}

	// Track reparenting info
	var reparented bool
	var oldParent string

	// Check if parent needs reparenting (merged, deleted, or has MERGED PR state)
	e.mu.RLock()
	needsReparent := e.shouldReparentBranch(ctx, parent, snap.meta, squashCache)
	e.mu.RUnlock()

	if needsReparent {
		oldParent = parent

		// Find nearest valid ancestor
		e.mu.RLock()
		newParent := e.findNearestValidAncestor(ctx, branchName, snap.meta, squashCache)
		e.mu.RUnlock()

		// Reparent to the nearest valid ancestor. DivergenceRecompute is
		// intentional: the old parent was merged/deleted, so SetParent's
		// shouldUpdateRevision logic correctly preserves the existing
		// divergence point when the old parent was merged into the new parent.
		// The subsequent restack then rebases the branch's own commits onto
		// the new parent.
		if err := e.SetParent(ctx, e.GetBranch(branchName), e.GetBranch(newParent), DivergenceRecompute); err != nil {
			return RestackBranchResult{Result: RestackConflict}, fmt.Errorf("failed to reparent %s to %s: %w", branchName, newParent, err)
		}
		parent = newParent
		reparented = true

		// Capture the old parent in merged history (best-effort, don't fail on error).
		// appendMergedDownstack reads fresh from disk and updates metaMap.
		_ = e.appendMergedDownstack(branchName, oldParent, snap.meta)

		// SetParent + appendMergedDownstack bumped the metadata ref. Refresh
		// the cached SHA so the later optimistic-locking UpdateRefsBatch
		// doesn't fail with "is at X but expected Y".
		if sha, refErr := e.git.GetRef(git.MetadataRefPrefix + branchName); refErr == nil {
			snap.metaRefSHAs[branchName] = sha
		}
	}

	// Get parent revision (needed for rebasedBranchBase even if restack is unneeded)
	parentBranch := e.GetBranch(parent)
	parentRev, err := e.branchRev(snap, parentBranch)
	if err != nil {
		return RestackBranchResult{Result: RestackConflict, RebasedBranchBase: parentRev}, fmt.Errorf("failed to get parent revision: %w", err)
	}

	// A worktree anchor is a hidden marker at trunk. When a branch parented
	// under an anchor is restacked in isolation (e.g. EnterConflictWorkflow
	// restacking just the conflict branch), the anchor ref can still lag trunk
	// because it isn't part of this restack set. PlanRestack substitutes trunk
	// for a stale anchor parent (see restack_plan.go); restackBranch must too.
	// Without it, parentRev stays at the anchor's stale tip, the check below
	// finds parentRev == oldParentRev and skips the rebase as "unneeded" — while
	// validation (which used trunk) predicted a conflict. That mismatch trips
	// EnterConflictWorkflow's "expected conflict but rebase completed
	// successfully" assertion and leaves the branch un-restacked.
	rebaseOnto := parent
	// Measure the other end of the replay range against trunk too. An anchor is
	// never the branch's real rebase base, and its own revision is always a
	// trunk commit -- so it stays an ancestor of any branch sitting on trunk and
	// the is-ancestor check below cannot spot that it has gone stale. Taking the
	// merge base against the anchor while rebasing onto trunk puts every trunk
	// commit since the anchor was created inside the range, which
	// .claude/rules/multiplayer-safety.md forbids. Mirrors planRestackBranch.
	rebaseBase := parent
	if e.IsWorktreeAnchor(parentBranch) {
		rebaseBase = e.trunk
		if trunkRev, trunkErr := e.Trunk().GetRevision(); trunkErr == nil && trunkRev != parentRev {
			parentRev = trunkRev
			rebaseOnto = trunkRev
		}
	}

	// Get metadata (read once to avoid duplicate disk I/O)
	meta := snap.meta.Get(branchName)
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
		if isAncestor, _ := e.git.IsAncestor(ctx, oldParentRev, branchName); !isAncestor {
			if mergeBase, err := e.git.GetMergeBase(ctx, branchName, rebaseBase); err == nil {
				oldParentRev = mergeBase
			}
		}
	} else {
		// No old parent revision in metadata, try to find merge base
		if mergeBase, err := e.git.GetMergeBase(ctx, branchName, rebaseBase); err == nil {
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
	gitResult, err := e.git.Rebase(ctx, branchName, rebaseOnto, oldParentRev)
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
	oldBranchSHA, err := branch.GetRevision()
	if err != nil {
		return RestackBranchResult{
			Result:            RestackConflict,
			RebasedBranchBase: parentRev,
			Reparented:        reparented,
			OldParent:         oldParent,
			NewParent:         parent,
		}, fmt.Errorf("failed to get current branch revision for %s: %w", branchName, err)
	}
	oldMetadataSHA := snap.metaRefSHAs[branchName]

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
	if worktreePath := snap.worktrees.PathForBranch(branchName); worktreePath != "" {
		e.resetWorktreeIfClean(ctx, worktreePath, snap)
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
	if err := e.git.UpdateRefsBatchWithLog(ctx, updates, reflogRestack); err != nil {
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
	if snap.meta != nil {
		if updatedMeta, err := e.readMetadata(branchName); err == nil {
			snap.meta[branchName] = updatedMeta
		}
	}

	return RestackBranchResult{
		Result:              RestackDone,
		RebasedBranchBase:   parentRev,
		NewRev:              newRev,
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
	snap *restackSnapshot,
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
		return e.applyPlannedRefUpdate(ctx, branch, item, snap)
	case RestackPlanApplyMetadataRefresh:
		return e.applyMetadataRefresh(ctx, branch, item, snap)
	case RestackPlanApplyValidated:
		return e.applyValidatedRestack(ctx, branch, validation, item, snap)
	default:
		return RestackBranchResult{Result: RestackConflict}, fmt.Errorf("unknown restack plan action for %s", branchName)
	}
}

func (e *engineImpl) applyValidatedRestack(
	ctx context.Context,
	branch Branch,
	validation *RebaseValidation,
	item RestackPlanItem,
	snap *restackSnapshot,
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
	if parentRev == "" && snap.revs != nil {
		parentRev = snap.revs[item.NewParent]
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

	result, err := e.applyBranchAndMetadata(ctx, branch, item, newRev, parentRev, snap)
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
	snap *restackSnapshot,
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

	return e.applyBranchAndMetadata(ctx, branch, item, item.TargetRev, parentRev, snap)
}

// applyMetadataRefresh corrects a branch's recorded parent revision without
// touching its branch ref or working tree. It runs when planRestackBranch
// finds the branch already sitting exactly on its parent's tip but the
// recorded parentBranchRevision has drifted (never set, or stale from manual
// git operations) — nothing needs to be rebased, only the record fixed, so
// only the metadata ref moves and the result reports RestackUnneeded.
func (e *engineImpl) applyMetadataRefresh(
	ctx context.Context,
	branch Branch,
	item RestackPlanItem,
	snap *restackSnapshot,
) (RestackBranchResult, error) {
	branchName := branch.GetName()
	// Metadata records the parent SHA consumed by later planning, so advancing
	// it past a held parent would create the same phantom-parent state as a ref
	// move. A held branch and every descendant must remain completely unchanged.
	if reason := e.holdBranch(ctx, branch, snap); reason != "" {
		return RestackBranchResult{Result: RestackUnneeded, RebasedBranchBase: item.ParentRev, HeldBy: reason}, nil
	}
	meta := snap.meta.Get(branchName)
	if meta == nil {
		var err error
		meta, err = e.readMetadata(branchName)
		if err != nil {
			return RestackBranchResult{Result: RestackConflict, RebasedBranchBase: item.ParentRev}, fmt.Errorf("failed to read metadata: %w", err)
		}
	}

	oldMetadataSHA := snap.metaRefSHAs[branchName]
	parentRev := item.ParentRev
	updatedMeta := meta.WithParentBranchRevision(&parentRev)
	metadataJSON, err := json.Marshal(updatedMeta)
	if err != nil {
		return RestackBranchResult{Result: RestackConflict, RebasedBranchBase: parentRev}, fmt.Errorf("failed to marshal metadata: %w", err)
	}

	metadataSHA, err := e.git.CreateBlob(string(metadataJSON))
	if err != nil {
		return RestackBranchResult{Result: RestackConflict, RebasedBranchBase: parentRev}, fmt.Errorf("failed to prepare metadata blob: %w", err)
	}

	updates := []git.RefUpdate{
		{RefName: fmt.Sprintf("%s%s", git.MetadataRefPrefix, branchName), NewSHA: metadataSHA, OldSHA: oldMetadataSHA},
	}
	if err := e.git.UpdateRefsBatchWithLog(ctx, updates, reflogRestack); err != nil {
		return RestackBranchResult{Result: RestackConflict, RebasedBranchBase: parentRev}, fmt.Errorf("failed to update metadata ref: %w", err)
	}

	if snap.meta != nil {
		snap.meta[branchName] = updatedMeta
	}

	return RestackBranchResult{
		Result:            RestackUnneeded,
		RebasedBranchBase: parentRev,
	}, nil
}

func (e *engineImpl) applyBranchAndMetadata(
	ctx context.Context,
	branch Branch,
	item RestackPlanItem,
	newRev string,
	parentRev string,
	snap *restackSnapshot,
) (RestackBranchResult, error) {
	branchName := branch.GetName()

	// See the matching guard in restackBranch: moving the ref without resetting
	// the worktree leaves a staged revert that the next `modify -a` commits, and
	// resetWorktreeIfClean refuses to reset while there is uncommitted work.
	// Hold the branch back instead. This is the plan path, reached by every
	// caller of actions.RestackBranches (modify, squash, absorb, delete, split,
	// reorder, pluck) as well as the `restack` command.
	if reason := e.holdBranch(ctx, branch, snap); reason != "" {
		return RestackBranchResult{Result: RestackUnneeded, RebasedBranchBase: parentRev, HeldBy: reason}, nil
	}

	meta := snap.meta.Get(branchName)
	if meta == nil {
		var err error
		meta, err = e.readMetadata(branchName)
		if err != nil {
			return RestackBranchResult{Result: RestackConflict, RebasedBranchBase: parentRev}, fmt.Errorf("failed to read metadata: %w", err)
		}
	}

	oldBranchSHA, err := e.branchRev(snap, branch)
	if err != nil {
		return RestackBranchResult{Result: RestackConflict, RebasedBranchBase: parentRev}, fmt.Errorf("failed to get current branch revision for %s: %w", branchName, err)
	}
	oldMetadataSHA := snap.metaRefSHAs[branchName]

	updatedMeta := meta.WithParentBranchRevision(&parentRev)
	if item.Reparented {
		updatedMeta = updatedMeta.WithParentBranchName(&item.NewParent)
		updatedMeta = e.withMergedDownstack(updatedMeta, item.OldParent, snap.meta)
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
	if err := e.git.UpdateRefsBatchWithLog(ctx, updates, reflogRestack); err != nil {
		return RestackBranchResult{Result: RestackConflict, RebasedBranchBase: parentRev}, fmt.Errorf("failed to update refs atomically: %w", err)
	}

	if worktreePath := snap.worktrees.PathForBranch(branchName); worktreePath != "" {
		e.resetWorktreeIfClean(ctx, worktreePath, snap)
	}

	if snap.meta != nil {
		snap.meta[branchName] = updatedMeta
	}
	if snap.revs != nil {
		snap.revs[branchName] = newRev
	}

	return RestackBranchResult{
		Result:            RestackDone,
		RebasedBranchBase: parentRev,
		NewRev:            newRev,
		Reparented:        item.Reparented,
		OldParent:         item.OldParent,
		NewParent:         item.NewParent,
	}, nil
}
