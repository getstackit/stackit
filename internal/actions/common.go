// Package actions provides high-level business logic for CLI commands.
//
// Each action corresponds to a stackit command (create, submit, sync, etc.)
// and orchestrates operations across the engine, git, and github packages.
//
// Key patterns:
//   - Actions accept runtime.Context which provides Engine, Splog, and other dependencies
//   - Actions are stateless - all state is managed through the Engine interface
//   - Actions handle user interaction through the tui package
//
// Dependencies:
//   - engine: Core branch state management
//   - git: Low-level git operations
//   - tui: User interface and prompts
package actions

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/config"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/errors"
	"github.com/getstackit/stackit/internal/output"
)

// FormatRerereResolved renders the standard message describing how many
// rebase conflicts git rerere auto-resolved during an operation.
func FormatRerereResolved(count int) string {
	return fmt.Sprintf("Resolved %d %s using git rerere.", count, Pluralize("conflict", count))
}

func printRerereResolved(ctx *app.Context, count int) {
	ctx.Output.Info("%s", FormatRerereResolved(count))
}

// TakeBestEffortSnapshot records an undo snapshot and logs failures without
// aborting the action. It is a no-op when undo.enabled is false in config.
func TakeBestEffortSnapshot(ctx *app.Context, opts engine.SnapshotOptions) {
	if ctx.Config != nil && !ctx.Config.UndoEnabled() {
		return
	}
	if err := ctx.Undo().TakeSnapshot(opts); err != nil {
		ctx.Output.Debug("Failed to take snapshot: %v", err)
	}
}

// Restacker is a minimal interface needed for restacking branches
type Restacker interface {
	engine.BranchReader
	engine.SyncManager
}

// RestackProgress describes the outcome of restacking a single branch. It is
// passed to RestackProgressCallback so callers can report progress without
// juggling a long positional parameter list.
type RestackProgress struct {
	Branch              string               // the branch being processed
	Result              engine.RestackResult // Done, Unneeded, Conflict, or Blocked
	NewRev              string               // new revision if restacked (empty otherwise)
	RerereResolvedCount int                  // number of rebase continuations handled by git rerere
	Conflict            bool                 // true if this is a conflict
	LockReason          engine.LockReason    // why the branch is locked (empty if not locked)
	Frozen              bool                 // true if the branch is frozen
	IsCurrent           bool                 // true if this is the current branch
	Reparented          bool                 // true if the branch was reparented
	OldParent           string               // the old parent name if reparented
	NewParent           string               // the new parent name if reparented
	StackRoot           string               // independent stack root (set by multi-stack callers; empty for single-stack)
}

// RestackProgressCallback is called for each branch during restack with a
// RestackProgress describing the outcome.
type RestackProgressCallback func(RestackProgress)

// ConflictMode controls how RestackBranchesWithHandler responds to rebase conflicts.
type ConflictMode int

const (
	// ConflictModeEnterWorkflow stops at the first conflict, writes rebase state
	// to the working tree, and expects the user to resolve it with stackit continue.
	// This is the standalone CLI default.
	ConflictModeEnterWorkflow ConflictMode = iota

	// ConflictModeContinue validates all branches first, restacks non-conflicting
	// ones, and reports conflicts through the progress callback without writing
	// rebase state. Use this in sync mode, parallel workers (rebase state would
	// be torn down with the worktree), and --continue-on-conflict flows.
	ConflictModeContinue
)

// RestackBranches restacks a list of branches using the engine's batch restack method
func RestackBranches(ctx *app.Context, branches engine.Branches) error {
	return RestackBranchesWithHandler(ctx, branches, nil, ConflictModeEnterWorkflow)
}

// RestackBranchesWithHandler restacks branches with optional progress callback.
// See ConflictMode for how mode controls conflict handling.
func RestackBranchesWithHandler(ctx *app.Context, branches engine.Branches, callback RestackProgressCallback, mode ConflictMode) error {
	return restackBranchesWithPlan(ctx, branches, nil, callback, mode)
}

// restackBranchesWithPlan is the implementation of RestackBranchesWithHandler
// with an optional pre-computed engine plan. When prePlan is non-nil, the
// engine.PlanRestack call is skipped — used by RestackAction when the CLI
// already built the plan to gate TUI initialization, so we don't pay for
// roughly 3 git operations per branch twice per invocation.
func restackBranchesWithPlan(ctx *app.Context, branches engine.Branches, prePlan *engine.RestackPlan, callback RestackProgressCallback, mode ConflictMode) error {
	if len(branches) == 0 {
		return nil
	}

	// Log entry point for diagnostics
	ctx.Logger.Info("restack started branches=%v count=%v", branches.Names(), len(branches))

	// Pre-flight validation: check branch ancestry relationships
	if err := validateBranchAncestry(ctx, branches); err != nil {
		return fmt.Errorf("pre-flight validation failed: %w", err)
	}

	// Build rebase specs for validation (or reuse the caller's plan).
	plan := prePlan
	var err error
	if plan == nil {
		plan, err = ctx.Engine.PlanRestack(ctx.Context, branches)
		if err != nil {
			return fmt.Errorf("failed to plan restack: %w", err)
		}
	}
	specs, plannedResults := plan.Specs, plan.PlannedResults
	if len(specs) == 0 && len(plan.ApplyMap) == 0 {
		reportPlannedResults(ctx, branches, plannedResults, callback)
		ctx.Logger.Info("restack no specs built, nothing to do")
		return nil
	}

	// Validate all rebases in a temporary worktree (clean, no side effects)
	validation := &engine.RebaseValidation{Success: true, NewSHAs: map[string]string{}, RerereResolved: map[string]int{}}
	if len(specs) > 0 {
		validation, err = ctx.Engine.ValidateRebases(ctx.Context, specs)
		if err != nil {
			return fmt.Errorf("failed to validate rebases: %w", err)
		}
	}

	// Check if validation failed due to a system error (not a conflict)
	if !validation.Success {
		for _, f := range validation.Failed {
			if f.ErrorType == engine.ValidationErrorSystem {
				return fmt.Errorf("validation failed for %s: %s", f.Branch, f.ErrorMessage)
			}
			// Else: conflict - continue with conflict workflow
			// Log conflicting files for debugging
			if len(f.ConflictingFiles) > 0 {
				ctx.Logger.Debug("conflict detected during validation branch=%v files=%v", f.Branch, f.ConflictingFiles)
			}
		}
	}

	successBranches, conflictBranches, blockedBranches := classifyValidatedBranches(branches, plan, validation)

	// Non-interactive restacks apply stacks atomically: a conflict anywhere in
	// an independent stack holds back the whole stack. Applying only part of a
	// stack would move a parent without its children, turning a benign
	// "behind trunk" state into "child not restacked on parent" — which
	// downstream tooling (submit validation) treats as a hard error.
	// The conflict workflow is exempt: it applies up to the conflict on
	// purpose so the user resolves on top of the restacked parent, and
	// 'stackit continue' finishes the rest of the stack.
	if mode == ConflictModeContinue && len(conflictBranches) > 0 {
		successBranches, blockedBranches = holdBackConflictedStacks(ctx.Engine, successBranches, conflictBranches, blockedBranches)
	}

	// For standalone mode, enter conflict workflow on first conflict
	if mode == ConflictModeEnterWorkflow && len(conflictBranches) > 0 {
		firstConflict := conflictBranches[0]

		// Restack successfully up to the conflict
		if len(successBranches) > 0 {
			currentBranchName := getCurrentBranchName(ctx.Engine)
			if _, err := ctx.Engine.RestackBranchesWithValidatedPlan(ctx.Context, successBranches, validation, plan, func(branch engine.Branch, result engine.RestackBranchResult) {
				reportRestackResult(ctx, branch, result, currentBranchName, callback)
			}); err != nil {
				return fmt.Errorf("failed to restack branches before conflict: %w", err)
			}
		}

		// Enter conflict workflow for the first conflict
		return EnterConflictWorkflow(ctx, firstConflict, branches)
	}

	// For sync mode (or standalone with no conflicts), restack all successful branches
	if len(successBranches) > 0 {
		// Keep track of where we started so sync mode can recover if a runtime conflict
		// slips past validation and leaves a rebase in progress.
		originalBranch := ctx.Engine.CurrentBranch()
		originalRev := ""
		if originalBranch == nil {
			originalRev, _ = ctx.Engine.GetCurrentRevision(ctx.Context)
		}

		currentBranchName := getCurrentBranchName(ctx.Engine)
		batchResult, err := ctx.Engine.RestackBranchesWithValidatedPlan(ctx.Context, successBranches, validation, plan, func(branch engine.Branch, result engine.RestackBranchResult) {
			reportRestackResult(ctx, branch, result, currentBranchName, callback)
		})
		if err != nil {
			return fmt.Errorf("batch restack failed: %w", err)
		}

		// Sync mode should never leave the repo in a conflict workflow unless explicitly requested.
		// If a runtime conflict occurred despite validation, clean it up and restore checkout state.
		if mode == ConflictModeContinue && ctx.Engine.IsRebaseInProgress(ctx.Context) {
			conflictBranch := batchResult.ConflictBranch
			if conflictBranch == "" {
				conflictBranch = "unknown"
			}

			ctx.Logger.Warn("unexpected rebase state after sync restack; aborting branch=%v", conflictBranch)
			if abortErr := ctx.Engine.RebaseAbort(ctx.Context); abortErr != nil {
				return fmt.Errorf("unexpected restack conflict on %s: failed to abort rebase: %w", conflictBranch, abortErr)
			}

			if originalBranch != nil {
				if checkoutErr := ctx.Engine.CheckoutBranch(ctx.Context, *originalBranch); checkoutErr != nil {
					return fmt.Errorf("unexpected restack conflict on %s: aborted rebase but failed to restore branch %s: %w",
						conflictBranch, originalBranch.GetName(), checkoutErr)
				}
			} else if originalRev != "" {
				if detachErr := ctx.Engine.Detach(ctx.Context, originalRev); detachErr != nil {
					return fmt.Errorf("unexpected restack conflict on %s: aborted rebase but failed to restore detached HEAD %s: %w",
						conflictBranch, originalRev, detachErr)
				}
			}
		}

		logRestackResults(ctx, batchResult.Results)
	}
	reportPlannedResults(ctx, branches, plannedResults, callback)

	// Report conflicts and blocked branches via callback for sync mode
	if mode == ConflictModeContinue && (len(conflictBranches) > 0 || len(blockedBranches) > 0) {
		currentBranchName := getCurrentBranchName(ctx.Engine)

		for _, branchName := range conflictBranches {
			if callback != nil {
				callback(RestackProgress{
					Branch:    branchName,
					Result:    engine.RestackConflict,
					Conflict:  true,
					IsCurrent: branchName == currentBranchName,
				})
			}
		}
		for _, branchName := range blockedBranches {
			if callback != nil {
				callback(RestackProgress{
					Branch:    branchName,
					Result:    engine.RestackBlocked,
					IsCurrent: branchName == currentBranchName,
				})
			}
		}
	}

	return nil
}

// holdBackConflictedStacks enforces per-stack atomicity: any branch whose
// independent stack (rooted at a child of trunk) contains a validation
// conflict is moved from the apply set into the blocked set. Returns the
// filtered apply set and the extended blocked list.
func holdBackConflictedStacks(eng engine.BranchReader, success engine.Branches, conflicts, blocked []string) (engine.Branches, []string) {
	graph := eng.Graph(engine.SortStrategyAlphabetical)
	trunk := eng.Trunk().GetName()
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
		if conflictedRoots[rootOf(branch.GetName())] {
			blocked = append(blocked, branch.GetName())
			continue
		}
		kept.Add(branch)
	}
	return kept.Build(), blocked
}

// classifyValidatedBranches splits the planned branches into ones safe to
// apply now, ones that conflicted, and ones blocked behind a conflict, using
// the validation result as the source of truth instead of position-in-specs
// arithmetic. A branch is applied only if its action proves it's safe:
// Frozen/Anchor/MetadataRefresh branches don't touch a rebase worktree at
// all, and Validated branches need a confirmed SHA. Every planned branch
// lands in exactly one of the three buckets so none silently disappears from
// reporting — a Validated branch with no SHA that isn't in the failed set was
// skipped because an ancestor failed, and is classified blocked.
func classifyValidatedBranches(branches engine.Branches, plan *engine.RestackPlan, validation *engine.RebaseValidation) (success engine.Branches, conflicts, blocked []string) {
	if plan == nil || validation == nil {
		return nil, nil, nil
	}
	failed := make(map[string]bool, len(validation.Failed))
	for _, f := range validation.Failed {
		failed[f.Branch] = true
	}
	builder := engine.NewBranchesBuilder(len(branches))
	for _, branch := range branches {
		branchName := branch.GetName()
		if _, exists := plan.ApplyMap[branchName]; !exists {
			continue
		}
		item, ok := plan.Items[branchName]
		if !ok {
			continue
		}

		switch item.Action {
		case engine.RestackPlanApplyFrozen, engine.RestackPlanApplyAnchor, engine.RestackPlanApplyMetadataRefresh:
			builder.Add(branch)
		case engine.RestackPlanApplyValidated:
			switch {
			case validation.NewSHAs[branchName] != "":
				builder.Add(branch)
			case failed[branchName]:
				conflicts = append(conflicts, branchName)
			default:
				blocked = append(blocked, branchName)
			}
		}
	}
	return builder.Build(), conflicts, blocked
}

func getCurrentBranchName(eng engine.BranchReader) string {
	currentBranch := eng.CurrentBranch()
	if currentBranch == nil {
		return ""
	}
	return currentBranch.GetName()
}

// restackResultName returns the diagnostic string for a RestackResult.
func restackResultName(r engine.RestackResult) string {
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

// logRestackResults emits a diagnostic log entry for each restack result.
func logRestackResults(ctx *app.Context, results map[string]engine.RestackBranchResult) {
	for branchName, result := range results {
		ctx.Logger.Info("restack result branch=%v result=%v reparented=%v oldParent=%v newParent=%v",
			branchName, restackResultName(result.Result), result.Reparented, result.OldParent, result.NewParent)
	}
}

// reportPlannedResults streams pre-skipped branch outcomes (locked, frozen,
// anchor) produced by PlanRestack. Branches that go through the actual rebase
// loop are reported via the engine's progress callback instead.
func reportPlannedResults(ctx *app.Context, branches engine.Branches, plannedResults map[string]engine.RestackBranchResult, callback RestackProgressCallback) {
	if len(plannedResults) == 0 {
		return
	}
	logRestackResults(ctx, plannedResults)
	currentBranchName := getCurrentBranchName(ctx.Engine)
	for _, branch := range branches {
		result, ok := plannedResults[branch.GetName()]
		if !ok {
			continue
		}
		reportRestackResult(ctx, branch, result, currentBranchName, callback)
	}
}

func reportRestackResult(ctx *app.Context, branch engine.Branch, result engine.RestackBranchResult, currentBranchName string, callback RestackProgressCallback) {
	branchName := branch.GetName()

	// Use the new revision the engine already captured during restack. Falling
	// back to branch.GetRevision is fine if it's not set (RestackUnneeded
	// leaves NewRev empty), but the common Done path doesn't need an extra
	// git call.
	newRev := ""
	if result.Result == engine.RestackDone {
		rev := result.NewRev
		if rev == "" {
			rev, _ = branch.GetRevision()
		}
		if len(rev) > 7 {
			newRev = rev[:7]
		} else {
			newRev = rev
		}
	}

	// Report via callback if provided.
	if callback != nil {
		callback(RestackProgress{
			Branch:              branchName,
			Result:              result.Result,
			NewRev:              newRev,
			RerereResolvedCount: result.RerereResolvedCount,
			LockReason:          result.LockReason,
			Frozen:              result.Frozen,
			IsCurrent:           branchName == currentBranchName,
			Reparented:          result.Reparented,
			OldParent:           result.OldParent,
			NewParent:           result.NewParent,
		})
		return
	}

	// Log via splog only when no callback is provided.
	if result.Reparented {
		isCurrent := branchName == currentBranchName
		ctx.Output.Info("Reparented %s from %s to %s (parent was merged/deleted).",
			output.Branch(branchName, isCurrent),
			output.BranchName(result.OldParent),
			output.BranchName(result.NewParent))
	}

	switch result.Result {
	case engine.RestackDone:
		parentName := branch.GetParentOrTrunk()
		isCurrent := branchName == currentBranchName
		ctx.Output.Info("Restacked %s on %s.",
			output.Branch(branchName, isCurrent),
			output.BranchName(parentName))
		if result.RerereResolvedCount > 0 {
			printRerereResolved(ctx, result.RerereResolvedCount)
		}
	case engine.RestackUnneeded:
		switch {
		case result.LockReason.IsLocked():
			ctx.Output.Info("%s locked: %s", output.Branch(branchName, branchName == currentBranchName), result.LockReason)
		case result.Frozen:
			ctx.Output.Info("%s frozen", output.Branch(branchName, branchName == currentBranchName))
		case !branch.CanModify():
			if branch.IsLocked() {
				ctx.Output.Info("%s locked: %s", output.Branch(branchName, branchName == currentBranchName), branch.GetLockReason())
			} else {
				ctx.Output.Info("%s frozen", output.Branch(branchName, branchName == currentBranchName))
			}
		case branch.IsTrunk():
			ctx.Output.Info("%s up to date", output.BranchName(branchName))
		default:
			isCurrent := branchName == currentBranchName
			ctx.Output.Info("%s up to date", output.Branch(branchName, isCurrent))
		}
	}
}

// ResolveConflictWorkflow enters the conflict workflow from an interactive
// "resolve conflicts now?" prompt by re-running the conflicted stack in
// ConflictModeEnterWorkflow. The prompt paths run their initial restack in
// ConflictModeContinue, which holds back the ENTIRE conflicted stack — the
// conflict branch's ancestors included. Calling EnterConflictWorkflow directly
// from that state would rebase the conflict branch against its never-moved
// parent, find no conflict, and fail after detaching HEAD. Re-running in
// EnterWorkflow mode applies the ancestors first (that mode is exempt from
// per-stack atomicity on purpose) and then enters the conflict on top of them.
// stackBranches must be the conflicted stack in topological order.
func ResolveConflictWorkflow(ctx *app.Context, stackBranches engine.Branches) error {
	return restackBranchesWithPlan(ctx, stackBranches, nil, nil, ConflictModeEnterWorkflow)
}

// EnterConflictWorkflow performs the rebase to enter conflict state and persists continuation state.
// This helper is shared between RestackBranchesWithHandler (standalone mode) and sync.RunSync (sync mode).
func EnterConflictWorkflow(ctx *app.Context, firstConflict string, allBranches engine.Branches) error {
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
	if currentBranch := ctx.Engine.CurrentBranch(); currentBranch != nil {
		currentRev, revErr := ctx.Engine.GetCurrentRevision(ctx.Context)
		if revErr != nil {
			return fmt.Errorf("failed to read HEAD before conflict workflow: %w", revErr)
		}
		if detachErr := ctx.Engine.Detach(ctx.Context, currentRev); detachErr != nil {
			return fmt.Errorf("failed to detach HEAD before conflict workflow: %w", detachErr)
		}
		originalBranch := *currentBranch
		reattach = func() {
			if !ctx.Engine.IsRebaseInProgress(ctx.Context) {
				_ = ctx.Engine.CheckoutBranch(ctx.Context, originalBranch)
			}
		}
	}

	// Perform rebase to enter conflict state
	conflictBranch := ctx.Engine.GetBranch(firstConflict)
	batchResult, err := ctx.Engine.RestackBranches(ctx.Context, engine.BranchesOf(conflictBranch))
	if err != nil {
		reattach()
		return fmt.Errorf("failed to enter conflict state for %s: %w", firstConflict, err)
	}

	// Verify we're actually in conflict state
	if !ctx.Engine.IsRebaseInProgress(ctx.Context) {
		reattach()
		return fmt.Errorf("expected conflict on %s but rebase completed successfully", firstConflict)
	}

	// Note: We don't verify the current branch here because CurrentBranch() doesn't work
	// correctly during an in-progress rebase. The IsRebaseInProgress check above is sufficient
	// to verify we're in the expected state.

	// Build remaining branches list
	var remainingBranches []string
	foundConflict := false
	for _, branch := range allBranches {
		if foundConflict {
			remainingBranches = append(remainingBranches, branch.GetName())
		} else if branch.GetName() == firstConflict {
			foundConflict = true
		}
	}

	// Get RebasedBranchBase from result
	rebasedBranchBase := ""
	if result, ok := batchResult.Results[firstConflict]; ok {
		rebasedBranchBase = result.RebasedBranchBase
	}

	// Persist continuation state
	continuation := &config.ContinuationState{
		BranchesToRestack:     remainingBranches,
		RebasedBranchBase:     rebasedBranchBase,
		CurrentBranchOverride: firstConflict,
	}

	if err := config.PersistContinuationState(ctx.RepoRoot, continuation); err != nil {
		return fmt.Errorf("failed to persist continuation: %w", err)
	}

	if err := PrintConflictStatus(ctx, firstConflict); err != nil {
		return fmt.Errorf("failed to print conflict status: %w", err)
	}

	return errors.NewConflictWorkflowError(firstConflict)
}

// validateBranchAncestry performs pre-flight checks on branch ancestry relationships.
// Returns an error if any branch has invalid parent relationships.
// Note: Missing parents are tolerated as buildRebaseSpecs will handle auto-reparenting.
func validateBranchAncestry(ctx *app.Context, branches engine.Branches) error {
	// Resolve every branch and parent revision in one batched call instead of
	// a git rev-parse per branch (and per parent).
	names := make(map[string]struct{}, len(branches)*2)
	for _, branch := range branches {
		if branch.IsTrunk() {
			continue
		}
		names[branch.GetName()] = struct{}{}
		if parent := branch.GetParent(); parent != nil {
			names[parent.GetName()] = struct{}{}
		}
	}
	revNames := make([]string, 0, len(names))
	for name := range names {
		revNames = append(revNames, name)
	}
	revisions, _ := ctx.Engine.GetRevisions(revNames)

	for _, branch := range branches {
		branchName := branch.GetName()

		// Trunk branches don't need validation
		if branch.IsTrunk() {
			continue
		}

		// Verify branch itself exists
		branchRev, ok := revisions.Rev(branchName)
		if !ok {
			return fmt.Errorf("branch %s cannot be resolved", branchName)
		}

		// If branch has a parent, validate it only if it exists
		// (missing parents will be handled by auto-reparenting logic)
		parent := branch.GetParent()
		if parent != nil {
			parentName := parent.GetName()
			parentRev, ok := revisions.Rev(parentName)
			if !ok {
				// Parent doesn't exist - this is OK, auto-reparenting will handle it
				ctx.Logger.Debug("parent branch missing, will auto-reparent branch=%v parent=%v", branchName, parentName)
				continue
			}

			// Parent exists - verify they have a common ancestor
			_, err := ctx.Engine.GetMergeBase(ctx.Context, parentRev, branchRev)
			if err != nil {
				return fmt.Errorf("branch %s and parent %s have no common ancestor: %w",
					branchName, parentName, err)
			}
		}
	}
	return nil
}

// wordBranch is the pluralizable noun passed to PluralSuffix/Pluralize by
// callers describing a count of branches.
const wordBranch = "branch"

// PluralSuffix returns the appropriate plural suffix for the given word if plural is true, otherwise empty string
func PluralSuffix(word string, plural bool) string {
	if !plural {
		return ""
	}
	switch word {
	case wordBranch:
		return "es"
	case "PR":
		return "s"
	default:
		return "s"
	}
}

// Pluralize returns the plural form of word if count != 1, otherwise returns the singular form
func Pluralize(word string, count int) string {
	if count == 1 {
		return word
	}
	return word + PluralSuffix(word, true)
}

// PluralIt returns "them" if plural is true, otherwise "it"
func PluralIt(plural bool) string {
	if plural {
		return "them"
	}
	return "it"
}

// SnapshotOption is a function that modifies SnapshotOptions
type SnapshotOption func(*engine.SnapshotOptions)

// NewSnapshot creates a new SnapshotOptions with the given command and options
func NewSnapshot(command string, options ...SnapshotOption) engine.SnapshotOptions {
	opts := engine.SnapshotOptions{
		Command: command,
		Args:    []string{},
	}
	for _, option := range options {
		option(&opts)
	}
	return opts
}

// WithArg appends a single argument if it's not empty
func WithArg(arg string) SnapshotOption {
	return func(opts *engine.SnapshotOptions) {
		if arg != "" {
			opts.Args = append(opts.Args, arg)
		}
	}
}

// WithArgs appends multiple arguments
func WithArgs(args ...string) SnapshotOption {
	return func(opts *engine.SnapshotOptions) {
		opts.Args = append(opts.Args, args...)
	}
}

// WithFlag appends a flag if condition is true
func WithFlag(condition bool, flag string) SnapshotOption {
	return func(opts *engine.SnapshotOptions) {
		if condition {
			opts.Args = append(opts.Args, flag)
		}
	}
}

// WithFlagValue appends a flag with a value if the value is not empty
func WithFlagValue(flag string, value string) SnapshotOption {
	return func(opts *engine.SnapshotOptions) {
		if value != "" {
			opts.Args = append(opts.Args, flag, value)
		}
	}
}

// PrintConflictStatus displays conflict information and instructions to the user.
// Assumes a rebase is in progress: HEAD-side markers reflect the parent/base and
// the ">>>>>>>" side reflects the branch being replayed. Both call sites
// (EnterConflictWorkflow and ContinueAction) operate during rebase.
func PrintConflictStatus(ctx *app.Context, branchName string) error {
	reader := ctx.Reader()
	out := ctx.Output

	msg := output.Red(fmt.Sprintf("Hit conflict restacking %s", branchName))
	out.Info("%s", msg)
	out.Newline()

	// Get unmerged files
	unmergedFiles, err := reader.GetUnmergedFiles(ctx.Context)
	if err == nil && len(unmergedFiles) > 0 {
		out.Info("%s", output.Yellow("Conflicted files:"))
		for _, file := range unmergedFiles {
			sections, sectionErr := conflictMarkerSectionsForFile(ctx.RepoRoot, file)
			if sectionErr != nil || len(sections) == 0 {
				out.Info("  %s", output.Red(file))
				continue
			}
			for _, section := range sections {
				out.Info("  %s (lines %d-%d)",
					output.Red(file),
					section.StartLine,
					section.EndLine,
				)
			}
		}
		out.Newline()
	}

	// Get rebase head
	rebaseHead, err := reader.GetRebaseHead()
	if err == nil && rebaseHead != "" {
		rebaseHeadShort := rebaseHead
		if len(rebaseHead) > 7 {
			rebaseHeadShort = rebaseHead[:7]
		}
		msg := output.Yellow(fmt.Sprintf("You are here (resolving %s):", rebaseHeadShort))
		out.Info("%s", msg)
		// Could show log here if needed
		out.Newline()
	}

	parentBranch := "the current base"
	if branch := ctx.Engine.GetBranch(branchName); branch.GetName() != "" {
		parentBranch = branch.GetParentOrTrunk()
	}

	out.Info("%s", output.Yellow("To resolve:"))
	out.Info("  1. Open each conflicted file and remove conflict markers:")
	out.Info("     %s  (incoming changes from %s)", output.Cyan("<<<<<<< HEAD"), parentBranch)
	out.Info("     %s", output.Cyan("======="))
	out.Info("     %s  (changes from %s)", output.Cyan(">>>>>>>"), branchName)
	out.Info("  2. Stage resolved files: %s", output.Cyan("stackit add <file>"))
	out.Info("  3. Continue the previous Stackit command: %s", output.Cyan("stackit continue"))
	out.Info("  4. Abort and restore the pre-command snapshot: %s", output.Cyan("stackit abort"))
	out.Info("Tip: %s stages all resolved files before continuing.", output.Cyan("stackit continue --all"))

	return nil
}

type conflictMarkerSection struct {
	StartLine int
	EndLine   int
}

func conflictMarkerSectionsForFile(repoRoot, file string) ([]conflictMarkerSection, error) {
	path := file
	if !filepath.IsAbs(path) {
		path = filepath.Join(repoRoot, file)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return findConflictMarkerSections(string(content)), nil
}

// findConflictMarkerSections scans for git conflict regions delimited by
// <<<<<<<, =======, and >>>>>>> markers. Diff3-style ||||||| ancestor markers
// between <<<<<<< and ======= are tolerated (ignored). A new <<<<<<< before a
// section is closed restarts tracking, so unterminated sections do not block
// detection of subsequent complete sections.
func findConflictMarkerSections(content string) []conflictMarkerSection {
	lines := strings.Split(content, "\n")
	sections := make([]conflictMarkerSection, 0)
	startLine := 0
	seenSeparator := false

	for i, line := range lines {
		lineNo := i + 1
		switch {
		case strings.HasPrefix(line, "<<<<<<<"):
			startLine = lineNo
			seenSeparator = false
		case startLine > 0 && strings.HasPrefix(line, "======="):
			seenSeparator = true
		case startLine > 0 && seenSeparator && strings.HasPrefix(line, ">>>>>>>"):
			sections = append(sections, conflictMarkerSection{
				StartLine: startLine,
				EndLine:   lineNo,
			})
			startLine = 0
			seenSeparator = false
		}
	}

	return sections
}
