package engine

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/getstackit/stackit/internal/git"
)

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
		e.normalizeCurrentBranchLocked()
	}

	// Validate branch exists
	branchExists := slices.Contains(e.state.branches, branchName)
	if !branchExists {
		// Refresh branches list
		branches, err := e.git.GetAllBranchNames(ctx)
		if err != nil {
			e.mu.Unlock()
			return fmt.Errorf("failed to get branches: %w", err)
		}
		e.state.setBranches(branches)
		branchExists = slices.Contains(e.state.branches, branchName)
		if !branchExists {
			hint := git.CaseMismatchHint(branchName, e.state.branches)
			e.mu.Unlock()
			return fmt.Errorf("branch %s does not exist%s", branchName, hint)
		}
	}

	// Validate parent exists (or is trunk)
	if parentBranchName != e.trunk {
		parentExists := slices.Contains(e.state.branches, parentBranchName)
		if !parentExists {
			// Refresh branches list to check again
			branches, err := e.git.GetAllBranchNames(ctx)
			if err != nil {
				e.mu.Unlock()
				return fmt.Errorf("failed to get branches: %w", err)
			}
			e.state.setBranches(branches)
			parentExists = slices.Contains(e.state.branches, parentBranchName)
			if !parentExists {
				hint := git.CaseMismatchHint(parentBranchName, e.state.branches)
				e.mu.Unlock()
				return fmt.Errorf("parent branch %s does not exist%s", parentBranchName, hint)
			}
		}
	}
	e.mu.Unlock()

	// New branches start without prior divergence metadata, so recompute the
	// merge-base from scratch.
	return e.SetParent(ctx, e.GetBranch(branchName), e.GetBranch(parentBranchName), DivergenceRecompute)
}

// PriorParent records the parent a branch was built on, as read from a source
// outside the local repository: stackit metadata fetched from the remote, or a
// PR base. It carries what SetParent would normally find in existing local
// metadata for a branch that has none yet.
type PriorParent struct {
	// Branch is the name of the parent the branch was built on.
	Branch string
	// Revision is that parent's tip at the time the branch was pushed. It is
	// still reachable from the branch itself even after the parent's ref is
	// deleted, which is what makes it usable as a divergence anchor.
	Revision string
}

// TrackBranchPastLandedParent tracks branchName under parentBranchName when the
// parent it was actually built on is not available locally — an ancestor that
// landed and was deleted on the remote before the branch was fetched.
//
// Recording prior first is what makes the divergence point correct. SetParent's
// recompute path keeps an existing parent revision when that revision has
// landed in the new parent, so a later restack replays only this branch's own
// commits. Without it the branch starts with no history at all and the
// merge-base against the new parent is taken instead — which, for every merge
// method that rewrites SHAs (squash and rebase), puts the landed parent's
// commits back into the replay range.
//
// When prior.Revision has not landed, the recompute path falls back to the
// merge-base on its own and the branch keeps the abandoned parent's commits.
func (e *engineImpl) TrackBranchPastLandedParent(ctx context.Context, branchName string, parentBranchName string, prior PriorParent) error {
	if branchName == e.trunk {
		return fmt.Errorf("cannot track trunk branch %s", e.trunk)
	}
	if branchName == parentBranchName {
		return fmt.Errorf("branch cannot be its own parent")
	}

	if prior.Branch != "" && prior.Revision != "" && prior.Branch != branchName {
		meta, err := e.readMetadata(branchName)
		if err != nil {
			meta = git.NewMeta()
		}
		meta = meta.WithParentBranchName(&prior.Branch).WithParentBranchRevision(&prior.Revision)

		tx := e.BeginTx(fmt.Sprintf("record prior parent: %s -> %s", branchName, prior.Branch))
		if err := tx.UpdateMeta(branchName, meta); err != nil {
			return fmt.Errorf("failed to record prior parent for %s: %w", branchName, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("failed to record prior parent for %s: %w", branchName, err)
		}
	}

	return e.SetParent(ctx, e.GetBranch(branchName), e.GetBranch(parentBranchName), DivergenceRecompute)
}

// UntrackBranch stops tracking a branch by deleting its metadata
func (e *engineImpl) UntrackBranch(ctx context.Context, branchName string) error {
	// Delete metadata
	if err := e.git.DeleteMetadata(ctx, branchName); err != nil {
		return fmt.Errorf("failed to delete metadata ref: %w", err)
	}

	// Rebuild cache
	return e.rebuild()
}

// UntrackBranches stops tracking multiple branches with a single metadata deletion
// and a single engine rebuild, rather than one rebuild per branch.
func (e *engineImpl) UntrackBranches(ctx context.Context, branchNames []string) error {
	if len(branchNames) == 0 {
		return nil
	}
	if len(branchNames) == 1 {
		return e.UntrackBranch(ctx, branchNames[0])
	}

	refNames := make([]string, len(branchNames))
	for i, name := range branchNames {
		refNames[i] = fmt.Sprintf("%s%s", git.MetadataRefPrefix, name)
	}
	if err := e.git.DeleteRefsBatch(ctx, refNames); err != nil {
		return fmt.Errorf("failed to delete metadata refs: %w", err)
	}
	return e.rebuild()
}

// DivergenceMode controls how SetParent updates a branch's recorded divergence
// point (ParentBranchRevision) when its parent changes.
type DivergenceMode int

const (
	// DivergencePreserve keeps the existing divergence point if it's still a
	// valid ancestor of the branch. Use when the branch's own commits should
	// not change — e.g., reparenting a branch onto a sibling stack.
	DivergencePreserve DivergenceMode = iota

	// DivergenceRecompute computes a fresh merge-base against the new parent
	// (with a minor exception: if the old parent was merged into the new
	// parent, the existing revision is retained as a stable anchor). Use when
	// the branch has absorbed commits from a former parent (e.g. after a
	// fold/merge), so that subsequent restacks include those commits.
	DivergenceRecompute
)

// SetParent updates a branch's parent. The mode controls whether the existing
// divergence point is preserved or recomputed; see DivergenceMode for the
// trade-off. Retries on concurrent modification.
func (e *engineImpl) SetParent(ctx context.Context, branch Branch, parentBranch Branch, mode DivergenceMode) error {
	branchName := branch.GetName()
	parentBranchName := parentBranch.GetName()

	if branchName == parentBranchName {
		return fmt.Errorf("branch %s cannot be its own parent", branchName)
	}
	if err := e.validateLinearParentMoves([]BranchParentMove{{Branch: branchName, NewParent: parentBranchName}}); err != nil {
		return err
	}

	switch mode {
	case DivergencePreserve:
		div, err := e.GetDivergencePoint(branchName)
		if err != nil {
			return fmt.Errorf("failed to determine divergence point for %s: %w", branchName, err)
		}
		return e.setParentPreservingDivergence(ctx, branch, parentBranch, div)
	case DivergenceRecompute:
		return e.setParentRecomputingDivergence(ctx, branch, parentBranch)
	default:
		return fmt.Errorf("unknown DivergenceMode: %d", mode)
	}
}

// setParentRecomputingDivergence updates a branch's parent and refreshes the
// divergence point by computing a new merge-base. Retries on concurrent
// modification.
func (e *engineImpl) setParentRecomputingDivergence(ctx context.Context, branch Branch, parentBranch Branch) error {
	branchName := branch.GetName()
	parentBranchName := parentBranch.GetName()
	squashCache := git.NewSquashMergeCache()

	return e.WithRetry(ctx, func() error {
		// Get new parent revision (may run multiple times on retry)
		parentRev, err := e.git.GetMergeBase(ctx, branchName, parentBranchName)
		if err != nil {
			return fmt.Errorf("failed to get merge base: %w", err)
		}

		// Read existing metadata
		meta, err := e.readMetadata(branchName)
		if err != nil {
			return fmt.Errorf("failed to read metadata: %w", err)
		}

		// Get old parent
		oldParent := ""
		if meta.GetParentBranchName() != nil {
			oldParent = *meta.GetParentBranchName()
		}

		// Only update ParentBranchRevision if it's currently nil, invalid, or if we're not
		// in a "parent merged into trunk" situation.
		shouldUpdateRevision := true
		if oldParent != "" && oldParent != parentBranchName && meta.GetParentBranchRevision() != nil && *meta.GetParentBranchRevision() != "" {
			// Check if existing revision is still a valid ancestor of the branch
			if isAncestor, _ := e.git.IsAncestor(ctx, *meta.GetParentBranchRevision(), branchName); isAncestor {
				// Check if the old parent was merged into the new parent (the
				// "merge" case). For trunk targets, use the centralized landed
				// policy so no-metadata collaborator squash merges preserve the
				// child's old upstream instead of replaying the parent's commits.
				// If the old parent ref is already gone, the stored divergence
				// revision is still enough to compare its aggregate patch against
				// trunk.
				oldParentRev := *meta.GetParentBranchRevision()
				if e.branchLanded(ctx, oldParent, parentBranchName, squashCache) ||
					e.branchLanded(ctx, oldParentRev, parentBranchName, squashCache) {
					shouldUpdateRevision = false
				} else if merged, _ := e.git.IsMerged(ctx, oldParent, parentBranchName); merged {
					shouldUpdateRevision = false
				}
			}
		}

		meta = meta.WithParentBranchName(&parentBranchName)
		if shouldUpdateRevision {
			meta = meta.WithParentBranchRevision(&parentRev)
		}

		// Use transaction for atomic update (Commit handles in-memory cache updates)
		tx := e.BeginTx(fmt.Sprintf("set parent: %s -> %s", branchName, parentBranchName))
		if err := tx.UpdateMeta(branchName, meta); err != nil {
			return err
		}
		return tx.Commit(ctx)
	})
}

// setParentName updates only the parent branch name in metadata without
// modifying ParentBranchRevision.
func (e *engineImpl) setParentName(ctx context.Context, branch Branch, parentBranch Branch) error {
	branchName := branch.GetName()
	parentBranchName := parentBranch.GetName()

	if branchName == parentBranchName {
		return fmt.Errorf("branch %s cannot be its own parent", branchName)
	}

	return e.WithRetry(ctx, func() error {
		meta, err := e.readMetadata(branchName)
		if err != nil {
			return fmt.Errorf("failed to read metadata: %w", err)
		}

		meta = meta.WithParentBranchName(&parentBranchName)

		tx := e.BeginTx(fmt.Sprintf("set parent name: %s -> %s", branchName, parentBranchName))
		if err := tx.UpdateMeta(branchName, meta); err != nil {
			return err
		}
		return tx.Commit(ctx)
	})
}

// setParentPreservingDivergence updates a branch's parent while preserving
// the divergence point if it remains a valid ancestor. Uses setParentName
// (not SetParent) so the existing ParentBranchRevision is never overwritten
// with an incorrect merge-base value.
func (e *engineImpl) setParentPreservingDivergence(ctx context.Context, branch Branch, newParent Branch, oldDivergencePoint string) error {
	if err := e.setParentName(ctx, branch, newParent); err != nil {
		return err
	}

	if oldDivergencePoint == "" {
		return nil
	}

	// Set the correct divergence point so restacking replays only this
	// branch's commits, not commits from the old parent.
	isAncestor, err := e.git.IsAncestor(ctx, oldDivergencePoint, branch.GetName())
	if err != nil {
		return fmt.Errorf("failed to check ancestry of divergence point %s for %s: %w", oldDivergencePoint, branch.GetName(), err)
	}
	if !isAncestor {
		return fmt.Errorf("divergence point %s is not an ancestor of %s: cannot preserve divergence point", oldDivergencePoint, branch.GetName())
	}

	return e.updateParentRevision(ctx, branch.GetName(), oldDivergencePoint)
}

// ReparentBranch changes a branch's parent while automatically preserving its
// divergence point. This is the preferred way to reparent an existing branch
// when the branch's own commits should not change.
//
// Automatically propagates the new parent's stack ID to the reparented branch
// when the move crosses a stack boundary; same-stack reparenting is a no-op
// for stack ID.
func (e *engineImpl) ReparentBranch(ctx context.Context, branch Branch, newParent Branch) error {
	if err := e.validateLinearParentMoves([]BranchParentMove{{Branch: branch.GetName(), NewParent: newParent.GetName()}}); err != nil {
		return err
	}
	div, err := e.GetDivergencePoint(branch.GetName())
	if err != nil {
		return fmt.Errorf("failed to determine divergence point for %s: %w", branch.GetName(), err)
	}
	if err := e.setParentPreservingDivergence(ctx, branch, newParent, div); err != nil {
		return err
	}
	// Fetch the post-reparent branch so syncStackIDFromParent sees the new parent.
	return e.syncStackIDFromParent(ctx, e.GetBranch(branch.GetName()))
}

// BranchParentMove pairs a branch with the name of the new parent it should
// adopt. It is the unit of work for ReparentBranchesToParents.
type BranchParentMove struct {
	Branch    string
	NewParent string
}

// BranchParentUpdate describes a parent change and how its divergence point
// should be handled. It is used for cleanup rewrites that must validate the
// topology after their obsolete parents are removed.
type BranchParentUpdate struct {
	Branch    string
	NewParent string
	Mode      DivergenceMode
}

// ApplyParentUpdatesAfterRemovals applies parent updates after validating the
// final topology without the named removed branches. It captures divergence
// points before any mutation so related updates remain correct.
func (e *engineImpl) ApplyParentUpdatesAfterRemovals(ctx context.Context, updates []BranchParentUpdate, removed []string) error {
	if len(updates) == 0 {
		return nil
	}

	moves := make([]BranchParentMove, len(updates))
	preserveBranches := make(Branches, 0, len(updates))
	for i, update := range updates {
		if update.Branch == update.NewParent {
			return fmt.Errorf("branch %s cannot be its own parent", update.Branch)
		}
		if update.Mode != DivergencePreserve && update.Mode != DivergenceRecompute {
			return fmt.Errorf("unknown DivergenceMode: %d", update.Mode)
		}
		moves[i] = BranchParentMove{Branch: update.Branch, NewParent: update.NewParent}
		if update.Mode == DivergencePreserve {
			preserveBranches = append(preserveBranches, e.GetBranch(update.Branch))
		}
	}
	if err := e.validateLinearParentMovesAfterRemovals(moves, removed); err != nil {
		return err
	}

	divPoints := e.BatchDivergencePoints(preserveBranches)
	for _, update := range updates {
		branch := e.GetBranch(update.Branch)
		parent := e.GetBranch(update.NewParent)

		switch update.Mode {
		case DivergencePreserve:
			divPoint, ok := divPoints.Rev(update.Branch)
			if !ok || divPoint == "" {
				return fmt.Errorf("failed to determine divergence point for %s", update.Branch)
			}
			if err := e.setParentPreservingDivergence(ctx, branch, parent, divPoint); err != nil {
				return fmt.Errorf("failed to reparent %s to %s: %w", update.Branch, update.NewParent, err)
			}
			if err := e.syncStackIDFromParent(ctx, e.GetBranch(update.Branch)); err != nil {
				return fmt.Errorf("failed to sync stack ID for %s: %w", update.Branch, err)
			}
		case DivergenceRecompute:
			if err := e.setParentRecomputingDivergence(ctx, branch, parent); err != nil {
				return fmt.Errorf("failed to set parent for %s: %w", update.Branch, err)
			}
		}
	}

	return nil
}

// ReparentBranches changes multiple branches to the same new parent while
// preserving each branch's divergence point. Divergence points are captured
// for all branches before any reparenting begins, ensuring correctness when
// branches in the list are related to each other.
//
// Automatically propagates the new parent's stack ID to each branch when the
// move crosses a stack boundary.
func (e *engineImpl) ReparentBranches(ctx context.Context, branchNames []string, newParent Branch) error {
	moves := make([]BranchParentMove, len(branchNames))
	for i, name := range branchNames {
		moves[i] = BranchParentMove{Branch: name, NewParent: newParent.GetName()}
	}
	return e.ReparentBranchesToParents(ctx, moves)
}

// ReparentBranchesToParents reparents each branch onto its own designated parent
// while preserving every branch's divergence point. Like ReparentBranches, all
// divergence points are captured before any mutation so related branches in the
// set stay correct; unlike it, each branch may move to a different parent — the
// shape needed by whole-stack rewrites such as reorder and flatten.
//
// Automatically propagates each new parent's stack ID when a move crosses a
// stack boundary.
func (e *engineImpl) ReparentBranchesToParents(ctx context.Context, moves []BranchParentMove) error {
	if err := e.validateLinearParentMoves(moves); err != nil {
		return err
	}

	branches := make(Branches, len(moves))
	for i, m := range moves {
		branches[i] = e.GetBranch(m.Branch)
	}
	divPoints := e.BatchDivergencePoints(branches)
	for _, m := range moves {
		// The batch reader returns "" where the individual lookup would error;
		// reparenting must not proceed with an unknown divergence point.
		if rev, ok := divPoints.Rev(m.Branch); !ok || rev == "" {
			return fmt.Errorf("failed to determine divergence point for %s", m.Branch)
		}
	}

	for _, m := range moves {
		newParent := e.GetBranch(m.NewParent)
		divPoint, _ := divPoints.Rev(m.Branch)
		if err := e.setParentPreservingDivergence(ctx, e.GetBranch(m.Branch), newParent, divPoint); err != nil {
			return fmt.Errorf("failed to reparent %s to %s: %w", m.Branch, m.NewParent, err)
		}
		if err := e.syncStackIDFromParent(ctx, e.GetBranch(m.Branch)); err != nil {
			return fmt.Errorf("failed to sync stack ID for %s: %w", m.Branch, err)
		}
	}
	return nil
}

// ReparentBranchesRecompute reparents each branch onto the same new parent and
// recomputes its divergence point against that parent (a fresh merge-base)
// rather than preserving the existing one. Use when the branches are moving
// under a newly created/inserted parent and should replay their own commits
// onto it — the batch counterpart to SetParent(..., DivergenceRecompute).
//
// Automatically propagates the new parent's stack ID when a move crosses a
// stack boundary.
func (e *engineImpl) ReparentBranchesRecompute(ctx context.Context, branchNames []string, newParent Branch) error {
	moves := make([]BranchParentMove, len(branchNames))
	for i, name := range branchNames {
		moves[i] = BranchParentMove{Branch: name, NewParent: newParent.GetName()}
	}
	if err := e.validateLinearParentMoves(moves); err != nil {
		return err
	}

	for _, name := range branchNames {
		if err := e.SetParent(ctx, e.GetBranch(name), newParent, DivergenceRecompute); err != nil {
			return fmt.Errorf("failed to reparent %s to %s: %w", name, newParent.GetName(), err)
		}
		if err := e.syncStackIDFromParent(ctx, e.GetBranch(name)); err != nil {
			return fmt.Errorf("failed to sync stack ID for %s: %w", name, err)
		}
	}
	return nil
}

// validateLinearParentMoves rejects a set of parent changes that would create
// a fork below a non-trunk branch. It evaluates the final topology as a batch
// so valid reorder operations do not fail merely because of an intermediate
// shape during their rewrite.
func (e *engineImpl) validateLinearParentMoves(moves []BranchParentMove) error {
	return e.validateLinearParentMovesAfterRemovals(moves, nil)
}

// validateLinearParentMovesAfterRemovals evaluates parent changes after the
// named branches have been removed. Deletion paths use it to avoid rejecting a
// valid chain collapse solely because the soon-to-be-deleted branch is still
// present in the in-memory graph.
func (e *engineImpl) validateLinearParentMovesAfterRemovals(moves []BranchParentMove, removed []string) error {
	if !e.linearStacks || len(moves) == 0 {
		return nil
	}

	graph := e.Graph(SortStrategyAlphabetical)
	children := make(map[string]map[string]struct{})
	for _, branch := range e.AllBranches() {
		parent := graph.Parent(branch)
		if parent == "" {
			continue
		}
		if children[parent] == nil {
			children[parent] = make(map[string]struct{})
		}
		children[parent][branch.GetName()] = struct{}{}
	}

	// Remove all moving branches first, then apply their proposed parents. This
	// models the final graph rather than an arbitrary mutation order.
	for _, move := range moves {
		branch := e.GetBranch(move.Branch)
		if parent := graph.Parent(branch); parent != "" {
			delete(children[parent], move.Branch)
		}
	}
	for _, branchName := range removed {
		branch := e.GetBranch(branchName)
		if parent := graph.Parent(branch); parent != "" {
			delete(children[parent], branchName)
		}
		delete(children, branchName)
	}
	for _, move := range moves {
		if children[move.NewParent] == nil {
			children[move.NewParent] = make(map[string]struct{})
		}
		children[move.NewParent][move.Branch] = struct{}{}
	}

	for parent, childSet := range children {
		if parent == e.trunk || len(childSet) <= 1 {
			continue
		}
		childNames := make([]string, 0, len(childSet))
		for child := range childSet {
			childNames = append(childNames, child)
		}
		slices.Sort(childNames)
		return fmt.Errorf("linear stacks are enabled: %s would have multiple children (%s); set stack.shape to tree to allow forks", parent, strings.Join(childNames, ", "))
	}

	return nil
}

// validateLinearSiblingSplit checks the final shape of a sibling commit split.
// ApplySplitToCommits creates metadata directly, so these new children cannot
// be represented as ordinary parent moves until after the mutation.
func (e *engineImpl) validateLinearSiblingSplit(parent, branchToSplit string, branchNames []string) error {
	if !e.linearStacks || parent == e.trunk {
		return nil
	}

	graph := e.Graph(SortStrategyAlphabetical)
	children := make(map[string]struct{})
	for _, child := range graph.Children(e.GetBranch(parent)) {
		children[child] = struct{}{}
	}

	// The original branch is removed when it is not retained as one of the
	// split branches. The remaining names all become direct children.
	if !slices.Contains(branchNames, branchToSplit) {
		delete(children, branchToSplit)
	}
	for _, branchName := range branchNames {
		children[branchName] = struct{}{}
	}

	if len(children) <= 1 {
		return nil
	}
	childNames := make([]string, 0, len(children))
	for child := range children {
		childNames = append(childNames, child)
	}
	slices.Sort(childNames)
	return fmt.Errorf("linear stacks are enabled: %s would have multiple children (%s); set stack.shape to tree to allow forks", parent, strings.Join(childNames, ", "))
}

// updateParentRevision updates the parent revision in metadata using transaction API
// with retry logic for concurrent modification resilience.
func (e *engineImpl) updateParentRevision(ctx context.Context, branchName string, parentRev string) error {
	return e.WithRetry(ctx, func() error {
		// Read existing metadata (outside lock for performance)
		meta, err := e.readMetadata(branchName)
		if err != nil {
			return fmt.Errorf("failed to read metadata: %w", err)
		}

		meta = meta.WithParentBranchRevision(&parentRev)

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
		meta, err := e.readMetadata(branchName)
		if err != nil {
			return fmt.Errorf("failed to read metadata: %w", err)
		}

		// Update scope
		if scope.IsEmpty() {
			meta = meta.WithScope(nil)
		} else {
			scopeStr := scope.String()
			meta = meta.WithScope(&scopeStr)
		}

		// Use transaction for atomic update
		tx := e.BeginTx(fmt.Sprintf("set scope: %s", branchName))
		if err := tx.UpdateMeta(branchName, meta); err != nil {
			return err
		}
		return tx.Commit(ctx)
	})
}

// SetScopeAndMarkForUpdate sets the scope and marks the branch as needing a PR
// body update in a single atomic transaction, saving one git ref write compared
// to calling SetScope + MarkBranchesForPRBodyUpdate separately.
func (e *engineImpl) SetScopeAndMarkForUpdate(ctx context.Context, branch Branch, scope Scope) error {
	branchName := branch.GetName()

	return e.WithRetry(ctx, func() error {
		meta, err := e.readMetadata(branchName)
		if err != nil {
			return fmt.Errorf("failed to read metadata: %w", err)
		}

		if scope.IsEmpty() {
			meta = meta.WithScope(nil)
		} else {
			scopeStr := scope.String()
			meta = meta.WithScope(&scopeStr)
		}

		localMeta, _ := e.readLocalMetadata(branchName)
		if localMeta == nil {
			localMeta = &git.LocalMeta{}
		}
		localMeta.NeedsPRBodyUpdate = true

		tx := e.BeginTx(fmt.Sprintf("set scope+mark: %s", branchName))
		if err := tx.UpdateMeta(branchName, meta); err != nil {
			return err
		}
		if err := tx.UpdateLocalMeta(branchName, localMeta); err != nil {
			return err
		}
		return tx.Commit(ctx)
	})
}

// SetLocked updates multiple branches' locked status atomically using transactions.
// It retries on concurrent modification errors with exponential backoff.
func (e *engineImpl) SetLocked(ctx context.Context, branches Branches, reason LockReason) (BatchLockResult, error) {
	result := BatchLockResult{
		AffectedBranches: make([]string, 0, len(branches)),
		Errors:           make(map[string]error),
	}

	if len(branches) == 0 {
		return result, nil
	}

	// Extract branch names for batch read (preserves order for deterministic results)
	branchNames := branches.Names()

	err := e.WithRetry(ctx, func() error {
		// Reset result for retry
		result.AffectedBranches = result.AffectedBranches[:0]
		result.Errors = make(map[string]error)

		// Batch read all metadata first (parallel, outside any lock)
		metas, readErrs := e.batchReadMetadata(branchNames)

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
				meta = git.NewMeta()
			}
			meta = meta.WithLockReason(reason)
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
func (e *engineImpl) SetFrozen(ctx context.Context, branches Branches, frozen bool) (BatchFreezeResult, error) {
	result := BatchFreezeResult{
		AffectedBranches: make([]string, 0, len(branches)),
		Errors:           make(map[string]error),
	}

	if len(branches) == 0 {
		return result, nil
	}

	// Extract branch names for batch read (preserves order for deterministic results)
	branchNames := branches.Names()

	err := e.WithRetry(ctx, func() error {
		// Reset result for retry
		result.AffectedBranches = result.AffectedBranches[:0]
		result.Errors = make(map[string]error)

		// Batch read all local metadata first (parallel, outside any lock)
		metas := e.batchReadLocalMetadata(branchNames)

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
