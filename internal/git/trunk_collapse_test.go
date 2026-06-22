package git_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
)

func TestCollapseStackMerges(t *testing.T) {
	t.Parallel()

	reg := func(pr int) git.RecentCommit {
		return git.RecentCommit{PRNumber: pr, Kind: git.RecentCommitKindRegular}
	}
	merge := func(pr int, prs ...int) git.RecentCommit {
		return git.RecentCommit{
			PRNumber:       pr,
			Kind:           git.RecentCommitKindStackMerge,
			StackSize:      len(prs),
			StackPRNumbers: prs,
		}
	}
	prNumbers := func(commits []git.RecentCommit) []int {
		out := make([]int, len(commits))
		for i, c := range commits {
			out[i] = c.PRNumber
		}
		return out
	}

	tests := []struct {
		name string
		in   []git.RecentCommit
		want []int // expected PRNumbers, in order
	}{
		{
			name: "no stack merges passes through",
			in:   []git.RecentCommit{reg(3), reg(2), reg(1)},
			want: []int{3, 2, 1},
		},
		{
			name: "stack merge drops all its constituents",
			in:   []git.RecentCommit{merge(100, 1, 2, 3), reg(3), reg(2), reg(1)},
			want: []int{100},
		},
		{
			name: "partial coverage keeps uncovered commits",
			in:   []git.RecentCommit{merge(100, 1, 2), reg(3), reg(2), reg(1)},
			want: []int{100, 3},
		},
		{
			name: "order preserved among survivors",
			in:   []git.RecentCommit{reg(9), merge(100, 1), reg(8), reg(1)},
			want: []int{9, 100, 8},
		},
		{
			name: "empty input",
			in:   nil,
			want: []int{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := git.CollapseStackMerges(tt.in)
			require.Equal(t, tt.want, prNumbers(got))
		})
	}
}
