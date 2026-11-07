package actions

import (
	"sync"

	"github.com/getstackit/stackit/internal/actions/restack"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
)

// RestackResult represents the outcome of a restack operation for a single branch
type RestackResult string

const (
	// RestackDone indicates the branch was successfully restacked
	RestackDone RestackResult = "done"
	// RestackUnneeded indicates the branch didn't need restacking
	RestackUnneeded RestackResult = "unneeded"
	// RestackConflict indicates the branch had a conflict
	RestackConflict RestackResult = "conflict"
	// RestackBlocked indicates the branch was left untouched because another
	// branch in its stack conflicted
	RestackBlocked RestackResult = "blocked"
)

// RestackHandler receives restack progress. Every command that restacks
// (restack, sync, get, submit) reports through it via RunRestack, so adapters
// render one event shape regardless of which command produced it.
type RestackHandler interface {
	// OnRestackStart is called at the beginning of restack with branch count
	OnRestackStart(branchCount int)

	// OnRestackBranch is called for each branch during restack.
	OnRestackBranch(event RestackBranchEvent)

	// OnRestackComplete is called when restack finishes. blocked lists
	// branches left untouched because their stack contained a conflict.
	OnRestackComplete(summary RestackSummary)
}

// RestackActivityHandler optionally receives live rebase validation activity.
// Result-only handlers (including JSON) need not implement it.
//
// OnRestackActivity is called concurrently, one goroutine per branch being
// validated, and outside the ordering of the other RestackHandler methods.
// Implementations must be safe for concurrent use and return quickly without
// blocking; they should not take a lock that result events also hold.
type RestackActivityHandler interface {
	OnRestackActivity(engine.RebaseProgress)
}

// RestackActivity returns the handler's activity callback, or nil when it does
// not implement RestackActivityHandler, so validation skips reporting.
func RestackActivity(handler RestackHandler) engine.RebaseProgressFunc {
	if h, ok := handler.(RestackActivityHandler); ok {
		return h.OnRestackActivity
	}
	return nil
}

// RestackBranchFunc adapts a function to RestackHandler for callers that
// bracket the restack in a phase of their own (sync, submit) and only want the
// per-branch events.
type RestackBranchFunc func(RestackBranchEvent)

// OnRestackStart implements RestackHandler.
func (f RestackBranchFunc) OnRestackStart(int) {}

// OnRestackBranch implements RestackHandler.
func (f RestackBranchFunc) OnRestackBranch(event RestackBranchEvent) { f(event) }

// OnRestackComplete implements RestackHandler.
func (f RestackBranchFunc) OnRestackComplete(RestackSummary) {}

// RestackConflictPrompter asks whether to resolve restack conflicts now. It is
// consulted only under RestackConflictPrompt.
type RestackConflictPrompter interface {
	IsInteractive() bool
	PromptResolveConflicts(conflictBranches []string) (bool, error)
}

// RestackBranchEvent describes the outcome of restacking one branch.
// Keeping these facts together makes the shared presentation contract safe to
// extend without relying on positional arguments.
type RestackBranchEvent struct {
	Branch              string
	Result              RestackResult
	NewRevision         string
	PRNumber            *git.PRNumber
	LockReason          engine.LockReason
	Frozen              bool
	HeldBy              string // why a worktree held this branch back (empty if it was not held)
	IsCurrent           bool
	Parent              string
	Reparented          bool
	OldParent           string
	NewParent           string
	RerereResolvedCount int
	StackRoot           string
}

// IsPlainUpToDate reports whether the event is a no-op with nothing worth
// showing: the branch was already current, and not locked, frozen, held back
// by a worktree, or reparented. A held branch reports the same RestackUnneeded
// status as one that needed no work, so it must be excluded here or "I
// protected your work" is silently counted as "already current".
func (e RestackBranchEvent) IsPlainUpToDate() bool {
	return e.Result == RestackUnneeded &&
		!e.LockReason.IsLocked() &&
		!e.Frozen &&
		e.HeldBy == "" &&
		!e.Reparented
}

// RestackSummary contains aggregate outcomes from a restack operation.
type RestackSummary struct {
	Restacked int
	Conflicts []string // branches skipped because they conflicted
	Blocked   []string
	// Held lists every branch a worktree held back, whether restack pruned it
	// while planning or the engine held it mid-run. A held branch is reported
	// as unneeded per branch, so this is the only place an adapter can tell
	// "protected your work" from "nothing to do".
	Held []RestackHeldInfo
}

// RestackHeldInfo is a branch a worktree held back, with the reason.
type RestackHeldInfo struct {
	Branch string `json:"branch"`
	Reason string `json:"reason"`
}

// Incomplete reports whether the restack left any branch behind.
func (s RestackSummary) Incomplete() bool {
	return len(s.Conflicts) > 0 || len(s.Blocked) > 0 || len(s.Held) > 0
}

// NullRestackHandler is a no-op handler for testing or when output is not needed
type NullRestackHandler struct{}

// OnRestackStart implements RestackHandler.
func (h *NullRestackHandler) OnRestackStart(_ int) {}

// OnRestackBranch implements RestackHandler.
func (h *NullRestackHandler) OnRestackBranch(RestackBranchEvent) {}

// OnRestackComplete implements RestackHandler.
func (h *NullRestackHandler) OnRestackComplete(RestackSummary) {}

// restackTally converts engine progress into RestackBranchEvents and keeps the
// running summary. It is the single place that mapping happens, so every
// command reports the same fields. Safe for the parallel path's concurrent
// workers.
type restackTally struct {
	mu       sync.Mutex
	eng      engine.BranchReader
	handler  RestackHandler
	prNumber func(branch string) *git.PRNumber
	summary  RestackSummary
}

func newRestackTally(eng engine.BranchReader, handler RestackHandler, prNumber func(string) *git.PRNumber) *restackTally {
	if prNumber == nil {
		prNumber = func(branch string) *git.PRNumber { return PRNumberForBranch(eng, branch) }
	}
	return &restackTally{eng: eng, handler: handler, prNumber: prNumber}
}

func (t *restackTally) record(p restack.Progress) {
	t.mu.Lock()
	defer t.mu.Unlock()

	event := RestackBranchEvent{
		Branch:              p.Branch,
		NewRevision:         p.NewRev,
		PRNumber:            t.prNumber(p.Branch),
		LockReason:          p.LockReason,
		Frozen:              p.Frozen,
		HeldBy:              p.HeldBy,
		IsCurrent:           p.IsCurrent,
		Parent:              t.parentName(p),
		Reparented:          p.Reparented,
		OldParent:           p.OldParent,
		NewParent:           p.NewParent,
		RerereResolvedCount: p.RerereResolvedCount,
		StackRoot:           p.StackRoot,
	}

	switch p.Result {
	case engine.RestackDone:
		t.summary.Restacked++
		event.Result = RestackDone
	case engine.RestackUnneeded:
		event.Result = RestackUnneeded
		if p.HeldBy != "" {
			t.summary.Held = append(t.summary.Held, RestackHeldInfo{Branch: p.Branch, Reason: p.HeldBy})
		}
	case engine.RestackConflict:
		t.summary.Conflicts = append(t.summary.Conflicts, p.Branch)
		event.Result = RestackConflict
	case engine.RestackBlocked:
		t.summary.Blocked = append(t.summary.Blocked, p.Branch)
		event.Result = RestackBlocked
	}

	t.handler.OnRestackBranch(event)
}

// parentName is the parent the branch now sits on. A reparented branch reports
// its new parent directly: the engine's view can still name the landed one.
func (t *restackTally) parentName(p restack.Progress) string {
	if p.Reparented && p.NewParent != "" {
		return p.NewParent
	}
	br := t.eng.GetBranch(p.Branch)
	if br.Name() == "" {
		return ""
	}
	return br.ParentOrTrunk()
}

func (t *restackTally) result() RestackSummary {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.summary
}
