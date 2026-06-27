package engine

import (
	"context"
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

	for _, branch := range branches {
		item, ok := e.planRestackBranch(ctx, branch, plan.BranchMap)
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
				if parentRev, err := e.GetBranch(item.NewParent).GetRevision(); err == nil && parentRev != item.ParentRev {
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

func (e *engineImpl) planRestackBranch(ctx context.Context, branch Branch, plannedBranches map[string]bool) (RestackPlanItem, bool) {
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
		if _, err := e.GetBranch(parentName).GetRevision(); err != nil {
			if ancestors, ancestorErr := e.FindMostRecentTrackedAncestors(ctx, branchName); ancestorErr == nil && len(ancestors) > 0 {
				parentName = ancestors[0]
			} else {
				parentName = e.trunk
			}
		}
	}

	// If this branch has already landed, do not rebase it during restack. This
	// covers merged PR metadata for all GitHub methods, plus Git-detected merge,
	// rebase, and multi-commit squash histories on trunk before sync refreshes
	// PR metadata.
	if e.branchLanded(ctx, branchName, parentName, landedMetadataOrTrunkGit) {
		item.Skip = true
		item.SkipResult = RestackBranchResult{Result: RestackUnneeded}
		return item, true
	}

	if branch.IsFrozen() {
		parentRev, err := e.GetBranch(parentName).GetRevision()
		if err != nil {
			return item, false
		}
		remoteSha, err := e.git.GetRemoteRevision(branchName)
		if err != nil || remoteSha == "" {
			item.Skip = true
			item.SkipResult = RestackBranchResult{Result: RestackUnneeded, Frozen: true}
			return item, true
		}
		localSha, err := branch.GetRevision()
		if err != nil {
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
		trunkRev, err := e.Trunk().GetRevision()
		if err != nil {
			return item, false
		}
		anchorRev, err := branch.GetRevision()
		if err != nil {
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
	needsReparent := state != nil && e.shouldReparentBranch(ctx, state.Parent, nil)
	if needsReparent {
		item.Reparented = true
		item.OldParent = state.Parent
		parentName = e.findNearestValidAncestor(ctx, branchName, nil)
	}
	e.mu.RUnlock()
	item.NewParent = parentName

	meta, err := e.git.ReadMetadata(branchName)
	if err != nil {
		return item, false
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

	parentRev, err := e.GetBranch(parentName).GetRevision()
	if err != nil {
		return item, false
	}
	if e.IsWorktreeAnchor(e.GetBranch(parentName)) {
		trunkRev, err := e.Trunk().GetRevision()
		if err != nil {
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
