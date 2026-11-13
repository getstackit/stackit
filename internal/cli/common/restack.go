package common

import (
	"fmt"
	"strings"

	"github.com/getstackit/stackit/internal/actions/restack"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/tui/style"
)

// RestackRowStatus classifies a restack row for the marker that leads it.
type RestackRowStatus int

const (
	// RestackRowDone is a branch that was restacked or needed nothing.
	RestackRowDone RestackRowStatus = iota
	// RestackRowWarning is a branch that was held back, conflicted, or blocked.
	RestackRowWarning
)

// Mark returns the single-width status marker for streaming output.
func (s RestackRowStatus) Mark() string {
	if s == RestackRowWarning {
		return style.MarkWarning()
	}
	return style.MarkSuccess()
}

// RestackRowStyle controls how a restack row names branches. The zero value
// prints full names and revision hashes, which is what piped output needs:
// scripts pass those names back to stackit.
type RestackRowStyle struct {
	// Name maps a branch name for display (e.g. shortening it); nil keeps it.
	Name func(string) string
	// HideRevision drops the "→ <rev>" suffix from restacked rows.
	HideRevision bool
}

func (s RestackRowStyle) name(branch string) string {
	if s.Name == nil {
		return branch
	}
	return s.Name(branch)
}

// FormatRestackRow renders one restack outcome without its leading marker.
// Every command that restacks prints its rows through here, so a branch reads
// the same whichever command moved it.
func FormatRestackRow(event actions.RestackBranchEvent, rowStyle RestackRowStyle) (string, RestackRowStatus) {
	branch := style.ColorBranchNameIf(rowStyle.name(event.Branch), event.IsCurrent) + FormatPRInfo(event.PRNumber)

	switch event.Result {
	case actions.RestackDone:
		row := "Restacked " + branch
		if event.Parent != "" {
			row += " on " + style.ColorBranchName(rowStyle.name(event.Parent))
		}
		if !rowStyle.HideRevision {
			row += " → " + style.ColorDim(event.NewRevision)
		}
		if event.RerereResolvedCount > 0 {
			row += " " + restack.FormatRerereResolved(event.RerereResolvedCount)
		}
		return row, RestackRowDone
	case actions.RestackConflict:
		return fmt.Sprintf("Skipped %s (conflict)", branch), RestackRowWarning
	case actions.RestackBlocked:
		return fmt.Sprintf("Skipped %s %s", branch, style.ColorDim("(blocked by conflict in stack)")), RestackRowWarning
	}

	switch {
	case event.HeldBy != "":
		// A hold reports the same status as "nothing to do", so say which one
		// happened — the remedy lives in another worktree.
		return fmt.Sprintf("Held %s back: %s", branch, event.HeldBy), RestackRowWarning
	case event.LockReason.IsLocked():
		return fmt.Sprintf("%s %s: %s", branch, ReasonLocked, event.LockReason), RestackRowDone
	case event.Frozen:
		return fmt.Sprintf("%s %s", branch, ReasonFrozen), RestackRowDone
	}
	return branch + " up to date", RestackRowDone
}

// FormatRestackReparent renders the line announcing that a branch moved past
// a merged or deleted parent, or "" when it did not.
func FormatRestackReparent(event actions.RestackBranchEvent, rowStyle RestackRowStyle) string {
	if !event.Reparented {
		return ""
	}
	return fmt.Sprintf("Reparented %s from %s to %s (parent was merged/deleted).",
		style.ColorBranchNameIf(rowStyle.name(event.Branch), event.IsCurrent),
		style.ColorBranchName(rowStyle.name(event.OldParent)),
		style.ColorBranchName(rowStyle.name(event.NewParent)))
}

// FormatRestackLines renders the streaming lines for one restack event: the
// reparent notice when there is one, then the row led by its status marker.
func FormatRestackLines(event actions.RestackBranchEvent) []string {
	var lines []string
	if reparent := FormatRestackReparent(event, RestackRowStyle{}); reparent != "" {
		lines = append(lines, "  "+reparent)
	}
	row, status := FormatRestackRow(event, RestackRowStyle{})
	return append(lines, fmt.Sprintf("  %s %s", status.Mark(), row))
}

// FormatRestackSummary renders "held H (worktree), restacked N, skipped M
// (conflict), blocked K, P already current". upToDate is the count of rows the
// caller suppressed. Holds lead: the remedy lives in another worktree the user
// is not looking at, so it must not be buried behind routine counts. Returns ""
// when there is nothing to summarize.
func FormatRestackSummary(summary actions.RestackSummary, upToDate int) string {
	var parts []string
	if n := len(summary.Held); n > 0 {
		parts = append(parts, fmt.Sprintf("held %d (worktree)", n))
	}
	if summary.Restacked > 0 {
		parts = append(parts, fmt.Sprintf("restacked %d", summary.Restacked))
	}
	if n := len(summary.Conflicts); n > 0 {
		parts = append(parts, fmt.Sprintf("skipped %d (conflict)", n))
	}
	if n := len(summary.Blocked); n > 0 {
		parts = append(parts, fmt.Sprintf("blocked %d", n))
	}
	if upToDate > 0 {
		parts = append(parts, fmt.Sprintf("%d already current", upToDate))
	}
	return strings.Join(parts, ", ")
}

// RestackChanged reports whether a restack moved, conflicted on, blocked, or
// held back any branch — anything other than "everything was already current".
func RestackChanged(summary actions.RestackSummary) bool {
	return summary.Restacked > 0 || summary.Incomplete()
}

// FormatRestackOutcome renders the final restack line plus a resolve hint per
// conflict, each hint on its own line behind indent. Holds, conflicts, and
// blocked branches make the outcome incomplete so it never reads as success.
func FormatRestackOutcome(summary actions.RestackSummary, upToDate int, indent string) string {
	if !RestackChanged(summary) && !summary.Failed {
		return "✨ Everything is up to date!"
	}
	prefix := "✅ Summary: "
	switch {
	case summary.Failed:
		// A failure always reports, even when it stopped before any work.
		prefix = "✗ Restack failed: "
	case summary.Incomplete():
		prefix = "⚠ Restack incomplete: "
	}
	line := strings.TrimSuffix(prefix+FormatRestackSummary(summary, upToDate), ": ")
	return WithResolveHints(line, summary.Conflicts, indent)
}

// WithResolveHints appends a resolve-and-continue line for every conflict.
func WithResolveHints(line string, conflicts []string, indent string) string {
	lines := make([]string, 0, 1+len(conflicts))
	lines = append(lines, line)
	for _, branch := range conflicts {
		lines = append(lines, indent+FormatRestackResolveHint(branch))
	}
	return strings.Join(lines, "\n")
}

// FormatRestackResolveHint renders the follow-up command for a conflicted branch.
func FormatRestackResolveHint(branch string) string {
	return fmt.Sprintf("Run %s to resolve and continue", style.ColorCyan("st restack --branch "+branch))
}
