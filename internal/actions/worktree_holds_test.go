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

	require.True(t, report.blocks("stack-a-child", "stack-a"))
	require.True(t, report.blocks("branch-b", "stack-b"))
	require.False(t, report.blocks("branch-c", "stack-b"))
	require.True(t, report.blocksStack("stack-a"))
	require.False(t, report.blocksStack("stack-b"))
}
