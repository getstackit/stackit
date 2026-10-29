package restack

import (
	"context"
	"fmt"

	"github.com/getstackit/stackit/internal/config"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/errors"
	"github.com/getstackit/stackit/internal/git"
)

// ApplyEngine lists exactly the engine methods applying a plan calls.
type ApplyEngine interface {
	engine.BranchLookup
	Trunk() engine.Branch
	Graph(ctx context.Context, strategy engine.SortStrategy) *engine.StackGraph
	CurrentBranch(ctx context.Context) *engine.Branch
	GetCurrentRevision(ctx context.Context) (string, error)
	RestackBranches(ctx context.Context, branches engine.Branches, opts engine.RestackOpts) (engine.RestackBatchResult, error)
	IsRebaseInProgress(ctx context.Context) bool
	RebaseAbort(ctx context.Context) error
	CheckoutBranch(ctx context.Context, branch engine.Branch) error
	Detach(ctx context.Context, revision string) error
	LastSnapshotID() string
	ListWorktrees(ctx context.Context) (git.WorktreeList, error)
	WorktreeRebaseInProgress(ctx context.Context, worktreePath engine.WorktreePath) bool
	WorktreeHasUncommittedChanges(ctx context.Context, worktreePath engine.WorktreePath) (bool, error)
}

// ContinuationStore persists where an interrupted restack resumes.
type ContinuationStore interface {
	Save(state *config.ContinuationState) error
}

// ContinuationFile stores continuation state in the repository at its root.
type ContinuationFile string

// Save implements ContinuationStore.
func (f ContinuationFile) Save(state *config.ContinuationState) error {
	return config.PersistContinuationState(string(f), state)
}

// Executor applies Plans.
type Executor struct {
	Engine ApplyEngine
	Log    Logger
	// Report receives each branch's outcome. Use TextReporter for plain output.
	Report ProgressFunc
	// Continuations and Conflicts are used only when a plan enters the
	// conflict workflow.
	Continuations ContinuationStore
	Conflicts     *ConflictReporter
}

// Execute applies plan. Under ConflictEnterWorkflow, a conflict applies the
// branches before it, leaves the repository mid-rebase on the first
// conflicting branch, and returns a ConflictWorkflowError; resume records
// the interrupted command when Execute is itself resuming one (stackit
// continue), and is nil otherwise. Under ConflictContinue, every stack free
// of conflicts is applied and the rest are reported.
func (e Executor) Execute(ctx context.Context, plan *Plan, mode ConflictMode, resume *config.ContinuationState) error {
	if len(plan.Branches) == 0 {
		return nil
	}
	if plan.Empty() {
		e.reportPlanned(ctx, plan)
		return nil
	}

	apply, blocked := plan.Apply, plan.Blocked
	// Non-interactive restacks apply stacks atomically: a conflict anywhere in
	// an independent stack holds back the whole stack. Applying only part of a
	// stack would move a parent without its children, turning a benign
	// "behind trunk" state into "child not restacked on parent" — which
	// downstream tooling (submit validation) treats as a hard error.
	// The conflict workflow is exempt: it applies up to the conflict on
	// purpose so the user resolves on top of the restacked parent, and
	// 'stackit continue' finishes the rest of the stack.
	if mode == ConflictContinue && len(plan.Conflicts) > 0 {
		apply, blocked = holdBackConflictedStacks(ctx, e.Engine, apply, plan.Conflicts, blocked)
	}

	if mode == ConflictEnterWorkflow && len(plan.Conflicts) > 0 {
		if len(apply) > 0 {
			if _, err := e.Engine.RestackBranches(ctx, apply, e.restackOpts(ctx, plan)); err != nil {
				return fmt.Errorf("failed to restack branches before conflict: %w", err)
			}
		}
		return e.enterConflictWorkflow(ctx, plan.Conflicts[0], plan.Branches, resume)
	}

	if len(apply) > 0 {
		if err := e.apply(ctx, plan, apply, mode); err != nil {
			return err
		}
	}
	e.reportPlanned(ctx, plan)

	if mode == ConflictContinue {
		current := currentBranchName(ctx, e.Engine)
		for _, name := range plan.Conflicts {
			e.Report(Progress{Branch: name, Result: engine.RestackConflict, Conflict: true, IsCurrent: name == current})
		}
		for _, name := range blocked {
			e.Report(Progress{Branch: name, Result: engine.RestackBlocked, IsCurrent: name == current})
		}
	}
	return nil
}

func (e Executor) restackOpts(ctx context.Context, plan *Plan) engine.RestackOpts {
	current := currentBranchName(ctx, e.Engine)
	return engine.RestackOpts{
		Validation: plan.Validation,
		Plan:       plan.Engine,
		Progress: func(branch engine.Branch, result engine.RestackBranchResult) {
			e.reportResult(ctx, branch, result, current)
		},
	}
}

func (e Executor) apply(ctx context.Context, plan *Plan, apply engine.Branches, mode ConflictMode) error {
	// Keep track of where we started so a runtime conflict that slips past
	// validation can be cleaned up without leaving a rebase in progress.
	originalBranch := e.Engine.CurrentBranch(ctx)
	originalRev := ""
	if originalBranch == nil {
		originalRev, _ = e.Engine.GetCurrentRevision(ctx)
	}

	batch, err := e.Engine.RestackBranches(ctx, apply, e.restackOpts(ctx, plan))
	if err != nil {
		return fmt.Errorf("batch restack failed: %w", err)
	}

	// Report mode must never leave the repo in a conflict workflow. If a
	// runtime conflict occurred despite validation, abort it and restore the
	// checkout.
	if mode == ConflictContinue && e.Engine.IsRebaseInProgress(ctx) {
		conflictBranch := batch.ConflictBranch
		if conflictBranch == "" {
			conflictBranch = "unknown"
		}

		e.Log.Warn("unexpected rebase state after sync restack; aborting branch=%v", conflictBranch)
		if abortErr := e.Engine.RebaseAbort(ctx); abortErr != nil {
			return fmt.Errorf("unexpected restack conflict on %s: failed to abort rebase: %w", conflictBranch, abortErr)
		}

		switch {
		case originalBranch != nil:
			if checkoutErr := e.Engine.CheckoutBranch(ctx, *originalBranch); checkoutErr != nil {
				return fmt.Errorf("unexpected restack conflict on %s: aborted rebase but failed to restore branch %s: %w",
					conflictBranch, originalBranch.Name(), checkoutErr)
			}
		case originalRev != "":
			if detachErr := e.Engine.Detach(ctx, originalRev); detachErr != nil {
				return fmt.Errorf("unexpected restack conflict on %s: aborted rebase but failed to restore detached HEAD %s: %w",
					conflictBranch, originalRev, detachErr)
			}
		}
	}

	e.logResults(batch.Results)
	return nil
}

// reportPlanned streams outcomes the engine decided at planning time
// (locked, frozen, anchor, held). Branches that go through the rebase loop
// are reported by the engine's progress callback instead.
func (e Executor) reportPlanned(ctx context.Context, plan *Plan) {
	if plan.Engine == nil || len(plan.Engine.PlannedResults) == 0 {
		return
	}
	e.logResults(plan.Engine.PlannedResults)
	current := currentBranchName(ctx, e.Engine)
	for _, branch := range plan.Branches {
		if result, ok := plan.Engine.PlannedResults[branch.Name()]; ok {
			e.reportResult(ctx, branch, result, current)
		}
	}
}

func (e Executor) reportResult(ctx context.Context, branch engine.Branch, result engine.RestackBranchResult, current string) {
	// Use the revision the engine captured during restack. RestackUnneeded
	// leaves it empty, and the common Done path needs no extra git call.
	newRev := ""
	if result.Result == engine.RestackDone {
		rev := result.NewRev
		if rev == "" {
			rev, _ = branch.Revision(ctx)
		}
		newRev = git.ShortRevision(rev, 0)
	}
	e.Report(Progress{
		Branch:              branch.Name(),
		Result:              result.Result,
		NewRev:              newRev,
		RerereResolvedCount: result.RerereResolvedCount,
		LockReason:          result.LockReason,
		Frozen:              result.Frozen,
		HeldBy:              result.HeldBy,
		IsCurrent:           branch.Name() == current,
		Reparented:          result.Reparented,
		OldParent:           result.OldParent,
		NewParent:           result.NewParent,
		branch:              branch,
	})
}

func (e Executor) logResults(results map[string]engine.RestackBranchResult) {
	for name, result := range results {
		e.Log.Info("restack result branch=%v result=%v reparented=%v oldParent=%v newParent=%v",
			name, resultName(result.Result), result.Reparented, result.OldParent, result.NewParent)
	}
}

// enterConflictWorkflow leaves the repository mid-rebase on firstConflict and
// records how `stackit continue` resumes. resume carries the original
// command's rollback and return destination when continue reaches another
// conflicted branch. Its empty snapshot ID is also authoritative: a command
// without a rollback point must never inherit one.
func (e Executor) enterConflictWorkflow(ctx context.Context, firstConflict string, allBranches engine.Branches, resume *config.ContinuationState) error {
	// Detach HEAD before the real rebase. Validation already told us this rebase
	// will conflict, so the worktree is about to be left in a long-lived rebase
	// state. If the worktree is still "on a branch" (typically trunk, since
	// `st sync` is usually run from main), a later `git rebase --abort` — by
	// the user, by stackit's own error paths, or even by another `st sync` —
	// can do `git reset --hard orig-head` and move that branch's ref to the
	// failed rebase's target, corrupting the branch. Detaching first ensures
	// abort can only touch HEAD, never a branch ref. `st continue` already
	// re-attaches to the conflict branch on success.
	// reattach undoes the detach below when the workflow is NOT entered after
	// all (rebase errored without leaving a conflict, or unexpectedly completed
	// clean). Failing on a detached HEAD would strand the user off their
	// branch, violating the no-detached-HEAD invariant. Best-effort and gated
	// on no rebase being in progress: checkout would fail on an unmerged index
	// anyway, and a live conflict is the one state where detached is correct.
	reattach := func() {}
	if currentBranch := e.Engine.CurrentBranch(ctx); currentBranch != nil {
		currentRev, revErr := e.Engine.GetCurrentRevision(ctx)
		if revErr != nil {
			return fmt.Errorf("failed to read HEAD before conflict workflow: %w", revErr)
		}
		if detachErr := e.Engine.Detach(ctx, currentRev); detachErr != nil {
			return fmt.Errorf("failed to detach HEAD before conflict workflow: %w", detachErr)
		}
		originalBranch := *currentBranch
		reattach = func() {
			if !e.Engine.IsRebaseInProgress(ctx) {
				_ = e.Engine.CheckoutBranch(ctx, originalBranch)
			}
		}
	}

	// Perform rebase to enter conflict state
	conflictBranch := e.Engine.GetBranch(firstConflict)
	expectedBranchRevision, err := conflictBranch.Revision(ctx)
	if err != nil {
		reattach()
		return fmt.Errorf("failed to get branch revision before conflict workflow: %w", err)
	}
	batch, err := e.Engine.RestackBranches(ctx, engine.BranchesOf(conflictBranch), engine.RestackOpts{})
	if err != nil {
		reattach()
		return fmt.Errorf("failed to enter conflict state for %s: %w", firstConflict, err)
	}

	// Verify we're actually in conflict state. CurrentBranch is not reliable
	// during an in-progress rebase, so this check is the one that counts.
	if !e.Engine.IsRebaseInProgress(ctx) {
		reattach()
		// A held branch reports RestackUnneeded, exactly like a branch that was
		// already up to date, so "the rebase succeeded" is the wrong diagnosis
		// and sends the user looking for a conflict that never started.
		if reason := HeldWorktreeReason(ctx, e.Engine, firstConflict); reason != "" {
			return fmt.Errorf("cannot resolve the conflict on %s: it was held back because %s; resolve that, then re-run", firstConflict, reason)
		}
		return fmt.Errorf("expected conflict on %s but rebase completed successfully", firstConflict)
	}

	var remaining []string
	foundConflict := false
	for _, branch := range allBranches {
		if foundConflict {
			remaining = append(remaining, branch.Name())
		} else if branch.Name() == firstConflict {
			foundConflict = true
		}
	}

	rebasedBranchBase := ""
	if result, ok := batch.Results[firstConflict]; ok {
		rebasedBranchBase = result.RebasedBranchBase
	}

	continuation := &config.ContinuationState{
		BranchesToRestack:      remaining,
		RebasedBranchBase:      rebasedBranchBase,
		CurrentBranchOverride:  firstConflict,
		ExpectedBranchRevision: expectedBranchRevision,
		// Bind abort's rollback to the snapshot this command took. Empty when
		// the command took none, which abort reads as "no rollback point"
		// rather than reaching for an unrelated command's snapshot.
		SnapshotID: e.Engine.LastSnapshotID(),
	}
	if resume != nil {
		continuation.SnapshotID = resume.SnapshotID
		continuation.ReturnToBranch = resume.ReturnToBranch
	}

	if err := e.Continuations.Save(continuation); err != nil {
		return fmt.Errorf("failed to persist continuation: %w", err)
	}
	if err := e.Conflicts.Print(ctx, firstConflict); err != nil {
		return fmt.Errorf("failed to print conflict status: %w", err)
	}
	return errors.NewConflictWorkflowError(firstConflict)
}

// holdBackConflictedStacks enforces per-stack atomicity: any branch whose
// independent stack (rooted at a child of trunk) contains a validation
// conflict is moved from the apply set into the blocked set. Returns the
// filtered apply set and the extended blocked list.
func holdBackConflictedStacks(ctx context.Context, eng ApplyEngine, success engine.Branches, conflicts, blocked []string) (engine.Branches, []string) {
	graph := eng.Graph(ctx, engine.SortStrategyAlphabetical)
	trunk := eng.Trunk().Name()
	rootOf := func(name string) string {
		cur := name
		for {
			node := graph.GetNode(cur)
			if node == nil || node.Parent == "" || node.Parent == trunk {
				return cur
			}
			cur = node.Parent
		}
	}

	conflictedRoots := make(map[string]bool, len(conflicts))
	for _, name := range conflicts {
		conflictedRoots[rootOf(name)] = true
	}

	kept := engine.NewBranchesBuilder(len(success))
	for _, branch := range success {
		if conflictedRoots[rootOf(branch.Name())] {
			blocked = append(blocked, branch.Name())
			continue
		}
		kept.Add(branch)
	}
	return kept.Build(), blocked
}

// WorktreeInspector is the engine surface HeldWorktreeReason reads.
type WorktreeInspector interface {
	ListWorktrees(ctx context.Context) (git.WorktreeList, error)
	WorktreeRebaseInProgress(ctx context.Context, worktreePath engine.WorktreePath) bool
	WorktreeHasUncommittedChanges(ctx context.Context, worktreePath engine.WorktreePath) (bool, error)
}

// HeldWorktreeReason returns why branchName's own checkout blocks a restack,
// or "" when nothing does.
//
// Restack holds a branch whose worktree it cannot safely reset, and a held
// branch is reported as RestackUnneeded — the same status as a branch that
// needed no work. Callers that expected a rebase to start need to tell those
// apart, because the remedy lives in the other worktree.
//
// Only the branch's own checkout is examined. A branch held because an ancestor
// is held returns "", and the caller falls back to its generic message.
func HeldWorktreeReason(ctx context.Context, eng WorktreeInspector, branchName string) string {
	worktrees, err := eng.ListWorktrees(ctx)
	if err != nil {
		return ""
	}
	path := worktrees.PathForBranch(branchName)
	if path == "" {
		return ""
	}
	if eng.WorktreeRebaseInProgress(ctx, path) {
		return fmt.Sprintf("worktree %s has a rebase in progress", path)
	}
	hasChanges, inspectErr := eng.WorktreeHasUncommittedChanges(ctx, path)
	switch {
	case inspectErr != nil:
		return fmt.Sprintf("worktree %s could not be inspected: %v", path, inspectErr)
	case hasChanges:
		return fmt.Sprintf("worktree %s has uncommitted changes", path)
	}
	return ""
}

func currentBranchName(ctx context.Context, eng interface {
	CurrentBranch(ctx context.Context) *engine.Branch
}) string {
	if b := eng.CurrentBranch(ctx); b != nil {
		return b.Name()
	}
	return ""
}

// resultName returns the diagnostic string for a RestackResult.
func resultName(r engine.RestackResult) string {
	switch r {
	case engine.RestackDone:
		return "done"
	case engine.RestackUnneeded:
		return "unneeded"
	case engine.RestackConflict:
		return "conflict"
	}
	return "unknown"
}

// Runner plans and applies in one call, for the common case of restacking a
// set of branches and stopping at the first conflict.
type Runner struct {
	Planner  Planner
	Executor Executor
}

// Restack plans branches and applies the plan, entering the conflict
// workflow on the first conflict.
func (r Runner) Restack(ctx context.Context, branches engine.Branches) error {
	return r.RestackWithReport(ctx, branches, nil, ConflictEnterWorkflow)
}

// RestackWithReport is Restack with a progress sink and conflict mode. A nil
// report keeps the executor's own.
func (r Runner) RestackWithReport(ctx context.Context, branches engine.Branches, report ProgressFunc, mode ConflictMode) error {
	plan, err := r.Planner.Plan(ctx, branches, nil)
	if err != nil {
		return err
	}
	exec := r.Executor
	if report != nil {
		exec.Report = report
	}
	return exec.Execute(ctx, plan, mode, nil)
}
