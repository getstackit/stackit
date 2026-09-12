package common

import (
	"fmt"
	"strings"

	"github.com/getstackit/stackit/internal/handlers"
)

// FormatRestackOutcome renders the final line of a restack phase. Holds,
// conflicts, and blocked branches make the outcome incomplete so it can never
// read as "up to date". upToDate counts already-current branches whose rows
// were suppressed.
func FormatRestackOutcome(summary handlers.RestackSummary, upToDate int) string {
	held := len(summary.Held)
	incomplete := RestackIncomplete(summary)
	if summary.Restacked == 0 && !incomplete && !summary.Failed {
		return "✨ Everything is up to date!"
	}
	line := FormatRestackSummaryLine(summary.Restacked, summary.Skipped, len(summary.Blocked), upToDate)
	if held > 0 {
		line = strings.TrimSuffix(fmt.Sprintf("held %d (worktree), %s", held, line), ", ")
	}
	prefix := "✅ Summary: "
	if incomplete {
		prefix = "⚠ Restack incomplete: "
	}
	if summary.Failed {
		prefix = "✗ Restack failed: "
	}
	return WithConflictAdvice(strings.TrimSuffix(prefix+line, ": "), summary.Conflicts)
}

// RestackIncomplete reports whether a restack left any branch behind.
func RestackIncomplete(summary handlers.RestackSummary) bool {
	return summary.Skipped > 0 || len(summary.Conflicts) > 0 || len(summary.Blocked) > 0 || len(summary.Held) > 0
}

// FormatRestackSummaryLine renders the shared "restacked N, skipped M
// (conflict), blocked K, U already current" body, omitting zero counts.
func FormatRestackSummaryLine(restacked, skipped, blocked, upToDate int) string {
	parts := []string{}
	if restacked > 0 {
		parts = append(parts, fmt.Sprintf("restacked %d", restacked))
	}
	if skipped > 0 {
		parts = append(parts, fmt.Sprintf("skipped %d (conflict)", skipped))
	}
	if blocked > 0 {
		parts = append(parts, fmt.Sprintf("blocked %d", blocked))
	}
	if upToDate > 0 {
		parts = append(parts, fmt.Sprintf("%d already current", upToDate))
	}
	return strings.Join(parts, ", ")
}

// WithConflictAdvice appends a resolve-and-continue line for every conflict.
func WithConflictAdvice(summary string, conflicts []string) string {
	for _, branch := range conflicts {
		summary += fmt.Sprintf("\n  Run st restack --branch %s to resolve and continue", branch)
	}
	return summary
}
