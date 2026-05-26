package actions

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/tui/style"
)

// CleanBranchesOptions contains options for cleaning branches
type CleanBranchesOptions struct {
	Force             bool
	InManagedWorktree bool   // True if running from a stackit-managed worktree
	CurrentBranch     string // Name of the current branch (used to skip deletion in worktree)
}

// CleanBranchesResult contains the result of cleaning branches
type CleanBranchesResult struct {
	DeletedBranches        map[string]string // name -> reason
	BranchesWithNewParents []string
	SkippedInWorktree      []string // branches that couldn't be deleted from worktree
	SkippedUnpushed        []string // branches skipped due to unpushed local changes
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
	plan *deletionPlan
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
	// Phase 1: Identify candidates for deletion
	deleteStatuses, skippedInWorktree, _, err := identifyBranchesToDelete(ctx, opts)
	if err != nil {
		return nil, err
	}

	// Phase 2: Build deletion plan
	plan, branchesWithNewParents, err := buildDeletionPlanAndReparent(ctx, deleteStatuses)
	if err != nil {
		return nil, err
	}

	// Capture planned deletions before executeDeletions removes them from plan.branches
	deletedBranches := make(map[string]string)
	for name, info := range plan.branches {
		deletedBranches[name] = info.reason
	}

	// Phase 3: Execute deletions
	if err := executeDeletions(ctx, plan); err != nil {
		return nil, err
	}

	return &CleanBranchesResult{
		DeletedBranches:        deletedBranches,
		BranchesWithNewParents: branchesWithNewParents,
		SkippedInWorktree:      skippedInWorktree,
	}, nil
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
	plan, branchesWithNewParents, err := buildDeletionPlanAndReparent(ctx, deleteStatuses)
	if err != nil {
		return nil, err
	}

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
	}, nil
}

// ExecuteBranchDeletions executes a previously planned deletion.
// The branchesToDelete parameter allows filtering which branches from the plan to actually delete.
// If nil, all planned branches are deleted.
func ExecuteBranchDeletions(ctx *app.Context, plannedDeletion *BranchDeletionPlan, branchesToDelete map[string]bool) (*CleanBranchesResult, error) {
	// If branchesToDelete filter is provided, remove branches not in the filter
	if branchesToDelete != nil {
		for name := range plannedDeletion.plan.branches {
			if !branchesToDelete[name] {
				delete(plannedDeletion.plan.branches, name)
			}
		}
	}

	// Capture planned deletions before executeDeletions removes them from plan.branches
	deletedBranches := make(map[string]string)
	for name, info := range plannedDeletion.plan.branches {
		deletedBranches[name] = info.reason
	}

	// Execute deletions
	if err := executeDeletions(ctx, plannedDeletion.plan); err != nil {
		return nil, err
	}

	return &CleanBranchesResult{
		DeletedBranches:        deletedBranches,
		BranchesWithNewParents: plannedDeletion.BranchesWithNewParents,
		SkippedInWorktree:      plannedDeletion.SkippedInWorktree,
	}, nil
}

// identifyBranchesToDelete pre-calculates deletion status for all tracked branches.
// Returns the branches to delete, any branches that were skipped due to being in a worktree,
// and which branches are utility branches (e.g., consolidated merge branches).
func identifyBranchesToDelete(ctx *app.Context, opts CleanBranchesOptions) (map[string]engine.DeletionStatus, []string, map[string]bool, error) {
	eng := ctx.Engine
	c := ctx.Context

	ctx.Logger.Info("identifyBranchesToDelete started force=%v inManagedWorktree=%v", opts.Force, opts.InManagedWorktree)

	// Collect non-trunk candidate branch names
	allTrackedBranches := eng.AllBranches()
	candidateNames := allTrackedBranches.WithoutTrunk().Names()

	// Single batch call to engine for deletion statuses
	statuses, err := eng.BatchGetDeletionStatuses(c, candidateNames)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to get deletion statuses: %w", err)
	}

	deleteStatuses := make(map[string]engine.DeletionStatus) // name -> status
	utilityBranches := make(map[string]bool)                 // branches that are utility type
	var skippedInWorktree []string

	for _, name := range candidateNames {
		status := statuses[name]
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
		remoteStatus, err := eng.GetBranchRemoteStatus(branch)
		if err == nil && (remoteStatus.Ahead() || remoteStatus.Diverged()) {
			status.HasUnpushedChanges = true
			ctx.Logger.Info("identifyBranchesToDelete branch has unpushed changes branch=%v ahead=%v diverged=%v", name, remoteStatus.Ahead(), remoteStatus.Diverged())
		}

		deleteStatuses[name] = status

		// Track utility branches using in-memory branch state (no extra fetch)
		if eng.GetBranchType(branch) == git.BranchTypeUtility {
			utilityBranches[name] = true
		}

		ctx.Logger.Info("identifyBranchesToDelete marked for deletion branch=%v reason=%v unpushed=%v", name, status.Reason, status.HasUnpushedChanges)
	}

	ctx.Logger.Info("identifyBranchesToDelete completed toDeleteCount=%v skippedCount=%v", len(deleteStatuses), len(skippedInWorktree))

	return deleteStatuses, skippedInWorktree, utilityBranches, nil
}

// buildDeletionPlanAndReparent constructs the deletion hierarchy and updates parents of surviving branches.
func buildDeletionPlanAndReparent(ctx *app.Context, deleteStatuses map[string]engine.DeletionStatus) (*deletionPlan, []string, error) {
	eng := ctx.Engine
	out := ctx.Output
	c := ctx.Context

	plan := newDeletionPlan()
	branchesWithNewParents := []string{}
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
			newParentName, err := reparentBranchIfNecessary(c, branch, plan, eng, out)
			if err != nil {
				return nil, nil, err
			}
			if newParentName != "" {
				branchesWithNewParents = append(branchesWithNewParents, branchName)
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

	return plan, branchesWithNewParents, nil
}

// executeDeletions greedily deletes unblocked branches from the plan.
func executeDeletions(ctx *app.Context, plan *deletionPlan) error {
	eng := ctx.Engine
	out := ctx.Output
	c := ctx.Context

	previousCount := len(plan.branches)
	for {
		var batchNames []string
		for name, info := range plan.branches {
			if len(info.blockers) == 0 {
				batchNames = append(batchNames, name)
			}
		}

		if len(batchNames) == 0 {
			break
		}

		// Sort for deterministic deletion order (helps with debugging and reproducibility)
		sort.Strings(batchNames)

		// Snapshot the branch→worktree map once for this batch instead of
		// re-running `git worktree list --porcelain` per branch.
		worktreeByBranch, err := eng.Git().WorktreeBranchMap(c)
		if err != nil {
			out.Debug("Failed to list worktrees for branch cleanup: %v", err)
			worktreeByBranch = map[string]string{}
		}

		// Remove any worktrees that have these branches checked out
		var failedWorktreeRemovals []string
		for _, name := range batchNames {
			if _, err := removeWorktreeIfCheckedOut(c, name, worktreeByBranch, eng, out); err != nil {
				out.Warn("Could not remove worktree for branch %s: %v", name, err)
				failedWorktreeRemovals = append(failedWorktreeRemovals, name)
			}
		}

		// Filter out branches where worktree removal failed
		if len(failedWorktreeRemovals) > 0 {
			failedSet := make(map[string]bool)
			for _, name := range failedWorktreeRemovals {
				failedSet[name] = true
				// Remove from plan so we don't try again
				delete(plan.branches, name)
			}

			filteredNames := make([]string, 0, len(batchNames))
			for _, name := range batchNames {
				if !failedSet[name] {
					filteredNames = append(filteredNames, name)
				}
			}
			batchNames = filteredNames
		}

		if len(batchNames) == 0 {
			break // All branches in this batch failed worktree removal
		}

		// Prepare engine branches and track parents
		branches := engine.NewBranchesBuilder(len(batchNames))
		parents := make(map[string]string)
		for _, name := range batchNames {
			branch := eng.GetBranch(name)
			branches.Add(branch)
			parents[name] = getParentName(branch)
		}

		// Batch delete from engine
		if _, err := eng.DeleteBranches(c, branches.Build()); err != nil {
			return fmt.Errorf("failed to delete branches [%s]: %w", strings.Join(batchNames, ", "), err)
		}

		// Batch delete remote metadata
		if err := eng.Git().BatchDeleteRemoteMetadataRefs(c, batchNames); err != nil {
			out.Debug("Failed to batch delete remote metadata: %v", err)
		}

		// Cleanup plan and update parent blockers
		for _, name := range batchNames {
			out.Info("Deleted branch %s", style.ColorBranchName(name, false))
			delete(plan.branches, name)

			parentName := parents[name]
			plan.removeBlocker(parentName, name)
		}

		// Safety check: ensure we're making progress to prevent infinite loops
		currentCount := len(plan.branches)
		if currentCount >= previousCount && currentCount > 0 {
			remaining := make([]string, 0, currentCount)
			for name := range plan.branches {
				remaining = append(remaining, name)
			}
			return fmt.Errorf("no progress made in deletion, %d branches remaining: %s", currentCount, strings.Join(remaining, ", "))
		}
		previousCount = currentCount
	}

	return nil
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
func appendStrandedRoots(eng engine.Engine, graph *engine.StackGraph, deleteStatuses map[string]engine.DeletionStatus, queue []string) []string {
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
			if meta, err := eng.Git().ReadMetadata(ghostName); err == nil && meta != nil {
				if pr := meta.GetPrInfo(); pr != nil && pr.State != nil && *pr.State == "MERGED" {
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

// findNonDeletingAncestor finds the nearest ancestor that is not marked for deletion
func findNonDeletingAncestor(startParent string, plan *deletionPlan, eng engine.Engine) string {
	current := startParent
	for {
		if !plan.isDeleting(current) {
			return current
		}
		branch := eng.GetBranch(current)
		parent := branch.GetParent()
		if parent == nil {
			return eng.Trunk().GetName()
		}
		current = parent.GetName()
	}
}

// reparentBranchIfNecessary updates a branch's parent if its current parent is being deleted.
// Returns the name of the new parent if changed, or empty string if not changed.
func reparentBranchIfNecessary(ctx context.Context, branch engine.Branch, plan *deletionPlan, eng engine.Engine, out output.Output) (string, error) {
	branchName := branch.GetName()
	parentName := getParentName(branch)

	// Find nearest ancestor that isn't being deleted
	newParentName := findNonDeletingAncestor(parentName, plan, eng)

	// If parent changed, update it
	if newParentName != parentName {
		reparentOpts := buildReparentOptions(plan, parentName)
		if err := applyReparent(ctx, eng, branch, newParentName, reparentOpts); err != nil {
			return "", fmt.Errorf("failed to set parent for %s: %w", branchName, err)
		}
		out.Info("Set parent of %s to %s.",
			style.ColorBranchName(branchName, false),
			style.ColorBranchName(newParentName, false))

		// Remove this branch as a blocker for its old parent in the plan
		plan.removeBlocker(parentName, branchName)
		return newParentName, nil
	}

	return "", nil
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
	return eng.SetParent(ctx, branch, newParent)
}

// removeWorktreeIfCheckedOut removes the worktree if the branch is checked out in one.
// Returns the worktree path that was removed (or empty string if none), and any error.
//
// The caller passes a precomputed branch→worktree map so we don't re-invoke
// `git worktree list` per branch when cleaning a batch.
//
// Error handling strategy:
//   - Errors when *removing* a worktree are returned because they indicate a real problem
//     that would prevent the branch from being deleted cleanly.
func removeWorktreeIfCheckedOut(ctx context.Context, branchName string, worktreeByBranch map[string]string, eng engine.Engine, out output.Output) (string, error) {
	worktreePath := worktreeByBranch[branchName]
	if worktreePath == "" {
		return "", nil // Branch not in any worktree
	}

	// Don't remove main worktree (resolve symlinks for comparison, e.g., /var vs /private/var on macOS)
	repoRoot := eng.Git().GetRepoRoot()
	resolvedWorktree, _ := filepath.EvalSymlinks(worktreePath)
	resolvedRoot, _ := filepath.EvalSymlinks(repoRoot)
	if resolvedWorktree == resolvedRoot {
		out.Debug("Branch %s is in main worktree, not removing", branchName)
		return "", nil
	}

	out.Debug("Removing worktree at %s for branch %s", worktreePath, branchName)

	if err := eng.Git().RemoveWorktree(ctx, worktreePath); err != nil {
		return worktreePath, fmt.Errorf("failed to remove worktree at %s for branch %s: %w", worktreePath, branchName, err)
	}

	out.Info("Removed worktree at %s for branch %s", worktreePath, branchName)
	return worktreePath, nil
}
