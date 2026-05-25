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

	"github.com/gertd/go-pluralize"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/config"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/tui/style"
)

var pluralizeClient = pluralize.NewClient()

// FormatRerereResolved renders the standard message describing how many
// rebase conflicts git rerere auto-resolved during an operation.
func FormatRerereResolved(count int) string {
	return fmt.Sprintf("Resolved %d %s using git rerere.", count, Pluralize("conflict", count))
}

func printRerereResolved(ctx *app.Context, count int) {
	ctx.Output.Info("%s", FormatRerereResolved(count))
}

// TakeBestEffortSnapshot records an undo snapshot and logs failures without
// aborting the action.
func TakeBestEffortSnapshot(ctx *app.Context, opts engine.SnapshotOptions) {
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
	Result              engine.RestackResult // Done, Unneeded, or Conflict
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
		if validation.ErrorType == engine.ValidationErrorSystem {
			return fmt.Errorf("validation failed for %s: %s", validation.FailedBranch, validation.ErrorMessage)
		}
		// Else: conflict - continue with conflict workflow
		// Log conflicting files for debugging
		if len(validation.ConflictingFiles) > 0 {
			ctx.Logger.Debug("conflict detected during validation branch=%v files=%v", validation.FailedBranch, validation.ConflictingFiles)
		}
	}

	successBranches, conflictBranches := classifyValidatedBranches(branches, plan, validation)

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
			originalRev, _ = ctx.Engine.Git().GetCurrentRevision(ctx.Context)
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
		if mode == ConflictModeContinue && ctx.Engine.Git().IsRebaseInProgress(ctx.Context) {
			conflictBranch := batchResult.ConflictBranch
			if conflictBranch == "" {
				conflictBranch = "unknown"
			}

			ctx.Logger.Warn("unexpected rebase state after sync restack; aborting branch=%v", conflictBranch)
			if abortErr := ctx.Engine.Git().RebaseAbort(ctx.Context); abortErr != nil {
				return fmt.Errorf("unexpected restack conflict on %s: failed to abort rebase: %w", conflictBranch, abortErr)
			}

			if originalBranch != nil {
				if checkoutErr := ctx.Engine.CheckoutBranch(ctx.Context, *originalBranch); checkoutErr != nil {
					return fmt.Errorf("unexpected restack conflict on %s: aborted rebase but failed to restore branch %s: %w",
						conflictBranch, originalBranch.GetName(), checkoutErr)
				}
			} else if originalRev != "" {
				if detachErr := ctx.Engine.Git().CheckoutDetached(ctx.Context, originalRev); detachErr != nil {
					return fmt.Errorf("unexpected restack conflict on %s: aborted rebase but failed to restore detached HEAD %s: %w",
						conflictBranch, originalRev, detachErr)
				}
			}
		}

		logRestackResults(ctx, batchResult.Results)
	}
	reportPlannedResults(ctx, branches, plannedResults, callback)

	// Report conflicts via callback for sync mode
	if mode == ConflictModeContinue && len(conflictBranches) > 0 {
		currentBranch := ctx.Engine.CurrentBranch()
		currentBranchName := ""
		if currentBranch != nil {
			currentBranchName = currentBranch.GetName()
		}

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
	}

	return nil
}

// classifyValidatedBranches splits the planned branches into ones safe to
// apply now and ones to surface as conflicts, using the validation result as
// the source of truth instead of position-in-specs arithmetic. The earlier
// position-based split was racy: when multiple sibling specs at the same
// depth validate in parallel and one fails, validation.FailedBranch is
// whichever failure arrives first on the results channel — non-deterministic.
// Alphabetically-earlier canceled siblings would then satisfy `pos < failedPos`
// and end up in successBranches without a matching entry in NewSHAs, and the
// engine would error with "missing validated SHA for X". Here we apply a
// branch only if its action proves it's safe: Frozen/Anchor branches don't
// touch a rebase worktree at all, and Validated branches need a confirmed SHA.
func classifyValidatedBranches(branches engine.Branches, plan *engine.RestackPlan, validation *engine.RebaseValidation) (success engine.Branches, conflicts []string) {
	if plan == nil || validation == nil {
		return nil, nil
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
		case engine.RestackPlanApplyFrozen, engine.RestackPlanApplyAnchor:
			builder.Add(branch)
		case engine.RestackPlanApplyValidated:
			if _, hasSHA := validation.NewSHAs[branchName]; hasSHA {
				builder.Add(branch)
			} else if branchName == validation.FailedBranch {
				conflicts = append(conflicts, branchName)
			}
			// Otherwise the branch was canceled — a sibling at the same depth
			// failed first, or validation never reached this depth. Leave it
			// for the next restack pass.
		}
	}
	return builder.Build(), conflicts
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

	// Get new revision if available.
	newRev := ""
	if result.Result == engine.RestackDone {
		if rev, err := branch.GetRevision(); err == nil {
			if len(rev) > 7 {
				newRev = rev[:7]
			} else {
				newRev = rev
			}
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
			style.ColorBranchName(branchName, isCurrent),
			style.ColorBranchName(result.OldParent, false),
			style.ColorBranchName(result.NewParent, false))
	}

	switch result.Result {
	case engine.RestackDone:
		parentName := branch.GetParentOrTrunk()
		isCurrent := branchName == currentBranchName
		ctx.Output.Info("Restacked %s on %s.",
			style.ColorBranchName(branchName, isCurrent),
			style.ColorBranchName(parentName, false))
		if result.RerereResolvedCount > 0 {
			printRerereResolved(ctx, result.RerereResolvedCount)
		}
	case engine.RestackUnneeded:
		switch {
		case result.LockReason.IsLocked():
			ctx.Output.Info("%s locked: %s", style.ColorBranchName(branchName, branchName == currentBranchName), result.LockReason)
		case result.Frozen:
			ctx.Output.Info("%s frozen", style.ColorBranchName(branchName, branchName == currentBranchName))
		case !branch.CanModify():
			if branch.IsLocked() {
				ctx.Output.Info("%s locked: %s", style.ColorBranchName(branchName, branchName == currentBranchName), branch.GetLockReason())
			} else {
				ctx.Output.Info("%s frozen", style.ColorBranchName(branchName, branchName == currentBranchName))
			}
		case branch.IsTrunk():
			ctx.Output.Info("%s up to date", style.ColorBranchName(branchName, false))
		default:
			isCurrent := branchName == currentBranchName
			ctx.Output.Info("%s up to date", style.ColorBranchName(branchName, isCurrent))
		}
	}
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
	if currentBranch := ctx.Engine.CurrentBranch(); currentBranch != nil {
		currentRev, revErr := ctx.Engine.Git().GetCurrentRevision(ctx.Context)
		if revErr != nil {
			return fmt.Errorf("failed to read HEAD before conflict workflow: %w", revErr)
		}
		if detachErr := ctx.Engine.Git().CheckoutDetached(ctx.Context, currentRev); detachErr != nil {
			return fmt.Errorf("failed to detach HEAD before conflict workflow: %w", detachErr)
		}
	}

	// Perform rebase to enter conflict state
	conflictBranch := ctx.Engine.GetBranch(firstConflict)
	batchResult, err := ctx.Engine.RestackBranches(ctx.Context, engine.BranchesOf(conflictBranch))
	if err != nil {
		return fmt.Errorf("failed to enter conflict state for %s: %w", firstConflict, err)
	}

	// Verify we're actually in conflict state
	if !ctx.Engine.Git().IsRebaseInProgress(ctx.Context) {
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

	return fmt.Errorf("restack stopped due to conflict on %s", firstConflict)
}

// validateBranchAncestry performs pre-flight checks on branch ancestry relationships.
// Returns an error if any branch has invalid parent relationships.
// Note: Missing parents are tolerated as buildRebaseSpecs will handle auto-reparenting.
func validateBranchAncestry(ctx *app.Context, branches engine.Branches) error {
	for _, branch := range branches {
		branchName := branch.GetName()

		// Trunk branches don't need validation
		if branch.IsTrunk() {
			continue
		}

		// Verify branch itself exists
		branchRev, err := branch.GetRevision()
		if err != nil {
			return fmt.Errorf("branch %s cannot be resolved: %w", branchName, err)
		}

		// If branch has a parent, validate it only if it exists
		// (missing parents will be handled by auto-reparenting logic)
		parent := branch.GetParent()
		if parent != nil {
			parentName := parent.GetName()
			parentRev, err := parent.GetRevision()
			if err != nil {
				// Parent doesn't exist - this is OK, auto-reparenting will handle it
				ctx.Logger.Debug("parent branch missing, will auto-reparent branch=%v parent=%v", branchName, parentName)
				continue
			}

			// Parent exists - verify they have a common ancestor
			_, err = ctx.Engine.Git().GetMergeBase(parentRev, branchRev)
			if err != nil {
				return fmt.Errorf("branch %s and parent %s have no common ancestor: %w",
					branchName, parentName, err)
			}
		}
	}
	return nil
}

// PluralSuffix returns the appropriate plural suffix for the given word if plural is true, otherwise empty string
func PluralSuffix(word string, plural bool) string {
	if !plural {
		return ""
	}
	pluralized := pluralizeClient.Plural(word)
	if len(pluralized) > len(word) {
		return pluralized[len(word):]
	}
	return "s" // fallback
}

// Pluralize returns the plural form of word if count != 1, otherwise returns the singular form
func Pluralize(word string, count int) string {
	if count == 1 {
		return word
	}
	return pluralizeClient.Plural(word)
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

	msg := style.ColorRed(fmt.Sprintf("Hit conflict restacking %s", branchName))
	out.Info("%s", msg)
	out.Newline()

	// Get unmerged files
	unmergedFiles, err := reader.GetUnmergedFiles(ctx.Context)
	if err == nil && len(unmergedFiles) > 0 {
		out.Info("%s", style.ColorYellow("Conflicted files:"))
		for _, file := range unmergedFiles {
			sections, sectionErr := conflictMarkerSectionsForFile(ctx.RepoRoot, file)
			if sectionErr != nil || len(sections) == 0 {
				out.Info("  %s", style.ColorRed(file))
				continue
			}
			for _, section := range sections {
				out.Info("  %s (lines %d-%d)",
					style.ColorRed(file),
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
		msg := style.ColorYellow(fmt.Sprintf("You are here (resolving %s):", rebaseHeadShort))
		out.Info("%s", msg)
		// Could show log here if needed
		out.Newline()
	}

	parentBranch := "the current base"
	if branch := ctx.Engine.GetBranch(branchName); branch.GetName() != "" {
		parentBranch = branch.GetParentOrTrunk()
	}

	out.Info("%s", style.ColorYellow("To resolve:"))
	out.Info("  1. Open each conflicted file and remove conflict markers:")
	out.Info("     %s  (incoming changes from %s)", style.ColorCyan("<<<<<<< HEAD"), parentBranch)
	out.Info("     %s", style.ColorCyan("======="))
	out.Info("     %s  (changes from %s)", style.ColorCyan(">>>>>>>"), branchName)
	out.Info("  2. Stage resolved files: %s", style.ColorCyan("stackit add <file>"))
	out.Info("  3. Continue the previous Stackit command: %s", style.ColorCyan("stackit continue"))
	out.Info("  4. Abort and restore the pre-command snapshot: %s", style.ColorCyan("stackit abort"))
	out.Info("Tip: %s stages all resolved files before continuing.", style.ColorCyan("stackit continue --all"))

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
