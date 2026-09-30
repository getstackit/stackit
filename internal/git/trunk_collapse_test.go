package git_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
)

func TestRecentCommitsCollapse(t *testing.T) {
	t.Parallel()

	reg := func(pr git.PRNumber) git.RecentCommit {
		return git.RecentCommit{PRNumber: pr, Kind: git.RecentCommitKindRegular}
	}
	merge := func(pr git.PRNumber, prs ...git.PRNumber) git.RecentCommit {
		return git.RecentCommit{
			PRNumber:       pr,
			Kind:           git.RecentCommitKindStackMerge,
			StackSize:      len(prs),
			StackPRNumbers: prs,
		}
	}
	prNumbers := func(commits []git.RecentCommit) []git.PRNumber {
		out := make([]git.PRNumber, len(commits))
		for i, c := range commits {
			out[i] = c.PRNumber
		}
		return out
	}

	tests := []struct {
		name string
		in   []git.RecentCommit
		want []git.PRNumber // expected PRNumbers, in order
	}{
		{
			name: "no stack merges passes through",
			in:   []git.RecentCommit{reg(3), reg(2), reg(1)},
			want: []git.PRNumber{3, 2, 1},
		},
		{
			name: "stack merge drops all its constituents",
			in:   []git.RecentCommit{merge(100, 1, 2, 3), reg(3), reg(2), reg(1)},
			want: []git.PRNumber{100},
		},
		{
			name: "partial coverage keeps uncovered commits",
			in:   []git.RecentCommit{merge(100, 1, 2), reg(3), reg(2), reg(1)},
			want: []git.PRNumber{100, 3},
		},
		{
			name: "order preserved among survivors",
			in:   []git.RecentCommit{reg(9), merge(100, 1), reg(8), reg(1)},
			want: []git.PRNumber{9, 100, 8},
		},
		{
			name: "empty input",
			in:   nil,
			want: []git.PRNumber{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := git.RecentCommits(tt.in).Collapse()
			require.Equal(t, tt.want, prNumbers(got))
		})
	}
}

func reg(pr git.PRNumber, subject string) git.RecentCommit {
	return git.RecentCommit{PRNumber: pr, Subject: subject, Kind: git.RecentCommitKindRegular}
}

func merge(pr git.PRNumber, subject string, prs ...git.PRNumber) git.RecentCommit {
	return git.RecentCommit{
		PRNumber:       pr,
		Subject:        subject,
		Kind:           git.RecentCommitKindStackMerge,
		StackSize:      len(prs),
		StackPRNumbers: prs,
	}
}

func TestRecentCommitsPRTitleNumbers(t *testing.T) {
	t.Parallel()

	// Only stack-merges contribute (consolidation PR + constituents), deduped,
	// first-seen order; regular commits' PRs are never displayed so are skipped.
	commits := []git.RecentCommit{
		merge(100, "consolidate", 1, 2),
		reg(2, "feat: two (#2)"),  // covered constituent — and regular, so ignored
		reg(9, "feat: nine (#9)"), // surviving regular — still ignored
	}
	require.Equal(t, []git.PRNumber{100, 1, 2}, git.RecentCommits(commits).PRTitleNumbers())

	require.Empty(t, git.RecentCommits{reg(9, "feat (#9)")}.PRTitleNumbers())
	require.Empty(t, git.RecentCommits(nil).PRTitleNumbers())
}

func TestRecentCommitDisplayMessage(t *testing.T) {
	t.Parallel()

	titles := map[git.PRNumber]string{100: "Consolidated title"}

	// Stack-merge with a known title → title replaces the raw merge subject.
	require.Equal(t, "Consolidated title",
		merge(100, "Merge pull request #100", 1, 2).DisplayMessage(titles))
	// Stack-merge with no title for its PR → falls back to subject.
	require.Equal(t, "Merge pull request #200",
		merge(200, "Merge pull request #200", 3).DisplayMessage(titles))
	// Regular commit → always the subject, even if a title happens to exist.
	require.Equal(t, "feat: hundred (#100)",
		reg(100, "feat: hundred (#100)").DisplayMessage(titles))
	// Empty subject stays empty.
	require.Equal(t, "", reg(0, "").DisplayMessage(nil))
}

func TestRecentCommitConstituentPRTitles(t *testing.T) {
	t.Parallel()

	titles := map[git.PRNumber]string{1: "One", 2: "Two", 9: "Nine"}

	// Only the commit's own constituents are selected, not unrelated titles.
	require.Equal(t, map[git.PRNumber]string{1: "One", 2: "Two"},
		merge(100, "m", 1, 2).ConstituentPRTitles(titles))
	// Regular commit → nil regardless of titles.
	require.Nil(t, reg(1, "feat (#1)").ConstituentPRTitles(titles))
	// Stack-merge whose constituents have no titles → nil, not empty map.
	require.Nil(t, merge(100, "m", 7, 8).ConstituentPRTitles(titles))
	// No titles available → nil.
	require.Nil(t, merge(100, "m", 1).ConstituentPRTitles(nil))
}
