package actions

import (
	"fmt"
	stdruntime "runtime"
	"slices"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/rerere"
	"github.com/getstackit/stackit/internal/utils"
)

// RestackConflictPolicy controls what RunRestack does when a branch conflicts.
type RestackConflictPolicy int

const (
	// RestackConflictEnterWorkflow stops at the first conflict and leaves the
	// repository mid-rebase for `stackit continue`.
	RestackConflictEnterWorkflow RestackConflictPolicy = iota

	// RestackConflictReport restacks every stack that applies cleanly and
	// reports conflicts without writing rebase state. Use it wherever nobody
	// can resolve a conflict interactively: JSON output, parallel workers, and
	// any caller that is not attached to a terminal.
	RestackConflictReport

	// RestackConflictPrompt reports like RestackConflictReport, then asks the
	// request's Prompter whether to resolve the first conflict now.
	RestackConflictPrompt
)

// RestackRequest describes one restack run. Callers decide which branches to
// restack; RunRestack owns everything after that.
type RestackRequest struct {
	// Branches is restacked as a single group, in topological order.
	Branches engine.Branches

	// groups replaces Branches when PlanRestack has already split the work
	// into independent stacks and computed their engine plans.
	groups []restackPlannedGroup

	ConflictPolicy RestackConflictPolicy
	// Prompter is consulted under RestackConflictPrompt. A nil or
	// non-interactive prompter reports conflicts instead.
	Prompter RestackConflictPrompter

	// PromptRerere allows asking the user before enabling git rerere.
	PromptRerere bool
	// Pauser releases the terminal around the rerere prompt. nil uses the
	// handler when it implements rerere.Pauser.
	Pauser rerere.Pauser

	// Parallel runs independent groups in separate worktrees. It only applies
	// when there is more than one group, and always reports conflicts.
	Parallel bool
	Jobs     int // parallel workers; 0 means NumCPU

	// PRNumber overrides the PR lookup for event enrichment. nil reads the
	// branch's local metadata.
	PRNumber func(branch string) *git.PRNumber
}

// RestackOutcome is the result of RunRestack.
type RestackOutcome struct {
	Summary RestackSummary
	// EnteredConflictWorkflow is true when the user chose to resolve a
	// conflict and the run handed off to the conflict workflow. The handler's
	// OnRestackComplete is not called in that case: the restack is unfinished.
	EnteredConflictWorkflow bool
}

// RunRestack is the restack pipeline every command shares: it enables rerere,
// executes the groups sequentially or in parallel, reports each branch to
// handler as a RestackBranchEvent, and applies the conflict policy.
//
// Worktree holds and snapshots are the caller's responsibility, because the
// commands disagree on scope: restack and sync filter holds before choosing
// branches, and only some callers have a rollback point worth taking.
func RunRestack(ctx *app.Context, req RestackRequest, handler RestackHandler) (RestackOutcome, error) {
	if handler == nil {
		handler = &NullRestackHandler{}
	}
	groups := req.groups
	if groups == nil && len(req.Branches) > 0 {
		groups = []restackPlannedGroup{{sortedBranches: req.Branches}}
	}
	branchCount := 0
	for _, g := range groups {
		branchCount += len(g.sortedBranches)
	}
	if branchCount == 0 {
		return RestackOutcome{}, nil
	}

	pauser := req.Pauser
	if pauser == nil {
		pauser, _ = handler.(rerere.Pauser)
	}
	if _, err := rerere.EnsureEnabled(ctx.Context, ctx.Engine, req.PromptRerere, pauser); err != nil {
		ctx.Output.Warn("Failed to enable git rerere: %v", err)
	}

	handler.OnRestackStart(branchCount)
	tally := newRestackTally(ctx.Engine, handler, req.PRNumber)

	if req.Parallel && len(groups) > 1 {
		err := restackGroupsParallel(ctx, req.Jobs, groups, tally)
		summary := tally.result()
		ctx.Logger.Info("restack completed (parallel) restacked=%v conflicts=%v blocked=%v", summary.Restacked, len(summary.Conflicts), len(summary.Blocked))
		handler.OnRestackComplete(summary)
		return RestackOutcome{Summary: summary}, err
	}

	mode := ConflictModeContinue
	if req.ConflictPolicy == RestackConflictEnterWorkflow {
		mode = ConflictModeEnterWorkflow
	}
	for _, group := range groups {
		groupRoot := group.rootBranch
		progress := func(p RestackProgress) {
			p.StackRoot = groupRoot
			tally.record(p)
		}
		if err := restackBranchesWithPlan(ctx, group.sortedBranches, group.enginePlan, progress, mode, nil); err != nil {
			return RestackOutcome{Summary: tally.result()}, err
		}
	}

	summary := tally.result()
	ctx.Logger.Info("restack completed restacked=%v conflicts=%v blocked=%v", summary.Restacked, len(summary.Conflicts), len(summary.Blocked))

	if req.ConflictPolicy == RestackConflictPrompt && len(summary.Conflicts) > 0 &&
		req.Prompter != nil && req.Prompter.IsInteractive() {
		resolve, err := req.Prompter.PromptResolveConflicts(summary.Conflicts)
		if err != nil {
			return RestackOutcome{Summary: summary}, fmt.Errorf("failed to prompt for conflict resolution: %w", err)
		}
		if resolve {
			// The run used ConflictModeContinue, which held back the whole
			// conflicted stack — ancestors included. Re-run that stack in
			// EnterWorkflow mode so ancestors are applied before entering the
			// conflict; see ResolveConflictWorkflow.
			stack := conflictedStack(ctx.Engine, groups, summary.Conflicts[0])
			return RestackOutcome{Summary: summary, EnteredConflictWorkflow: true}, ResolveConflictWorkflow(ctx, stack)
		}
	}

	handler.OnRestackComplete(summary)
	return RestackOutcome{Summary: summary}, nil
}

// conflictedStack returns the branches of the group containing conflictBranch
// that share its stack root, in the group's topological order. A group can span
// several stacks (sync restacks every reparented branch at once); re-running the
// others in EnterWorkflow mode could stop on an unrelated conflict.
func conflictedStack(eng engine.Engine, groups []restackPlannedGroup, conflictBranch string) engine.Branches {
	root := eng.GetStackRootForBranch(eng.GetBranch(conflictBranch))
	for _, group := range groups {
		if !slices.Contains(group.sortedBranches.Names(), conflictBranch) {
			continue
		}
		builder := engine.NewBranchesBuilder(len(group.sortedBranches))
		for _, branch := range group.sortedBranches {
			if eng.GetStackRootForBranch(branch) == root {
				builder.Add(branch)
			}
		}
		return builder.Build()
	}
	return nil
}

// restackGroupsParallel runs each independent stack group in its own temporary
// worktree. The groups have no shared branches, so rebases + ref updates are
// safe to interleave. Progress is serialized through the tally.
//
// Each worker calls RestackBranchesWithHandler which computes its own engine
// plan against the worktree-local engine; the parent's pre-computed plan in
// restackPlannedGroup.enginePlan is not portable across engines.
func restackGroupsParallel(ctx *app.Context, jobs int, groups []restackPlannedGroup, tally *restackTally) error {
	eng := ctx.Engine

	numJobs := jobs
	if numJobs <= 0 {
		numJobs = stdruntime.NumCPU()
	}
	numJobs = min(numJobs, len(groups))

	// Prune stale worktrees once before creating new ones.
	_ = eng.PruneWorktrees(ctx.Context)

	// Snapshot the parent engine state once — each worktree engine gets a copy.
	snapshot := eng.SnapshotForWorktree()

	// recordGroupFailure attributes every branch in a failed group to the
	// handler as a conflict so the final summary reflects what the worker did
	// not process. Without this a worktree/engine setup failure silently shows
	// "skipped=0" while an entire stack failed to start.
	recordGroupFailure := func(group restackPlannedGroup, err error) error {
		for _, b := range group.sortedBranches {
			tally.record(RestackProgress{
				Branch:    b.GetName(),
				Result:    engine.RestackConflict,
				Conflict:  true,
				StackRoot: group.rootBranch,
			})
		}
		return err
	}

	err := utils.RunAll(groups, numJobs, func(group restackPlannedGroup) error {
		// Create a temporary worktree for this group. PruneSkip because we
		// pruned once above and don't want N workers racing on prune.
		wtPath, cleanup, err := eng.CreateTemporaryWorktree(ctx.Context, eng.Trunk().GetName(), "stackit-restack-*", engine.WorktreePruneSkip)
		if err != nil {
			wrappedErr := fmt.Errorf("stack %s: create worktree: %w", group.rootBranch, err)
			ctx.Logger.Warn("failed to create worktree for parallel restack: %v", wrappedErr)
			return recordGroupFailure(group, wrappedErr)
		}
		defer cleanup()

		// Build a per-worktree engine from the snapshot.
		wtEngine, err := newWorktreeEngine(ctx.Context, engine.WorktreeEngineOptions{
			WorktreePath: wtPath,
			Snapshot:     snapshot,
		})
		if err != nil {
			wrappedErr := fmt.Errorf("stack %s: create worktree engine: %w", group.rootBranch, err)
			ctx.Logger.Warn("failed to create worktree engine: %v", wrappedErr)
			return recordGroupFailure(group, wrappedErr)
		}

		// Shallow-copy the app context, swapping in the worktree engine.
		wtCtx := *ctx
		wtCtx.Engine = wtEngine
		wtCtx.RepoRoot = wtPath.String()

		groupRoot := group.rootBranch
		progress := func(p RestackProgress) {
			p.StackRoot = groupRoot
			tally.record(p)
		}

		// Parallel mode always reports conflicts via callback: the interactive conflict
		// workflow writes rebase state into the worktree, which defer cleanup() tears
		// down, so entering it would silently destroy what the user needs to resolve.
		// Per-branch outcomes reach the tally through the callback before any
		// error surfaces, so only the error itself needs recording.
		if err := RestackBranchesWithHandler(&wtCtx, group.sortedBranches, progress, ConflictModeContinue); err != nil {
			wrappedErr := fmt.Errorf("stack %s: %w", group.rootBranch, err)
			ctx.Logger.Warn("parallel restack group failed: %v", wrappedErr)
			return wrappedErr
		}
		return nil
	})

	// Rebuild the main engine's cache so it sees the ref changes made by the
	// worktree engines (they share the same .git directory).
	if err := eng.Rebuild(ctx.Context, eng.Trunk().GetName()); err != nil {
		ctx.Logger.Warn("failed to rebuild engine after parallel restack: %v", err)
	}

	return err
}
