package actions

import (
	"context"
	"errors"
	"fmt"
	stdruntime "runtime"
	"slices"

	"github.com/getstackit/stackit/internal/actions/restack"
	"github.com/getstackit/stackit/internal/engine"
	stackErrors "github.com/getstackit/stackit/internal/errors"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/rerere"
	"github.com/getstackit/stackit/internal/workpool"
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
	// Activity receives live rebase-validation events. nil uses the handler
	// when it implements RestackActivityHandler.
	Activity engine.RebaseProgressFunc

	// newWorktreeEngine builds parallel workers' engines. nil uses
	// engine.NewEngineForWorktree; a test injects failures here rather than
	// through a package variable, which would race other parallel tests.
	newWorktreeEngine worktreeEngineFactory

	// held seeds the summary with branches PlanRestack pruned for worktree
	// holds. They never reach the engine, so only the plan knows them.
	held []RestackHeldInfo

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
	// EnteredConflictWorkflow is true when the run handed off to the conflict
	// workflow, which stopped on a conflict for the user to resolve. The
	// handler's OnRestackComplete is not called in that case: the workflow's
	// continue/abort guidance must be the last thing the user sees.
	EnteredConflictWorkflow bool
}

// RunRestack is the restack pipeline every command shares: it enables rerere,
// executes the groups sequentially or in parallel, reports each branch to
// handler as a RestackBranchEvent, and applies the conflict policy.
//
// Worktree holds and snapshots are the caller's responsibility, because the
// commands disagree on scope: restack and sync filter holds before choosing
// branches, and only some callers have a rollback point worth taking.
//
// Every run that starts reports OnRestackComplete exactly once, marking the
// summary Failed on an unexpected error, except a handoff to the conflict
// workflow (see RestackOutcome.EnteredConflictWorkflow).
func RunRestack(ctx context.Context, deps RestackDeps, req RestackRequest, handler RestackHandler) (RestackOutcome, error) {
	if handler == nil {
		handler = &NullRestackHandler{}
	}
	outcome, started, err := runRestack(ctx, deps, req, handler)
	switch {
	case !started:
	case errors.Is(err, stackErrors.ErrConflictWorkflow):
		outcome.EnteredConflictWorkflow = true
	default:
		// A canceled conflict workflow is the user's choice, not a failure.
		outcome.Summary.Failed = err != nil && !errors.Is(err, stackErrors.ErrCanceled)
		handler.OnRestackComplete(outcome.Summary)
	}
	return outcome, err
}

// runRestack does the work of RunRestack and reports whether the run started
// (had any branch to restack), leaving completion to the caller.
func runRestack(ctx context.Context, deps RestackDeps, req RestackRequest, handler RestackHandler) (RestackOutcome, bool, error) {
	groups := req.groups
	if groups == nil && len(req.Branches) > 0 {
		groups = []restackPlannedGroup{{sortedBranches: req.Branches}}
	}
	branchCount := 0
	for _, g := range groups {
		branchCount += len(g.sortedBranches)
	}
	if branchCount == 0 {
		return RestackOutcome{}, false, nil
	}

	pauser := req.Pauser
	if pauser == nil {
		pauser, _ = handler.(rerere.Pauser)
	}
	if _, err := rerere.EnsureEnabled(ctx, deps.Engine, req.PromptRerere, pauser); err != nil {
		deps.Out.Warn("Failed to enable git rerere: %v", err)
	}

	activity := req.Activity
	if activity == nil {
		activity = RestackActivity(handler)
	}

	handler.OnRestackStart(branchCount)
	tally := newRestackTally(deps.Engine, handler, req.PRNumber)
	tally.summary.Held = slices.Clone(req.held)

	if req.Parallel && len(groups) > 1 {
		newWorktreeEngine := req.newWorktreeEngine
		if newWorktreeEngine == nil {
			newWorktreeEngine = engine.NewEngineForWorktree
		}
		err := restackGroupsParallel(ctx, deps, req.Jobs, groups, tally, activity, newWorktreeEngine)
		summary := tally.result()
		deps.Log.Info("restack completed (parallel) restacked=%v conflicts=%v blocked=%v", summary.Restacked, len(summary.Conflicts), len(summary.Blocked))
		return RestackOutcome{Summary: summary}, true, err
	}

	mode := restack.ConflictContinue
	if req.ConflictPolicy == RestackConflictEnterWorkflow {
		mode = restack.ConflictEnterWorkflow
	}
	for _, group := range groups {
		groupRoot := group.rootBranch
		progress := func(p restack.Progress) {
			p.StackRoot = groupRoot
			tally.record(p)
		}
		if err := restackWithPlan(ctx, deps, group.sortedBranches, group.enginePlan, progress, mode, activity); err != nil {
			return RestackOutcome{Summary: tally.result()}, true, err
		}
	}

	summary := tally.result()
	deps.Log.Info("restack completed restacked=%v conflicts=%v blocked=%v", summary.Restacked, len(summary.Conflicts), len(summary.Blocked))

	if req.ConflictPolicy == RestackConflictPrompt && len(summary.Conflicts) > 0 &&
		req.Prompter != nil && req.Prompter.IsInteractive() {
		resolve, err := req.Prompter.PromptResolveConflicts(summary.Conflicts)
		switch {
		case errors.Is(err, stackErrors.ErrCanceled):
			// Canceling the prompt is a decline: report the conflicts as
			// incomplete work, not as a failed restack.
		case err != nil:
			return RestackOutcome{Summary: summary}, true, fmt.Errorf("failed to prompt for conflict resolution: %w", err)
		case resolve:
			// The run used restack.ConflictContinue, which held back the whole
			// conflicted stack — ancestors included. Entering the conflict
			// directly from that state would rebase the conflict branch onto
			// its never-moved parent, find no conflict, and fail after
			// detaching HEAD. Re-run the stack in EnterWorkflow mode instead
			// (exempt from per-stack atomicity on purpose), so its ancestors
			// apply first and the conflict is entered on top of them.
			stack := conflictedStack(deps.Engine, groups, summary.Conflicts[0])
			workflowErr := restackWithPlan(ctx, deps, stack, nil, nil, restack.ConflictEnterWorkflow, nil)
			if workflowErr == nil {
				// The workflow finished without stopping (e.g. rerere resolved
				// every conflict), so the stack was restacked after all.
				summary = summary.resolveStack(stack.Names())
			}
			return RestackOutcome{Summary: summary}, true, workflowErr
		}
	}

	return RestackOutcome{Summary: summary}, true, nil
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
// Each worker plans its own restack against the worktree-local engine; the
// parent's pre-computed plan in restackPlannedGroup.enginePlan is not portable
// across engines.
func restackGroupsParallel(
	ctx context.Context,
	deps RestackDeps,
	jobs int,
	groups []restackPlannedGroup,
	tally *restackTally,
	activity engine.RebaseProgressFunc,
	newWorktreeEngine worktreeEngineFactory,
) error {
	eng := deps.Engine

	numJobs := jobs
	if numJobs <= 0 {
		numJobs = stdruntime.NumCPU()
	}
	numJobs = min(numJobs, len(groups))

	// Prune stale worktrees once before creating new ones.
	_ = eng.PruneWorktrees(ctx)

	// Snapshot the parent engine state once — each worktree engine gets a copy.
	snapshot := eng.SnapshotForWorktree()

	// recordGroupFailure attributes every branch in a failed group to the
	// handler as a conflict so the final summary reflects what the worker did
	// not process. Without this a worktree/engine setup failure silently shows
	// "skipped=0" while an entire stack failed to start.
	recordGroupFailure := func(group restackPlannedGroup, err error) error {
		for _, b := range group.sortedBranches {
			tally.record(restack.Progress{
				Branch:    b.Name(),
				Result:    engine.RestackConflict,
				Conflict:  true,
				StackRoot: group.rootBranch,
			})
		}
		return err
	}

	err := workpool.RunAll(groups, numJobs, func(group restackPlannedGroup) error {
		// Create a temporary worktree for this group. PruneSkip because we
		// pruned once above and don't want N workers racing on prune.
		wtPath, cleanup, err := eng.CreateTemporaryWorktree(ctx, eng.Trunk().Name(), "stackit-restack-*", engine.WorktreePruneSkip)
		if err != nil {
			wrappedErr := fmt.Errorf("stack %s: create worktree: %w", group.rootBranch, err)
			deps.Log.Warn("failed to create worktree for parallel restack: %v", wrappedErr)
			return recordGroupFailure(group, wrappedErr)
		}
		defer cleanup()

		// Build a per-worktree engine from the snapshot.
		wtEngine, err := newWorktreeEngine(ctx, engine.WorktreeEngineOptions{
			WorktreePath: wtPath,
			Snapshot:     snapshot,
		})
		if err != nil {
			wrappedErr := fmt.Errorf("stack %s: create worktree engine: %w", group.rootBranch, err)
			deps.Log.Warn("failed to create worktree engine: %v", wrappedErr)
			return recordGroupFailure(group, wrappedErr)
		}

		// Copy the deps, swapping in the worktree engine and root.
		wtDeps := deps
		wtDeps.Engine = wtEngine
		wtDeps.RepoRoot = wtPath.String()

		groupRoot := group.rootBranch
		progress := func(p restack.Progress) {
			p.StackRoot = groupRoot
			tally.record(p)
		}

		// Parallel mode always reports conflicts via callback: the interactive conflict
		// workflow writes rebase state into the worktree, which defer cleanup() tears
		// down, so entering it would silently destroy what the user needs to resolve.
		// Per-branch outcomes reach the tally through the callback before any
		// error surfaces, so only the error itself needs recording.
		if err := restackWithPlan(ctx, wtDeps, group.sortedBranches, nil, progress, restack.ConflictContinue, activity); err != nil {
			wrappedErr := fmt.Errorf("stack %s: %w", group.rootBranch, err)
			deps.Log.Warn("parallel restack group failed: %v", wrappedErr)
			return wrappedErr
		}
		return nil
	})

	// Rebuild the main engine's cache so it sees the ref changes made by the
	// worktree engines (they share the same .git directory).
	if err := eng.Rebuild(ctx, eng.Trunk().Name()); err != nil {
		deps.Log.Warn("failed to rebuild engine after parallel restack: %v", err)
	}

	return err
}

// restackerFor builds the restack planner and executor over deps' engine.
// report receives each branch's outcome; nil prints plain text instead.
func restackerFor(deps RestackDeps, report restack.ProgressFunc) (restack.Planner, restack.Executor) {
	if report == nil {
		report = restack.TextReporter(deps.Out)
	}
	return restack.Planner{Engine: deps.Engine, Log: deps.Log}, restack.Executor{
		Engine:        deps.Engine,
		Log:           deps.Log,
		Report:        report,
		Continuations: restack.ContinuationFile(deps.RepoRoot),
		Conflicts:     &restack.ConflictReporter{Engine: deps.Engine, Out: deps.Out, RepoRoot: deps.RepoRoot},
	}
}

// restackWithPlan plans and executes a fresh (non-resumed) restack. When prePlan is non-nil the engine plan is reused
// rather than recomputed; activity, when non-nil, receives live validation events.
func restackWithPlan(ctx context.Context, deps RestackDeps, branches engine.Branches, prePlan *engine.RestackPlan, report restack.ProgressFunc, mode restack.ConflictMode, activity engine.RebaseProgressFunc) error {
	planner, executor := restackerFor(deps, report)
	planner.Activity = activity
	plan, err := planner.Plan(ctx, branches, prePlan)
	if err != nil {
		return err
	}
	return executor.Execute(ctx, plan, mode, nil)
}

// NewRestackRunner builds the restack runner for deps, printing per-branch
// results as plain text.
func NewRestackRunner(deps RestackDeps) restack.Runner {
	planner, executor := restackerFor(deps, nil)
	return restack.Runner{Planner: planner, Executor: executor}
}
