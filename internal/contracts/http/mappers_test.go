package httpcontract

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
)

func TestMapTrunkCommits(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	commits := []git.RecentCommit{
		{
			SHA:            "1234567890abcdef",
			Subject:        "Consolidate auth stack",
			Author:         "alice",
			Date:           now,
			PRNumber:       123,
			Kind:           git.RecentCommitKindStackMerge,
			StackSize:      2,
			StackPRNumbers: []int{45, 46},
			StackScope:     "PROJ-1",
		},
		{
			SHA:     "abcdef1234567890",
			Subject: "Fix typo",
			Author:  "bob",
			Date:    now,
			Kind:    git.RecentCommitKindRegular,
		},
	}

	got := MapTrunkCommits(commits, nil)
	require.Len(t, got, 2)

	require.Equal(t, "1234567", got[0].SHA)
	require.Equal(t, "Consolidate auth stack", got[0].Message)
	require.Equal(t, "alice", got[0].Author)
	require.Equal(t, now.Format(time.RFC3339), got[0].Date)
	require.Equal(t, 123, got[0].PRNumber)
	require.Equal(t, TrunkCommitKindStackMerge, got[0].Kind)
	require.Equal(t, 2, got[0].StackSize)
	require.Equal(t, []int{45, 46}, got[0].StackPRs)
	require.Equal(t, "PROJ-1", got[0].StackScope)

	require.Equal(t, "abcdef1", got[1].SHA)
	require.Equal(t, TrunkCommitKindRegular, got[1].Kind)
	require.Equal(t, 0, got[1].PRNumber)
	require.Empty(t, got[1].StackPRs)
}

func TestMapTrunkCommits_FiltersCoveredPRs(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	commits := []git.RecentCommit{
		{
			SHA:            "aaaa000000000000",
			Subject:        "Consolidate stack (#99)",
			Author:         "alice",
			Date:           now,
			PRNumber:       99,
			Kind:           git.RecentCommitKindStackMerge,
			StackSize:      3,
			StackPRNumbers: []int{45, 46, 47},
		},
		{
			SHA:      "bbbb000000000000",
			Subject:  "feat: add auth (#45)",
			Author:   "alice",
			Date:     now,
			PRNumber: 45,
			Kind:     git.RecentCommitKindRegular,
		},
		{
			SHA:      "cccc000000000000",
			Subject:  "feat: add middleware (#46)",
			Author:   "alice",
			Date:     now,
			PRNumber: 46,
			Kind:     git.RecentCommitKindRegular,
		},
		{
			SHA:      "dddd000000000000",
			Subject:  "unrelated fix (#50)",
			Author:   "bob",
			Date:     now,
			PRNumber: 50,
			Kind:     git.RecentCommitKindRegular,
		},
	}

	got := MapTrunkCommits(commits, nil)
	require.Len(t, got, 2, "commits covered by stack-merge should be filtered out")

	require.Equal(t, "aaaa000", got[0].SHA)
	require.Equal(t, TrunkCommitKindStackMerge, got[0].Kind)

	require.Equal(t, "dddd000", got[1].SHA)
	require.Equal(t, "unrelated fix (#50)", got[1].Message)
}

func TestMapTrunkCommits_DefaultKindFallback(t *testing.T) {
	t.Parallel()

	commits := []git.RecentCommit{
		{
			SHA:       "1234567890abcdef",
			Subject:   "Consolidate stack",
			Author:    "alice",
			Date:      time.Now().UTC(),
			StackSize: 3,
		},
	}

	got := MapTrunkCommits(commits, nil)
	require.Len(t, got, 1)
	require.Equal(t, TrunkCommitKindStackMerge, got[0].Kind)
}

func TestMapTrunkCommits_WithPRTitles(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	commits := []git.RecentCommit{
		{
			SHA:            "1234567890abcdef",
			Subject:        "Merge pull request #99 from org/stack-merge-123",
			Author:         "alice",
			Date:           now,
			PRNumber:       99,
			Kind:           git.RecentCommitKindStackMerge,
			StackSize:      3,
			StackPRNumbers: []int{45, 46, 47},
		},
	}

	titles := map[int]string{
		99: "Consolidate auth stack",
		45: "feat: add auth",
		46: "feat: add middleware",
		// 47 intentionally missing to test partial titles
	}

	got := MapTrunkCommits(commits, titles)
	require.Len(t, got, 1)
	// Consolidation PR title replaces the raw merge subject
	require.Equal(t, "Consolidate auth stack", got[0].Message)
	require.Equal(t, map[int]string{
		45: "feat: add auth",
		46: "feat: add middleware",
	}, got[0].StackPRTitles)
}

func TestMapTrunkCommits_NilTitlesNoStackPRTitles(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	commits := []git.RecentCommit{
		{
			SHA:            "1234567890abcdef",
			Subject:        "Consolidate auth stack",
			Author:         "alice",
			Date:           now,
			Kind:           git.RecentCommitKindStackMerge,
			StackSize:      2,
			StackPRNumbers: []int{45, 46},
		},
	}

	got := MapTrunkCommits(commits, nil)
	require.Len(t, got, 1)
	require.Nil(t, got[0].StackPRTitles)
}
