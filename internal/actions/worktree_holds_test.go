package actions

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRestackWorktreeHoldWarning(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		hold RestackWorktreeHold
		want string
	}{
		{
			name: "stack with uncommitted changes",
			hold: RestackWorktreeHold{
				Scope:        RestackWorktreeHoldStack,
				StackRoot:    "feature",
				WorktreePath: "/tmp/feature",
				Reason:       "uncommitted changes",
			},
			want: "Skipping stack rooted at feature (worktree /tmp/feature has uncommitted changes)",
		},
		{
			name: "branch with a rebase in progress",
			hold: RestackWorktreeHold{
				Scope:        RestackWorktreeHoldBranch,
				Branch:       "feature-child",
				WorktreePath: "/tmp/feature-child",
				Reason:       "rebase in progress",
			},
			want: "Holding feature-child (worktree /tmp/feature-child has a rebase in progress)",
		},
		{
			name: "branch with an untracked collision",
			hold: RestackWorktreeHold{
				Scope:        RestackWorktreeHoldBranch,
				Branch:       "feature",
				WorktreePath: "/tmp/feature",
				Reason:       "an untracked file would be overwritten",
			},
			want: "Skipping feature (an untracked file in worktree /tmp/feature would be overwritten by the incoming commit; move or commit it, then restack again)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, tt.hold.warning())
		})
	}
}

func TestRestackWorktreeHoldHeldBy(t *testing.T) {
	t.Parallel()

	branch := RestackWorktreeHold{Scope: RestackWorktreeHoldBranch, Branch: "y", WorktreePath: "/tmp/y", Reason: "uncommitted changes"}
	require.Equal(t, "worktree /tmp/y has uncommitted changes", branch.heldBy())

	stack := RestackWorktreeHold{Scope: RestackWorktreeHoldStack, StackRoot: "x", WorktreePath: "/tmp/x", Reason: "rebase in progress"}
	require.Equal(t, "worktree /tmp/x has a rebase in progress (holds the stack rooted at x)", stack.heldBy())
}

func TestRestackWorktreeHoldReportBlocks(t *testing.T) {
	t.Parallel()

	report := RestackWorktreeHoldReport{Holds: []RestackWorktreeHold{
		{
			Scope:     RestackWorktreeHoldStack,
			StackRoot: "stack-a",
		},
		{
			Scope:  RestackWorktreeHoldBranch,
			Branch: "branch-b",
		},
	}}

	hold, ok := report.holdFor("stack-a-child", "stack-a")
	require.True(t, ok)
	require.Equal(t, RestackWorktreeHoldStack, hold.Scope)
	hold, ok = report.holdFor("branch-b", "stack-b")
	require.True(t, ok)
	require.Equal(t, "branch-b", hold.Branch)
	_, ok = report.holdFor("branch-c", "stack-b")
	require.False(t, ok)
	require.True(t, report.blocksStack("stack-a"))
	require.False(t, report.blocksStack("stack-b"))
}
