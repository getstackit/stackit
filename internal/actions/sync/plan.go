package sync

import (
	"context"
	"sort"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/utils"
)

// PlanRequest configures a dry-run sync plan.
type PlanRequest struct {
	// Restack previews restacking of every out-of-date tracked branch, as
	// `sync --restack` would.
	Restack bool
}

// PlanEngine is the read-only engine surface PlanDryRun needs.
type PlanEngine interface {
	WorktreeInspector
	Trunk() engine.Branch
	AllBranches() engine.Branches
	ReadBranchRemoteStatuses(ctx context.Context, branches engine.Branches) engine.BranchRemoteStatuses
	ReadBranchStatuses(branches engine.Branches) engine.BranchStatuses
	GetDeletionStatuses(ctx context.Context, branchNames []string) (engine.DeletionStatuses, error)
	GetStackRootForBranch(branch engine.Branch) string
	ListManagedWorktrees() ([]engine.WorktreeInfo, error)
}

// DryRunPlan is the read-only snapshot of what a sync WOULD do, built from the
// current (remote-aware) state. It carries enough detail to render a preview
// that mirrors the live run — target revision, deletion reasons, restack
// parents. The JSON contract is a stable, names-only projection (see Result);
// the richer fields exist only to make the human-readable preview as
// informative as the real sync.
type DryRunPlan struct {
	PullBranch        string              // trunk that would be pulled (empty if up to date)
	PullRevision      string              // short remote SHA the trunk would move to
	Clean             []DryRunCleanItem   // branches that would be deleted
	Restack           []DryRunRestackItem // branches that would be restacked
	RestackStacks     []string            // deduped stack roots covering the restack set (JSON only)
	SkippedStacks     []string            // dirty-worktree anchors whose stacks are skipped
	RestackRequested  bool                // whether --restack was requested
	TrunkStateUnknown bool                // trunk's remote relationship could not be resolved
}

// DryRunCleanItem is a branch a sync would delete, with the reason.
type DryRunCleanItem struct {
	Branch string
	Reason string // e.g. "merged into main", "closed on GitHub"
}

// DryRunRestackItem is a branch a sync would restack onto Parent.
type DryRunRestackItem struct {
	Branch string
	Parent string
}

// Result projects the plan onto the stable JSON contract (branch names only).
// The shape is byte-compatible with what scripts already consume, so the richer
// preview fields are deliberately dropped here.
func (p DryRunPlan) Result() DryRunResult {
	result := DryRunResult{
		WouldPull:          p.PullBranch,
		WouldClean:         []string{},
		WouldRestack:       []string{},
		WouldRestackStacks: p.RestackStacks,
		SkippedStacks:      p.SkippedStacks,
		TrunkStateUnknown:  p.TrunkStateUnknown,
	}
	for _, c := range p.Clean {
		result.WouldClean = append(result.WouldClean, c.Branch)
	}
	for _, r := range p.Restack {
		result.WouldRestack = append(result.WouldRestack, r.Branch)
	}
	return result
}

// PlanDryRun builds a read-only snapshot of what a sync WOULD do from the
// current (remote-aware) state. It never mutates: it probes remote status and
// reads PR/deletion state, but performs no fetch-and-merge, deletion, restack,
// or GitHub write. It is a query rather than a simulation of Action with a
// special handler; the dirty-worktree skip decision is shared with Action via
// dirtyWorktreeStacks so the preview cannot promise work the run then skips.
//
// ctx should be the bounded remote-operation context so every remote-status and
// deletion read shares one deadline.
func PlanDryRun(ctx context.Context, eng PlanEngine, req PlanRequest) DryRunPlan {
	plan := DryRunPlan{RestackRequested: req.Restack}

	// Check if trunk needs to be pulled from remote
	trunk := eng.Trunk()
	remoteStatus := eng.ReadBranchRemoteStatuses(ctx, engine.BranchesOf(trunk)).ForBranch(trunk)
	switch {
	case remoteStatus.Unknown():
		// Report the uncertainty rather than the absence of work: the remote
		// SHA is known but its objects are not local, so Behind() is false for
		// want of a merge base, not because trunk is current.
		plan.TrunkStateUnknown = true
	case remoteStatus.Behind():
		plan.PullBranch = trunk.GetName()
		plan.PullRevision = utils.ShortRevision(remoteStatus.RemoteSha, 0)
	}

	// Collect candidate branches for deletion and restack checks
	allBranches := eng.AllBranches()
	var candidateNames []string
	restackRootSet := make(map[string]struct{})

	// Resolve up-to-date status for every branch in one batched parent-revision
	// read instead of a per-branch IsBranchUpToDate() inside the loop (each of
	// which would shell a separate `git rev-parse` for the parent).
	var statuses engine.BranchStatuses
	if req.Restack {
		statuses = eng.ReadBranchStatuses(allBranches)
	}

	for _, branch := range allBranches {
		if branch.IsTrunk() || !branch.IsTracked() {
			continue
		}
		candidateNames = append(candidateNames, branch.GetName())

		if req.Restack && !statuses.IsUpToDate(branch) {
			plan.Restack = append(plan.Restack, DryRunRestackItem{
				Branch: branch.GetName(),
				Parent: branch.GetParentOrTrunk(),
			})
			if root := eng.GetStackRootForBranch(branch); root != "" {
				restackRootSet[root] = struct{}{}
			}
		}
	}

	// Sort and dedupe restack roots for the current dry-run snapshot. Recompute after
	// running sync before using these roots for a follow-up `restack --stacks`, since
	// cleanup and reparenting can change which roots need work.
	if len(restackRootSet) > 0 {
		roots := make([]string, 0, len(restackRootSet))
		for root := range restackRootSet {
			roots = append(roots, root)
		}
		sort.Strings(roots)
		plan.RestackStacks = roots
	}

	// Batch-check deletion status for all candidates
	if len(candidateNames) > 0 {
		deletions, err := eng.GetDeletionStatuses(ctx, candidateNames)
		if err == nil {
			for _, name := range candidateNames {
				if status, ok := deletions[name]; ok && status.SafeToDelete {
					plan.Clean = append(plan.Clean, DryRunCleanItem{Branch: name, Reason: status.Reason})
				}
			}
		}
	}

	for _, stack := range dirtyWorktreeStacks(ctx, eng) {
		plan.SkippedStacks = append(plan.SkippedStacks, stack.anchor)
	}

	return plan
}

// dirtyWorktreeSource is the engine surface dirtyWorktreeStacks needs.
type dirtyWorktreeSource interface {
	WorktreeInspector
	ListManagedWorktrees() ([]engine.WorktreeInfo, error)
}

// dirtyWorktreeStack is a managed worktree whose stack sync must leave alone.
type dirtyWorktreeStack struct {
	anchor string
	reason string
}

// dirtyWorktreeStacks returns the managed worktrees whose stacks sync skips
// this pass, in registry order. Both Action and PlanDryRun use it so the
// preview and the live run make the same skip decision. A failure to list
// worktrees yields no skips, matching sync's best-effort behavior.
func dirtyWorktreeStacks(ctx context.Context, eng dirtyWorktreeSource) []dirtyWorktreeStack {
	managedWorktrees, err := eng.ListManagedWorktrees()
	if err != nil {
		return nil
	}
	var stacks []dirtyWorktreeStack
	for _, wt := range managedWorktrees {
		if reason := SkipReasonForWorktree(ctx, eng, wt.Path); reason != "" {
			stacks = append(stacks, dirtyWorktreeStack{anchor: wt.AnchorBranch, reason: reason})
		}
	}
	return stacks
}
