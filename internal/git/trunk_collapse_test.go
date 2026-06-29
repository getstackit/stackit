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

func reg(pr int, subject string) git.RecentCommit {
	return git.RecentCommit{PRNumber: pr, Subject: subject, Kind: git.RecentCommitKindRegular}
}

func merge(pr int, subject string, prs ...int) git.RecentCommit {
	return git.RecentCommit{
		PRNumber:       pr,
		Subject:        subject,
		Kind:           git.RecentCommitKindStackMerge,
		StackSize:      len(prs),
		StackPRNumbers: prs,
	}
}

func TestPRTitleNumbers(t *testing.T) {
	t.Parallel()

	// Only stack-merges contribute (consolidation PR + constituents), deduped,
	// first-seen order; regular commits' PRs are never displayed so are skipped.
	commits := []git.RecentCommit{
		merge(100, "consolidate", 1, 2),
		reg(2, "feat: two (#2)"),  // covered constituent — and regular, so ignored
		reg(9, "feat: nine (#9)"), // surviving regular — still ignored
	}
	require.Equal(t, []int{100, 1, 2}, git.PRTitleNumbers(commits))

	require.Empty(t, git.PRTitleNumbers([]git.RecentCommit{reg(9, "feat (#9)")}))
	require.Empty(t, git.PRTitleNumbers(nil))
}

func TestCollapsedMessage(t *testing.T) {
	t.Parallel()

	titles := map[int]string{100: "Consolidated title"}

	// Stack-merge with a known title → title replaces the raw merge subject.
	require.Equal(t, "Consolidated title",
		git.CollapsedMessage(merge(100, "Merge pull request #100", 1, 2), titles))
	// Stack-merge with no title for its PR → falls back to subject.
	require.Equal(t, "Merge pull request #200",
		git.CollapsedMessage(merge(200, "Merge pull request #200", 3), titles))
	// Regular commit → always the subject, even if a title happens to exist.
	require.Equal(t, "feat: hundred (#100)",
		git.CollapsedMessage(reg(100, "feat: hundred (#100)"), titles))
	// Empty subject stays empty.
	require.Equal(t, "", git.CollapsedMessage(reg(0, ""), nil))
}

func TestConstituentPRTitles(t *testing.T) {
	t.Parallel()

	titles := map[int]string{1: "One", 2: "Two", 9: "Nine"}

	// Only the commit's own constituents are selected, not unrelated titles.
	require.Equal(t, map[int]string{1: "One", 2: "Two"},
		git.ConstituentPRTitles(merge(100, "m", 1, 2), titles))
	// Regular commit → nil regardless of titles.
	require.Nil(t, git.ConstituentPRTitles(reg(1, "feat (#1)"), titles))
	// Stack-merge whose constituents have no titles → nil, not empty map.
	require.Nil(t, git.ConstituentPRTitles(merge(100, "m", 7, 8), titles))
	// No titles available → nil.
	require.Nil(t, git.ConstituentPRTitles(merge(100, "m", 1), nil))
}
