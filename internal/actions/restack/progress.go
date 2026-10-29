package restack

import (
	"fmt"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/output"
)

// Progress describes the outcome of restacking a single branch.
type Progress struct {
	Branch              string               // the branch being processed
	Result              engine.RestackResult // Done, Unneeded, Conflict, or Blocked
	NewRev              string               // new revision if restacked (empty otherwise)
	RerereResolvedCount int                  // number of rebase continuations handled by git rerere
	Conflict            bool                 // true if this is a conflict
	LockReason          engine.LockReason    // why the branch is locked (empty if not locked)
	Frozen              bool                 // true if the branch is frozen
	HeldBy              string               // why a worktree held this branch back (empty if it was not held)
	IsCurrent           bool                 // true if this is the current branch
	Reparented          bool                 // true if the branch was reparented
	OldParent           string               // the old parent name if reparented
	NewParent           string               // the new parent name if reparented
	StackRoot           string               // independent stack root (set by multi-stack callers; empty for single-stack)

	// branch is the processed branch, for TextReporter's fallback lookups.
	// Conflict and blocked reports leave it unset.
	branch engine.Branch
}

// ProgressFunc receives one Progress per processed branch.
type ProgressFunc func(Progress)

// ConflictMode controls how Execute responds to a conflicting branch.
type ConflictMode int

const (
	// ConflictEnterWorkflow stops at the first conflict, writes rebase state to
	// the working tree, and expects the user to resolve it with stackit
	// continue. This is the standalone CLI default.
	ConflictEnterWorkflow ConflictMode = iota

	// ConflictContinue restacks the stacks that apply cleanly and reports
	// conflicts without writing rebase state. Use it in sync, parallel workers
	// (rebase state would be torn down with the worktree), and
	// --continue-on-conflict flows.
	ConflictContinue
)

// Printer is the terminal surface TextReporter and ConflictReporter write to.
// output.Output satisfies it.
type Printer interface {
	Info(format string, args ...any)
	Warn(format string, args ...any)
	Newline()
}

// FormatRerereResolved renders the standard message describing how many
// rebase conflicts git rerere auto-resolved during an operation.
func FormatRerereResolved(count int) string {
	noun := "conflicts"
	if count == 1 {
		noun = "conflict"
	}
	return fmt.Sprintf("Resolved %d %s using git rerere.", count, noun)
}

// TextReporter prints one line per restacked branch, for commands that have
// no progress UI of their own. Conflicts and blocked branches print nothing:
// the conflict workflow, or the caller's summary, reports those.
func TextReporter(out Printer) ProgressFunc {
	return func(p Progress) {
		name := p.Branch
		if p.Reparented {
			out.Info("Reparented %s from %s to %s (parent was merged/deleted).",
				output.Branch(name, p.IsCurrent),
				output.BranchName(p.OldParent),
				output.BranchName(p.NewParent))
		}

		switch p.Result {
		case engine.RestackDone:
			parentName := p.branch.ParentOrTrunk()
			if p.Reparented && p.NewParent != "" {
				parentName = p.NewParent
			}
			out.Info("Restacked %s on %s.", output.Branch(name, p.IsCurrent), output.BranchName(parentName))
			if p.RerereResolvedCount > 0 {
				out.Info("%s", FormatRerereResolved(p.RerereResolvedCount))
			}
		case engine.RestackUnneeded:
			switch {
			case p.HeldBy != "":
				// A hold and "nothing to do" are the same status, so say which one
				// happened — the remedy lives in another worktree.
				out.Warn("Held %s back: %s", output.Branch(name, p.IsCurrent), p.HeldBy)
			case p.LockReason.IsLocked():
				out.Info("%s locked: %s", output.Branch(name, p.IsCurrent), p.LockReason)
			case p.Frozen:
				out.Info("%s frozen", output.Branch(name, p.IsCurrent))
			case !p.branch.CanModify():
				if p.branch.IsLocked() {
					out.Info("%s locked: %s", output.Branch(name, p.IsCurrent), p.branch.LockReason())
				} else {
					out.Info("%s frozen", output.Branch(name, p.IsCurrent))
				}
			case p.branch.IsTrunk():
				out.Info("%s up to date", output.BranchName(name))
			default:
				out.Info("%s up to date", output.Branch(name, p.IsCurrent))
			}
		}
	}
}
