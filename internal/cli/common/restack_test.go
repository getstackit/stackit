package common

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
)

func TestFormatRestackRow(t *testing.T) {
	t.Parallel()

	pr := git.PRNumber(7)
	tests := []struct {
		name   string
		event  actions.RestackBranchEvent
		row    string
		status RestackRowStatus
	}{
		{
			name:   "restacked with parent and rerere",
			event:  actions.RestackBranchEvent{Branch: "a", Result: actions.RestackDone, PRNumber: &pr, Parent: "main", NewRevision: "abc1234", RerereResolvedCount: 1},
			row:    "Restacked a (PR #7) on main → abc1234 Resolved 1 conflict using git rerere.",
			status: RestackRowDone,
		},
		{
			name:   "up to date",
			event:  actions.RestackBranchEvent{Branch: "a", Result: actions.RestackUnneeded},
			row:    "a up to date",
			status: RestackRowDone,
		},
		{
			// A hold reports RestackUnneeded too; it must never read as up to date.
			name:   "held by a worktree",
			event:  actions.RestackBranchEvent{Branch: "a", Result: actions.RestackUnneeded, HeldBy: "worktree /wt has uncommitted changes"},
			row:    "Held a back: worktree /wt has uncommitted changes",
			status: RestackRowWarning,
		},
		{
			name:   "locked",
			event:  actions.RestackBranchEvent{Branch: "a", Result: actions.RestackUnneeded, LockReason: engine.LockReason("release")},
			row:    "a is locked: release",
			status: RestackRowDone,
		},
		{
			name:   "frozen",
			event:  actions.RestackBranchEvent{Branch: "a", Result: actions.RestackUnneeded, Frozen: true},
			row:    "a is frozen",
			status: RestackRowDone,
		},
		{
			name:   "conflict",
			event:  actions.RestackBranchEvent{Branch: "a", Result: actions.RestackConflict},
			row:    "Skipped a (conflict)",
			status: RestackRowWarning,
		},
		{
			name:   "blocked",
			event:  actions.RestackBranchEvent{Branch: "a", Result: actions.RestackBlocked},
			row:    "Skipped a (blocked by conflict in stack)",
			status: RestackRowWarning,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			row, status := FormatRestackRow(tt.event, RestackRowStyle{})
			require.Equal(t, tt.row, ansi.Strip(row))
			require.Equal(t, tt.status, status)
		})
	}
}

func TestFormatRestackSummary(t *testing.T) {
	t.Parallel()

	require.Empty(t, FormatRestackSummary(actions.RestackSummary{}, 0))
	require.Equal(t,
		"restacked 2, skipped 1 (conflict), blocked 1, 3 already current",
		FormatRestackSummary(actions.RestackSummary{
			Restacked: 2,
			Conflicts: []string{"c"},
			Blocked:   []string{"d"},
		}, 3),
	)
}
