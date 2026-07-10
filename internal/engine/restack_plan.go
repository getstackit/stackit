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
		BranchMap:      make(map[string]bool),
		ApplyMap:       make(map[string]bool),
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
func (e *engineImpl) planRestackBranch(ctx context.Context, branch Branch, plannedBranches map[string]bool, squashCache *git.SquashMergeCache, metaMap map[string]*git.Meta, revMap map[string]string) (RestackPlanItem, bool) {
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

	if oldParentRev != "" {
		isAncestor, err := e.git.IsAncestor(ctx, oldParentRev, branchName)
		if err != nil {
			isAncestor = false
		}
		if !isAncestor {
			mergeBase, err := e.git.GetMergeBase(ctx, branchName, parentName)
			if err != nil {
				return item, false
			}
			oldParentRev = mergeBase
		}
	} else {
		mergeBase, err := e.git.GetMergeBase(ctx, branchName, parentName)
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

	if parentRev == oldParentRev && !plannedBranches[parentName] && !item.Reparented {
		item.Skip = true
		item.SkipResult = RestackBranchResult{
			Result:            RestackUnneeded,
			RebasedBranchBase: parentRev,
		}
	}

	return item, true
}

// planRev returns a branch's revision from the batch-resolved snapshot,
// falling back to an individual read on a miss. ok is false when the revision
// cannot be resolved at all (e.g. the branch was deleted).
func (e *engineImpl) planRev(revMap map[string]string, name string) (string, bool) {
	if rev, ok := revMap[name]; ok {
		return rev, true
	}
	rev, err := e.GetBranch(name).GetRevision()
	if err != nil {
		return "", false
	}
	return rev, true
}
