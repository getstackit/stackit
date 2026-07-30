package engine

import (
	"context"

	"github.com/getstackit/stackit/internal/git"
)

// PlanRestack builds the rebase specs and branch decisions for a restack.
// Branches that are locked or provably up to date are reported in
// PlannedResults so callers can skip validation worktrees for them.
func (e *engineImpl) PlanRestack(ctx context.Context, branches Branches) (*RestackPlan, error) {
	plan := &RestackPlan{
		Specs:          make([]RebaseSpec, 0, len(branches)),
		BranchMap:      make(BranchNameSet),
		ApplyMap:       make(BranchNameSet),
		PlannedResults: make(map[string]RestackBranchResult),
		Items:          make(map[string]RestackPlanItem),
	}
	squashCache := git.NewSquashMergeCache()

	// Resolve metadata and revisions for the branches, their tracked ancestors,
	// and trunk in two batched reads instead of per-branch git calls. Planning
	// is read-only, so the snapshot stays valid for the whole loop.
	metaMap, revMap := e.collectRestackData(branches.Names())

	for _, branch := range branches {
		item, ok := e.planRestackBranch(ctx, branch, plan.BranchMap, squashCache, metaMap, revMap)
		if !ok {
			continue
		}
		plan.Items[item.Branch] = item
		if item.Skip {
			plan.PlannedResults[item.Branch] = item.SkipResult
			continue
		}
		plan.ApplyMap[item.Branch] = true
		if item.Action == RestackPlanApplyAnchor {
			// A moved anchor has to invalidate its children exactly like any
			// other moved parent. Without this they stay "up to date" against
			// the anchor's previous tip, so their recorded parent revision
			// never catches up and every consumer reading it keeps drifting.
			plan.BranchMap[item.Branch] = true
		}
		if item.Action == RestackPlanApplyValidated {
			specNewParent := item.NewParent
			if parentItem, ok := plan.Items[item.NewParent]; ok && parentItem.Action == RestackPlanApplyAnchor && parentItem.TargetRev != "" {
				specNewParent = parentItem.TargetRev
			} else if e.IsWorktreeAnchor(e.GetBranch(item.NewParent)) && item.ParentRev != "" {
				if parentRev, ok := e.planRev(revMap, item.NewParent); ok && parentRev != item.ParentRev {
					specNewParent = item.ParentRev
				}
			}
			plan.Specs = append(plan.Specs, RebaseSpec{
				Branch:      item.Branch,
				NewParent:   specNewParent,
				OldUpstream: item.OldUpstream,
			})
			plan.BranchMap[item.Branch] = true
		}
	}

	return plan, nil
}

// planRestackBranch builds the plan item for one branch. metaMap and revMap
// are batch-resolved snapshots from collectRestackData; lookups fall back to
// individual reads on a miss so the maps are an optimization, not a contract.
func (e *engineImpl) planRestackBranch(ctx context.Context, branch Branch, plannedBranches BranchNameSet, squashCache *git.SquashMergeCache, metaMap MetaMap, revMap RevisionMap) (RestackPlanItem, bool) {
	branchName := branch.GetName()
	item := RestackPlanItem{Branch: branchName, Action: RestackPlanApplyValidated}

	lockReason := e.GetLockReason(branch)
	if lockReason.IsLocked() && lockReason != LockReasonDraining {
		item.Skip = true
		item.SkipResult = RestackBranchResult{
			Result:     RestackUnneeded,
			LockReason: lockReason,
		}
		return item, true
	}

	parent := branch.GetParent()
	parentName := e.trunk
	e.mu.RLock()
	state := e.readState(branchName)
	e.mu.RUnlock()
	if state != nil && state.Parent != "" {
		parentName = state.Parent
	}
	if parent != nil {
		parentName = parent.GetName()
		if _, ok := e.planRev(revMap, parentName); !ok {
			if ancestors, ancestorErr := e.FindMostRecentTrackedAncestors(ctx, branchName); ancestorErr == nil && len(ancestors) > 0 {
				parentName = ancestors[0]
			} else {
				parentName = e.trunk
			}
		}
	}

	// Worktree anchors are handled before the landed check below. An anchor
	// holds no commits of its own -- it marks where trunk was when the worktree
	// was created -- so it is always an ancestor of trunk and the landed check
	// would always skip it. Skipping it means it never fast-forwards, and every
	// consumer that measures a child against its recorded parent (restack
	// ranges, tree commit counts and diffs) drifts further from reality with
	// each trunk advance.
	if branch.IsWorktreeAnchor() {
		trunkRev, ok := e.planRev(revMap, e.trunk)
		if !ok {
			return item, false
		}
		anchorRev, ok := e.planRev(revMap, branchName)
		if !ok {
			return item, false
		}
		if anchorRev == trunkRev {
			item.Skip = true
			item.SkipResult = RestackBranchResult{Result: RestackUnneeded}
			return item, true
		}
		item.Action = RestackPlanApplyAnchor
		item.NewParent = e.trunk
		item.ParentRev = trunkRev
		item.TargetRev = trunkRev
		return item, true
	}

	// If this branch has already landed, do not rebase it during restack. This
	// covers merged PR metadata for all GitHub methods, plus Git-detected merge,
	// rebase, and multi-commit squash histories on trunk even when the merged
	// branch has no stackit PR metadata.
	if e.branchLanded(ctx, branchName, parentName, squashCache) {
		item.Skip = true
		item.SkipResult = RestackBranchResult{Result: RestackUnneeded}
		return item, true
	}

	if branch.IsFrozen() {
		parentRev, ok := e.planRev(revMap, parentName)
		if !ok {
			return item, false
		}
		remoteSha, err := e.git.GetRemoteRevision(branchName)
		if err != nil || remoteSha == "" {
			item.Skip = true
			item.SkipResult = RestackBranchResult{Result: RestackUnneeded, Frozen: true}
			return item, true
		}
		localSha, ok := e.planRev(revMap, branchName)
		if !ok {
			return item, false
		}
		if localSha == remoteSha {
			item.Skip = true
			item.SkipResult = RestackBranchResult{Result: RestackUnneeded, Frozen: true}
			return item, true
		}
		item.Action = RestackPlanApplyFrozen
		item.NewParent = parentName
		item.ParentRev = parentRev
		item.TargetRev = remoteSha
		return item, true
	}

	e.mu.RLock()
	needsReparent := state != nil && e.shouldReparentBranch(ctx, state.Parent, nil, squashCache)
	if needsReparent {
		item.Reparented = true
		item.OldParent = state.Parent
		parentName = e.findNearestValidAncestor(ctx, branchName, nil, squashCache)
	}
	e.mu.RUnlock()
	item.NewParent = parentName

	meta := metaMap[branchName]
	if meta == nil {
		var err error
		meta, err = e.git.ReadMetadata(branchName)
		if err != nil {
			return item, false
		}
	}

	oldParentRev := ""
	if rev := meta.GetParentBranchRevision(); rev != nil {
		oldParentRev = *rev
	}

	// A worktree anchor is never the branch's real rebase base: children of an
	// anchor are rebased onto trunk's tip below. Measure the other end of the
	// range against trunk too, or the two ends sit on different bases. The
	// anchor's recorded revision goes stale as soon as trunk advances, and the
	// is-ancestor check below cannot detect that: a stale anchor revision is an
	// ancestor of trunk, so it stays an ancestor of a branch sitting on trunk.
	// Trusting it keeps every trunk commit since the anchor was created inside
	// the replay range.
	rebaseBase := parentName
	if e.IsWorktreeAnchor(e.GetBranch(parentName)) {
		rebaseBase = e.trunk
		oldParentRev = ""
	}

	if oldParentRev != "" {
		isAncestor, err := e.git.IsAncestor(ctx, oldParentRev, branchName)
		if err != nil {
			isAncestor = false
		}
		if !isAncestor {
			mergeBase, err := e.git.GetMergeBase(ctx, branchName, rebaseBase)
			if err != nil {
				return item, false
			}
			oldParentRev = mergeBase
		}
	} else {
		mergeBase, err := e.git.GetMergeBase(ctx, branchName, rebaseBase)
		if err != nil {
			return item, false
		}
		oldParentRev = mergeBase
	}
	item.OldUpstream = oldParentRev

	parentRev, ok := e.planRev(revMap, parentName)
	if !ok {
		return item, false
	}
	if e.IsWorktreeAnchor(e.GetBranch(parentName)) {
		trunkRev, ok := e.planRev(revMap, e.trunk)
		if !ok {
			return item, false
		}
		if parentRev != trunkRev {
			parentRev = trunkRev
		}
	}
	item.ParentRev = parentRev

	// A branch can already sit exactly on its parent's tip (parentRev ==
	// oldParentRev) while its recorded parent revision has still drifted —
	// never set, stale from manual git operations, or (for a worktree
	// anchor's children) resolved to trunk while the metadata still holds
	// whatever the anchor pointed at. A branch that needs no rebase never
	// reaches a path that rewrites its metadata on its own, so a drifted
	// record can never catch up by itself — it just keeps drifting, and
	// everything that reads it (tree commit counts and diffs, "restack
	// suggested") drifts with it.
	//
	// The two needs are handled separately rather than folded into one
	// rebase: a branch that is not actually based where it should be gets a
	// real rebase, but a branch that's already correctly based only gets its
	// metadata corrected. Rebasing purely to fix the record would replay the
	// branch's own commits onto themselves, minting a fresh SHA on every
	// restack for no content change — forcing unnecessary force-pushes, CI
	// re-runs, and detached review comments.
	recordedRev := ""
	if rev := meta.GetParentBranchRevision(); rev != nil {
		recordedRev = *rev
	}
	correctlyBased := parentRev == oldParentRev
	skippable := !plannedBranches.Contains(parentName) && !item.Reparented
	switch {
	case correctlyBased && oldParentRev == recordedRev && skippable:
		item.Skip = true
		item.SkipResult = RestackBranchResult{
			Result:            RestackUnneeded,
			RebasedBranchBase: parentRev,
		}
	case correctlyBased && skippable:
		item.Action = RestackPlanApplyMetadataRefresh
	}

	return item, true
}

// planRev returns a branch's revision from the batch-resolved snapshot,
// falling back to an individual read on a miss. ok is false when the revision
// cannot be resolved at all (e.g. the branch was deleted).
func (e *engineImpl) planRev(revMap RevisionMap, name string) (string, bool) {
	if rev, ok := revMap.Rev(name); ok {
		return rev, true
	}
	rev, err := e.GetBranch(name).GetRevision()
	if err != nil {
		return "", false
	}
	return rev, true
}
