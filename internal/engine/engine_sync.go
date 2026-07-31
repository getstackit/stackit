package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/getstackit/stackit/internal/git"
)

// RestackBranches implements a hybrid batch approach for performance:
// 1. Collect all data required for the restack (in bulk)
// 2. Process branches using individual restackBranch calls with deferred rebuilds
// 3. Final cache rebuild
func (e *engineImpl) RestackBranches(ctx context.Context, branches Branches) (RestackBatchResult, error) {
	return e.restackBranches(ctx, branches, nil, nil, nil)
}

// RestackBranchesWithProgress mirrors RestackBranches but reports progress
// after each branch is processed.
func (e *engineImpl) RestackBranchesWithProgress(ctx context.Context, branches Branches, progress RestackBranchProgressFunc) (RestackBatchResult, error) {
	return e.restackBranches(ctx, branches, nil, nil, progress)
}

// RestackBranchesWithValidatedRebases applies successful dry-run validation
// commits directly to branch refs. Validation worktrees share the repository's
// object database, so the rebased commits can be reused instead of replaying
// the same rebase a second time.
func (e *engineImpl) RestackBranchesWithValidatedRebases(ctx context.Context, branches Branches, validation *RebaseValidation, progress RestackBranchProgressFunc) (RestackBatchResult, error) {
	plan, err := e.PlanRestack(ctx, branches)
	if err != nil {
		return RestackBatchResult{}, err
	}
	return e.restackBranches(ctx, branches, validation, plan, progress)
}

// RestackBranchesWithValidatedPlan applies successful dry-run validation
// commits using the caller's restack plan.
func (e *engineImpl) RestackBranchesWithValidatedPlan(ctx context.Context, branches Branches, validation *RebaseValidation, plan *RestackPlan, progress RestackBranchProgressFunc) (RestackBatchResult, error) {
	return e.restackBranches(ctx, branches, validation, plan, progress)
}

// collectRestackData resolves the metadata and revisions restack planning and
// application need — the given branches, every tracked ancestor up to trunk,
// and trunk itself — in two batched reads instead of per-branch git calls.
// Consumers treat the maps as a point-in-time snapshot and fall back to
// individual reads on a miss.
func (e *engineImpl) collectRestackData(branchNames []string) (MetaMap, RevisionMap) {
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

	metaMap, _ := e.batchReadMetadata(involvedBranchNames)
	revMap, _ := e.git.BatchGetRevisions(involvedBranchNames)
	return metaMap, revMap
}

func (e *engineImpl) restackBranches(ctx context.Context, branches Branches, validation *RebaseValidation, plan *RestackPlan, progress RestackBranchProgressFunc) (RestackBatchResult, error) {
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
	allMeta, allRevisions := e.collectRestackData(branches.Names())

	// Snapshot the worktree list once. Restacking N branches used to call
	// `git worktree list` N times for the "reset other worktree's working dir"
	// check; the snapshot is stable across the restack loop since we don't
	// add or remove worktrees here.
	worktrees, err := e.git.ListWorktrees(ctx)
	if err != nil {
		worktrees = git.WorktreeList{}
	}

	// Snapshot which of those worktrees have tracked-file changes before any
	// branch in this pass is touched. This must happen up front: restacking a
	// branch moves its ref before its worktree gets reset, so checking
	// dirtiness lazily at reset time would compare the worktree's now-stale
	// content against its own new ref and read as dirty every time, not just
	// when the user actually had uncommitted work. Bounded by worktree count,
	// not branch count, so this stays a handful of calls even for a large stack.
	//
	// Tracked changes only, deliberately. This gates a `git reset --hard`, which
	// cannot touch untracked files — so counting them would suppress a reset
	// that was never a threat to them, leaving the worktree holding the old
	// commit's content while its branch ref has moved. `git status` then reports
	// the new commit's files as deleted, and the next `stackit modify -a`
	// commits that deletion into the stack.
	dirtyWorktrees := make(map[string]bool, len(worktrees))
	for _, path := range worktrees.Paths() {
		if dirty, dirtyErr := e.git.WorktreeHasTrackedChanges(ctx, path); dirtyErr == nil && dirty {
			dirtyWorktrees[path] = true
		}
	}

	// Snapshot every metadata ref's SHA once via a single in-process iterator
	// (git.ListRefs). The previous code called GetRef per branch in three
	// places (frozen path, anchor path, regular rebase) plus applyBranchAndMetadata
	// — N branches meant N+ rev-parse subprocesses. Each branch only reads its
	// own SHA for optimistic locking on the metadata ref UpdateRefsBatch, so a
	// pre-loop snapshot is stable.
	metaRefSHAs := make(map[string]string)
	if refs, listErr := e.git.ListRefs(git.MetadataRefPrefix); listErr == nil {
		for refName, sha := range refs {
			metaRefSHAs[strings.TrimPrefix(refName, git.MetadataRefPrefix)] = sha
		}
	}
	snap := &restackSnapshot{meta: allMeta, revs: allRevisions, worktrees: worktrees, metaRefSHAs: metaRefSHAs, dirtyWorktrees: dirtyWorktrees}

	// 2. Apply the restack changes
	results := make(map[string]RestackBranchResult)
	needsRebuild := false
	squashCache := git.NewSquashMergeCache()

	for i, branch := range branches {
		branchName := branch.GetName()
		var result RestackBranchResult
		var err error
		if validation != nil {
			result, err = e.restackBranchWithValidatedRebase(ctx, branch, validation, plan, snap)
		} else {
			result, err = e.restackBranch(ctx, branch, snap, squashCache)
		}
		results[branchName] = result

		if err == nil && progress != nil {
			progress(branch, result)
		}

		if err == nil && result.Result == RestackDone && result.NewRev != "" {
			// Update the revision map with the new SHA for downstream branches
			// in the batch. The new SHA was already computed inside restackBranch
			// / applyBranchAndMetadata — using it here avoids a per-branch
			// GetRevision subprocess. For RestackUnneeded we leave the map alone
			// because the branch's SHA didn't change and allRevisions already
			// holds the pre-restack value from BatchGetRevisions.
			if snap.revs == nil {
				snap.revs = make(RevisionMap)
			}
			snap.revs[branchName] = result.NewRev
		}

		if err != nil {
			// Convert remaining branches to []string for RestackBatchResult
			remainingBranchNames := make([]string, len(branches[i+1:]))
			for j, b := range branches[i+1:] {
				remainingBranchNames[j] = b.GetName()
			}
			if needsRebuild {
				if rebuildErr := e.rebuild(); rebuildErr != nil {
					return RestackBatchResult{
						ConflictBranch:    branchName,
						RebasedBranchBase: result.RebasedBranchBase,
						RemainingBranches: remainingBranchNames,
						Results:           results,
					}, fmt.Errorf("failed to rebuild after partial batch restack: %w", rebuildErr)
				}
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
			if needsRebuild {
				if rebuildErr := e.rebuild(); rebuildErr != nil {
					return RestackBatchResult{
						ConflictBranch:    branchName,
						RebasedBranchBase: result.RebasedBranchBase,
						RemainingBranches: remainingBranchNames,
						Results:           results,
					}, fmt.Errorf("failed to rebuild after partial batch restack: %w", rebuildErr)
				}
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
