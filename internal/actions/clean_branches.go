package actions

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/output"
)

// CleanBranchesOptions contains options for cleaning branches
type CleanBranchesOptions struct {
	Force             bool
	InManagedWorktree bool   // True if running from a stackit-managed worktree
	CurrentBranch     string // Name of the current branch (used to skip deletion in worktree)
	RemoteStatuses    engine.BranchRemoteStatuses
}

// CleanBranchesResult contains the result of cleaning branches
type CleanBranchesResult struct {
	DeletedBranches        map[string]string // name -> reason
	BranchesWithNewParents []string
	SkippedInWorktree      []string // branches that couldn't be deleted from worktree
	SkippedUnpushed        []string // branches skipped due to unpushed local changes
	SkippedCheckedOut      []string // branches checked out in the main working tree, which cannot be removed
}

// BranchDeletionPlan contains the planned branch deletions before execution
type BranchDeletionPlan struct {
	// BranchesToDelete maps branch name to deletion reason
	BranchesToDelete map[string]string
	// BranchesWithNewParents lists branches that will be reparented
	BranchesWithNewParents []string
	// SkippedInWorktree lists branches skipped due to being in a worktree
	SkippedInWorktree []string
	// UtilityBranches tracks which branches in BranchesToDelete are utility branches
	// (e.g., consolidated merge branches). These can be auto-confirmed for deletion
	// when their associated PR is closed/merged.
	UtilityBranches map[string]bool
	// UnpushedBranches tracks which branches in BranchesToDelete have unpushed local changes
	// (local branch is ahead of or diverged from remote)
	UnpushedBranches map[string]bool
	// internal plan for execution
	plan           *deletionPlan
	reparentMoves  []plannedReparentMove
	deleteStatuses engine.DeletionStatuses
}

// branchDeletionInfo stores information about a branch marked for deletion
type branchDeletionInfo struct {
	reason     string
	reasonKind engine.DeletionReasonKind
	blockers   map[string]bool
}

// deletionPlan manages the state of branches being deleted
type deletionPlan struct {
	branches map[string]*branchDeletionInfo
}

type plannedReparentMove struct {
	branchName         string
	newParentName      string
	preserveDivergence bool
}

func newDeletionPlan() *deletionPlan {
	return &deletionPlan{
		branches: make(map[string]*branchDeletionInfo),
	}
}

func (p *deletionPlan) add(name string, status engine.DeletionStatus, blockers map[string]bool) {
	p.branches[name] = &branchDeletionInfo{
		reason:     status.Reason,
		reasonKind: status.Kind,
		blockers:   blockers,
	}
}

func (p *deletionPlan) isDeleting(name string) bool {
	_, ok := p.branches[name]
	return ok
}

func (p *deletionPlan) removeBlocker(branchName, blockerName string) {
	if info, ok := p.branches[branchName]; ok {
		delete(info.blockers, blockerName)
	}
}

// CleanBranches finds and deletes merged/closed branches.
// It follows a multi-phase approach:
// 1. Identify which branches SHOULD be deleted (parallel pre-calculation).
// 2. Build a deletion plan by traversing the stack (DFS).
// 3. Reparent branches that are NOT being deleted but whose parents ARE.
// 4. Execute the deletions in batches (greedy iterative approach).
func CleanBranches(ctx *app.Context, opts CleanBranchesOptions) (*CleanBranchesResult, error) {
	plan, err := PlanBranchDeletions(ctx, opts)
	if err != nil {
		return nil, err
	}

	return ExecuteBranchDeletions(ctx, plan, nil)
}

// PlanBranchDeletions identifies branches that should be deleted and builds a deletion plan.
// This does NOT execute any deletions - use ExecuteBranchDeletions to apply the plan.
func PlanBranchDeletions(ctx *app.Context, opts CleanBranchesOptions) (*BranchDeletionPlan, error) {
	// Phase 1: Identify candidates for deletion
	deleteStatuses, skippedInWorktree, utilityBranches, err := identifyBranchesToDelete(ctx, opts)
	if err != nil {
		return nil, err
	}

	// Phase 2: Build deletion plan
	plan, branchesWithNewParents, reparentMoves := buildDeletionPlan(ctx, deleteStatuses)

	// Build the public plan
	branchesToDelete := make(map[string]string)
	unpushedBranches := make(map[string]bool)
	for name, info := range plan.branches {
		branchesToDelete[name] = info.reason
	}
	for name, status := range deleteStatuses {
		if status.HasUnpushedChanges {
			unpushedBranches[name] = true
		}
	}

	return &BranchDeletionPlan{
		BranchesToDelete:       branchesToDelete,
		BranchesWithNewParents: branchesWithNewParents,
		SkippedInWorktree:      skippedInWorktree,
		UtilityBranches:        utilityBranches,
		UnpushedBranches:       unpushedBranches,
		plan:                   plan,
		reparentMoves:          reparentMoves,
		deleteStatuses:         deleteStatuses,
	}, nil
}

// ExecuteBranchDeletions executes a previously planned deletion.
// The branchesToDelete parameter allows filtering which branches from the plan to actually delete.
// If nil, all planned branches are deleted.
func ExecuteBranchDeletions(ctx *app.Context, plannedDeletion *BranchDeletionPlan, branchesToDelete map[string]bool) (*CleanBranchesResult, error) {
	plan := plannedDeletion.plan
	branchesWithNewParents := plannedDeletion.BranchesWithNewParents
	reparentMoves := plannedDeletion.reparentMoves

	// If branchesToDelete filter is provided, remove branches not in the filter
	if branchesToDelete != nil {
		filteredStatuses := make(engine.DeletionStatuses)
		for name := range plannedDeletion.plan.branches {
			if !branchesToDelete[name] {
				continue
			}
			status := plannedDeletion.deleteStatuses.For(name)
			if !status.SafeToDelete {
				info := plannedDeletion.plan.branches[name]
				status = engine.DeletionStatus{
					SafeToDelete: true,
					Reason:       info.reason,
					Kind:         info.reasonKind,
				}
			}
			filteredStatuses[name] = status
		}

		plan, branchesWithNewParents, reparentMoves = buildDeletionPlan(ctx, filteredStatuses)
	}

	// Record each planned branch's reason before execution, which removes them
	// from plan.branches. Only the names execution actually deleted are
	// reported back, so a branch it declined to touch is not announced as gone.
	reasons := make(map[string]string, len(plan.branches))
	for name, info := range plan.branches {
		reasons[name] = info.reason
	}

	if err := applyReparentMoves(ctx, reparentMoves); err != nil {
		return nil, err
	}

	// Execute deletions
	deleted, skippedCheckedOut, err := executeDeletions(ctx, plan)
	if err != nil {
		return nil, err
	}

	deletedBranches := make(map[string]string, len(deleted))
	for _, name := range deleted {
		deletedBranches[name] = reasons[name]
	}

	return &CleanBranchesResult{
		DeletedBranches:        deletedBranches,
		BranchesWithNewParents: branchesWithNewParents,
		SkippedInWorktree:      plannedDeletion.SkippedInWorktree,
		SkippedCheckedOut:      skippedCheckedOut,
	}, nil
}

// identifyBranchesToDelete pre-calculates deletion status for all tracked branches.
// Returns the branches to delete, any branches that were skipped due to being in a worktree,
// and which branches are utility branches (e.g., consolidated merge branches).
func identifyBranchesToDelete(ctx *app.Context, opts CleanBranchesOptions) (engine.DeletionStatuses, []string, map[string]bool, error) {
	eng := ctx.Engine
	c := ctx.Context

	ctx.Logger.Info("identifyBranchesToDelete started force=%v inManagedWorktree=%v", opts.Force, opts.InManagedWorktree)
	identifyStart := time.Now()

	// Collect non-trunk candidate branch names
	allTrackedBranches := eng.AllBranches()
	candidateNames := allTrackedBranches.WithoutTrunk().Names()

	// Single batch call to engine for deletion statuses
	batchStart := time.Now()
	statuses, err := eng.GetDeletionStatuses(c, candidateNames)
	ctx.Logger.Info("GetDeletionStatuses completed durationMs=%d candidateCount=%d ok=%v",
		time.Since(batchStart).Milliseconds(), len(candidateNames), err == nil)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to get deletion statuses: %w", err)
	}

	deleteStatuses := make(engine.DeletionStatuses) // name -> status
	utilityBranches := make(map[string]bool)        // branches that are utility type
	var skippedInWorktree []string
	remoteStatuses := opts.RemoteStatuses
	if remoteStatuses == nil {
		branchesNeedingRemoteStatus := engine.NewBranchesBuilder(len(candidateNames))
		for _, name := range candidateNames {
			if statuses.For(name).SafeToDelete {
				branchesNeedingRemoteStatus.Add(eng.GetBranch(name))
			}
		}
		remoteCtx, cancelRemote := ctx.RemoteOperationContext()
		remoteStatuses = eng.ReadBranchRemoteStatuses(remoteCtx, branchesNeedingRemoteStatus.Build())
		cancelRemote()
	}

	for _, name := range candidateNames {
		status := statuses.For(name)
		if !status.SafeToDelete {
			continue
		}

		// Skip current branch if in a managed worktree (can't checkout trunk to delete it)
		if opts.InManagedWorktree && name == opts.CurrentBranch {
			skippedInWorktree = append(skippedInWorktree, name)
			ctx.Logger.Info("identifyBranchesToDelete skipped (worktree) branch=%v", name)
			continue
		}

		// Check if local branch has unpushed changes relative to remote
		branch := eng.GetBranch(name)
		remoteStatus := remoteStatuses[name]
		// A missing remote branch cannot prove that the local tip was pushed.
		// This is common after a closed PR's remote branch is deleted; treating
		// it as clean would make non-interactive sync discard local follow-up
		// commits without a prompt. Preserve the branch until an explicit user
		// action decides otherwise.
		if remoteStatus.Ahead() || remoteStatus.Diverged() ||
			(status.Kind == engine.DeletionReasonClosedPR && remoteStatus.MissingRemote()) {
			status.HasUnpushedChanges = true
			ctx.Logger.Info("identifyBranchesToDelete branch has unpushed or unverifiable changes branch=%v ahead=%v diverged=%v missingRemote=%v", name, remoteStatus.Ahead(), remoteStatus.Diverged(), remoteStatus.MissingRemote())
		}

		deleteStatuses[name] = status

		// Track utility branches using in-memory branch state (no extra fetch)
		if eng.GetBranchType(branch) == git.BranchTypeUtility {
			utilityBranches[name] = true
		}

		ctx.Logger.Info("identifyBranchesToDelete marked for deletion branch=%v reason=%v unpushed=%v", name, status.Reason, status.HasUnpushedChanges)
	}

	ctx.Logger.Info("identifyBranchesToDelete completed durationMs=%d toDeleteCount=%v skippedCount=%v",
		time.Since(identifyStart).Milliseconds(), len(deleteStatuses), len(skippedInWorktree))

	return deleteStatuses, skippedInWorktree, utilityBranches, nil
}

// buildDeletionPlan constructs the deletion hierarchy and records parent updates
// for surviving branches. It does not mutate branch metadata.
func buildDeletionPlan(ctx *app.Context, deleteStatuses engine.DeletionStatuses) (*deletionPlan, []string, []plannedReparentMove) {
	eng := ctx.Engine
	out := ctx.Output

	plan := newDeletionPlan()
	branchesWithNewParents := []string{}
	reparentMoves := []plannedReparentMove{}
	visited := make(map[string]bool)

	// Build StackGraph for efficient traversals
	graph := eng.Graph(engine.SortStrategyAlphabetical)

	// Start DFS from trunk children to handle the tracked hierarchy
	trunk := eng.Trunk()
	trunkChildren := graph.ChildBranches(trunk)
	branchesToProcess := make([]string, len(trunkChildren))
	for i, child := range trunkChildren {
		branchesToProcess[i] = child.GetName()
	}

	// Adopt branches stranded by ghost ancestors. If the user manually ran
	// `git branch -D` on a parent, its metadata is still on disk but the git
	// ref is gone — the graph treats children of that ghost as roots, so the
	// trunk-children DFS above misses them entirely. Synthesize deletion
	// entries for each ghost so the existing reparent + cleanup machinery
	// runs against them.
	branchesToProcess = appendStrandedRoots(eng, graph, deleteStatuses, branchesToProcess)

	for len(branchesToProcess) > 0 {
		branchName := branchesToProcess[len(branchesToProcess)-1]
		branchesToProcess = branchesToProcess[:len(branchesToProcess)-1]

		if visited[branchName] {
			continue
		}
		visited[branchName] = true

		status, shouldDelete := deleteStatuses[branchName]
		branch := eng.GetBranch(branchName)
		children := graph.ChildBranches(branch)

		// Add children to DFS stack
		for _, child := range children {
			branchesToProcess = append(branchesToProcess, child.GetName())
		}

		if shouldDelete {
			// Add to plan with its children as initial blockers
			blockers := make(map[string]bool)
			for _, child := range children {
				blockers[child.GetName()] = true
			}
			plan.add(branchName, status, blockers)
			out.Debug("Marked %s for deletion. Reason: %s. Blockers: %v", branchName, status.Reason, blockers)
		} else {
			// Branch is NOT being deleted. Check if it needs a new parent.
			move := planReparentIfNecessary(branch, plan, eng, out)
			if move != nil {
				branchesWithNewParents = append(branchesWithNewParents, branchName)
				reparentMoves = append(reparentMoves, *move)
			}
		}
	}

	// NEW: Handle "orphan" branches (untracked branches identified for deletion)
	for branchName, status := range deleteStatuses {
		if !visited[branchName] {
			// This branch is disconnected from the trunk hierarchy but should still be deleted
			plan.add(branchName, status, make(map[string]bool))
			visited[branchName] = true
			out.Debug("Marked orphan branch %s for deletion. Reason: %s", branchName, status.Reason)
		}
	}

	return plan, branchesWithNewParents, reparentMoves
}

// errMainWorktreeCheckout marks a branch that cannot be deleted because it is
// checked out in the main working tree, which — unlike a linked worktree —
// cannot be removed to free the branch.
var errMainWorktreeCheckout = errors.New("checked out in the main working tree")

// executeDeletions removes worktrees and then deletes every branch that can be
// safely removed from the plan in one engine batch. The planning pass has
// already reparented surviving children, so branches from different
// topological layers can share a single ref update and engine rebuild.
//
// Returns the branches actually deleted and those skipped because they are
// checked out in the main working tree.
func executeDeletions(ctx *app.Context, plan *deletionPlan) ([]string, []string, error) {
	if len(plan.branches) == 0 {
		return nil, nil, nil
	}

	eng := ctx.Engine
	out := ctx.Output
	c := ctx.Context

	// Snapshot the worktree list once for the whole cleanup. Deleting a
	// worktree cannot make another branch appear in a worktree, so this list is
	// sufficient for every branch selected below.
	listStart := time.Now()
	worktrees, err := eng.ListWorktrees(c)
	ctx.Logger.Info("list worktrees for cleanup completed durationMs=%d worktreeCount=%d",
		time.Since(listStart).Milliseconds(), len(worktrees))
	if err != nil {
		out.Debug("Failed to list worktrees for branch cleanup: %v", err)
		worktrees = git.WorktreeList{}
	}

	selectionStart := time.Now()
	worktreesRemoved := 0
	worktreesFailed := 0
	var skippedCheckedOut []string
	branchNames := selectBranchesForDeletion(plan, func(name string) bool {
		removed, removeErr := removeWorktreeIfCheckedOut(c, name, worktrees, eng, out)
		switch {
		case errors.Is(removeErr, errMainWorktreeCheckout):
			// Drop just this branch. Keeping it would fail the atomic ref
			// batch and take every other branch's cleanup down with it.
			skippedCheckedOut = append(skippedCheckedOut, name)
			return false
		case removeErr != nil:
			out.Warn("Could not remove worktree for branch %s: %v", name, removeErr)
			worktreesFailed++
			return false
		}
		if removed != "" {
			worktreesRemoved++
		}
		return true
	}, func(name string) string {
		return getParentName(eng.GetBranch(name))
	})
	ctx.Logger.Info("select branches for cleanup completed durationMs=%d branchCount=%d worktreesRemoved=%d worktreesFailed=%d skippedCheckedOut=%d",
		time.Since(selectionStart).Milliseconds(), len(branchNames), worktreesRemoved, worktreesFailed, len(skippedCheckedOut))

	if len(branchNames) == 0 {
		return nil, skippedCheckedOut, nil
	}

	branches := engine.NewBranchesBuilder(len(branchNames))
	for _, name := range branchNames {
		branches.Add(eng.GetBranch(name))
	}

	engineStart := time.Now()
	_, engineErr := eng.DeleteBranches(c, branches.Build())
	ctx.Logger.Info("engine delete branches completed durationMs=%d branchCount=%d ok=%v",
		time.Since(engineStart).Milliseconds(), len(branchNames), engineErr == nil)
	if engineErr != nil {
		return nil, skippedCheckedOut, fmt.Errorf("failed to delete branches [%s]: %w", strings.Join(branchNames, ", "), engineErr)
	}

	pushStart := time.Now()
	err = eng.DeleteRemoteMetadataForBranches(c, branchNames)
	ctx.Logger.Info("delete remote metadata refs completed durationMs=%d branchCount=%d ok=%v",
		time.Since(pushStart).Milliseconds(), len(branchNames), err == nil)
	if err != nil {
		out.Debug("Failed to batch delete remote metadata: %v", err)
	}

	for _, name := range branchNames {
		out.Info("Deleted branch %s", output.BranchName(name))
	}

	return branchNames, skippedCheckedOut, nil
}

// selectBranchesForDeletion resolves the deletion plan without mutating Git.
// A branch rejected by canDelete is removed from the plan but remains a blocker
// for its parent, preventing an unsafe ancestor deletion. Selected branches are
// returned in children-before-parent order for DeleteBranches.
func selectBranchesForDeletion(plan *deletionPlan, canDelete func(string) bool, parentName func(string) string) []string {
	var selected []string
	for {
		var candidates []string
		for name, info := range plan.branches {
			if len(info.blockers) == 0 {
				candidates = append(candidates, name)
			}
		}
		if len(candidates) == 0 {
			return selected
		}
		sort.Strings(candidates)

		selectedInLayer := make([]string, 0, len(candidates))
		for _, name := range candidates {
			if canDelete(name) {
				selectedInLayer = append(selectedInLayer, name)
				continue
			}
			delete(plan.branches, name)
		}
		if len(selectedInLayer) == 0 {
			return selected
		}

		for _, name := range selectedInLayer {
			delete(plan.branches, name)
			plan.removeBlocker(parentName(name), name)
		}
		selected = append(selected, selectedInLayer...)
	}
}

// getParentName returns the name of the parent branch or trunk if no parent exists
func getParentName(branch engine.Branch) string {
	return branch.GetParentOrTrunk()
}

// appendStrandedRoots adds branches whose metadata-parent no longer exists
// as a git ref to the DFS queue, and synthesizes deletion entries for every
// ghost ancestor along the way. Returns the updated queue.
//
// "Ghost" = present in metadata refs (refs/stackit/metadata/<name>) but no
// corresponding refs/heads/<name>. The most common cause is the user
// running `git branch -D <merged-branch>` directly instead of letting
// stackit clean up.
func appendStrandedRoots(eng engine.Engine, graph *engine.StackGraph, deleteStatuses engine.DeletionStatuses, queue []string) []string {
	trunkName := eng.Trunk().GetName()
	branchNames := eng.BranchNames()

	for _, rootName := range graph.RootBranches() {
		if rootName == trunkName {
			continue
		}
		root := eng.GetBranch(rootName)
		parent := root.GetParent()
		// Skip genuine roots (no metadata parent) and roots whose parent is
		// real — those would already have been visited from trunkChildren.
		if parent == nil || branchNames.Contains(parent.GetName()) {
			continue
		}

		// Walk the ghost chain, registering each ghost for deletion.
		ghostName := parent.GetName()
		for ghostName != "" && ghostName != trunkName && !branchNames.Contains(ghostName) {
			if _, already := deleteStatuses[ghostName]; already {
				break
			}
			kind := engine.DeletionReasonGhost
			reason := "branch no longer exists locally"
			if meta, err := eng.ReadMetadataRaw(ghostName); err == nil && meta != nil {
				if pr := meta.GetPrInfo(); pr != nil && pr.State != nil && *pr.State == git.PRStateMerged {
					kind = engine.DeletionReasonMergedPR
					reason = "branch deleted locally; PR was merged"
				}
			}
			deleteStatuses[ghostName] = engine.DeletionStatus{
				SafeToDelete: true,
				Reason:       reason,
				Kind:         kind,
			}
			ghostBranch := eng.GetBranch(ghostName)
			ghostParent := ghostBranch.GetParent()
			if ghostParent == nil {
				break
			}
			ghostName = ghostParent.GetName()
		}

		queue = append(queue, rootName)
	}
	return queue
}

// planReparentIfNecessary records a parent update if the branch's current parent
// is being deleted. It returns nil when no parent change is needed.
func planReparentIfNecessary(branch engine.Branch, plan *deletionPlan, eng engine.Engine, out output.Output) *plannedReparentMove {
	branchName := branch.GetName()
	parentName := getParentName(branch)

	// Find nearest ancestor that isn't being deleted
	newParentName := eng.FindNearestNonExcludedAncestor(parentName, plan.isDeleting)

	// If parent changed, update it
	if newParentName != parentName {
		reparentOpts := buildReparentOptions(plan, parentName)
		out.Debug("Planned parent update for %s from %s to %s.", branchName, parentName, newParentName)

		// Remove this branch as a blocker for its old parent in the plan
		plan.removeBlocker(parentName, branchName)
		return &plannedReparentMove{
			branchName:         branchName,
			newParentName:      newParentName,
			preserveDivergence: reparentOpts.preserveDivergence,
		}
	}

	return nil
}

type reparentOptions struct {
	// Preserve existing divergence point when changing parent.
	preserveDivergence bool
}

func buildReparentOptions(plan *deletionPlan, oldParentName string) reparentOptions {
	return reparentOptions{
		preserveDivergence: shouldPreserveDivergenceOnReparent(plan, oldParentName),
	}
}

// Preserve divergence when old parent is being removed as merged/empty.
// This avoids replaying parent commits after squash merge cleanup.
func shouldPreserveDivergenceOnReparent(plan *deletionPlan, oldParentName string) bool {
	info, ok := plan.branches[oldParentName]
	if !ok || info == nil {
		return false
	}

	switch info.reasonKind {
	case engine.DeletionReasonMergedPR, engine.DeletionReasonMergedIntoTrunk, engine.DeletionReasonEmptyWithPR:
		return true
	default:
		return false
	}
}

func applyReparent(ctx context.Context, eng engine.Engine, branch engine.Branch, newParentName string, opts reparentOptions) error {
	newParent := eng.GetBranch(newParentName)
	if opts.preserveDivergence {
		return eng.ReparentBranch(ctx, branch, newParent)
	}
	return eng.SetParent(ctx, branch, newParent, engine.DivergenceRecompute)
}

func applyReparentMoves(ctx *app.Context, moves []plannedReparentMove) error {
	if len(moves) == 0 {
		return nil
	}

	eng := ctx.Engine
	for _, move := range moves {
		branch := eng.GetBranch(move.branchName)
		if err := applyReparent(ctx.Context, eng, branch, move.newParentName, reparentOptions{
			preserveDivergence: move.preserveDivergence,
		}); err != nil {
			return fmt.Errorf("failed to set parent for %s: %w", move.branchName, err)
		}
		ctx.Output.Info("Set parent of %s to %s.",
			output.BranchName(move.branchName),
			output.BranchName(move.newParentName))
	}
	return nil
}

// removeWorktreeIfCheckedOut removes the worktree if the branch is checked out in one.
// Returns the worktree path that was removed (or empty string if none), and any error.
//
// The caller passes a precomputed WorktreeList so we don't re-invoke
// `git worktree list` per branch when cleaning a batch.
//
// Error handling strategy:
//   - Errors when *removing* a worktree are returned because they indicate a real problem
//     that would prevent the branch from being deleted cleanly.
func removeWorktreeIfCheckedOut(ctx context.Context, branchName string, worktrees git.WorktreeList, eng engine.Engine, out output.Output) (string, error) {
	worktreePath := worktrees.PathForBranch(branchName)
	if worktreePath == "" {
		return "", nil // Branch not in any worktree
	}

	// The main working tree can never be removed to free its branch.
	if worktrees.IsMain(worktreePath) {
		// Unless it is where we are running: DeleteBranches checks out trunk
		// when the batch contains its own HEAD, which frees the branch.
		if git.IsMainWorktree(worktreePath, eng.GetRepoRoot()) {
			out.Debug("Branch %s is checked out here; deletion will switch to trunk first", branchName)
			return "", nil
		}
		// Someone else's checkout. Report it rather than claiming the branch is
		// ready to delete: refs are deleted in one atomic batch, so git rejects
		// the whole batch over this one branch and nothing gets cleaned.
		out.Debug("Branch %s is checked out in the main worktree at %s, not removing", branchName, worktreePath)
		return "", errMainWorktreeCheckout
	}

	out.Debug("Removing worktree at %s for branch %s", worktreePath, branchName)

	if err := eng.RemoveWorktree(ctx, worktreePath); err != nil {
		return worktreePath, fmt.Errorf("failed to remove worktree at %s for branch %s: %w", worktreePath, branchName, err)
	}

	out.Info("Removed worktree at %s for branch %s", worktreePath, branchName)
	return worktreePath, nil
}
