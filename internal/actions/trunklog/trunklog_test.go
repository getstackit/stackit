package trunklog_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions/trunklog"
	"github.com/getstackit/stackit/internal/git"
)

type fakeSource struct {
	recent      []git.RecentCommit
	ranged      []git.RecentCommit
	err         error
	gotCount    int
	gotFrom     string
	gotTo       string
	calledRange bool
}

func (f *fakeSource) GetRecentTrunkCommits(count int) ([]git.RecentCommit, error) {
	f.gotCount = count
	return f.recent, f.err
}

func (f *fakeSource) GetTrunkCommitsInRange(from, to string) ([]git.RecentCommit, error) {
	f.calledRange = true
	f.gotFrom, f.gotTo = from, to
	return f.ranged, f.err
}

type fakeTitles struct {
	titles map[int]string
	gotPRs []int
	err    error
}

func (f *fakeTitles) GetOwnerRepo() (string, string) { return "owner", "repo" }

func (f *fakeTitles) BatchGetPRTitles(_ context.Context, _, _ string, prNumbers []int) (map[int]string, error) {
	f.gotPRs = prNumbers
	return f.titles, f.err
}

func reg(sha string, pr int, subject string) git.RecentCommit {
	return git.RecentCommit{SHA: sha, Subject: subject, PRNumber: pr, Kind: git.RecentCommitKindRegular}
}

func merge(sha string, pr int, scope string, prs ...int) git.RecentCommit {
	return git.RecentCommit{
		SHA: sha, Subject: "Merge pull request #x", PRNumber: pr,
		Kind: git.RecentCommitKindStackMerge, StackSize: len(prs),
		StackPRNumbers: prs, StackScope: scope,
	}
}

func TestGather_DefaultCountPath(t *testing.T) {
	t.Parallel()
	src := &fakeSource{recent: []git.RecentCommit{reg("a", 5, "feat: a (#5)")}}

	res, err := trunklog.Gather(context.Background(), src, nil, trunklog.Request{Count: 10})
	require.NoError(t, err)
	require.False(t, src.calledRange)
	require.Equal(t, 10, src.gotCount)
	require.Len(t, res.Commits, 1)
	require.Equal(t, "feat: a (#5)", res.Commits[0].Message)
}

func TestGather_RangePath(t *testing.T) {
	t.Parallel()
	src := &fakeSource{ranged: []git.RecentCommit{reg("a", 5, "feat: a")}}

	res, err := trunklog.Gather(context.Background(), src, nil, trunklog.Request{From: "v1", To: "main"})
	require.NoError(t, err)
	require.True(t, src.calledRange)
	require.Equal(t, "v1", src.gotFrom)
	require.Equal(t, "main", src.gotTo)
	require.Equal(t, "v1", res.From)
	require.Equal(t, "main", res.To)
	require.Len(t, res.Commits, 1)
}

func TestGather_CollapsesAndEnriches(t *testing.T) {
	t.Parallel()
	src := &fakeSource{recent: []git.RecentCommit{
		merge("m", 100, "FEAT-1", 1, 2),
		reg("c2", 2, "feat: two (#2)"),
		reg("c1", 1, "feat: one (#1)"),
	}}
	titles := &fakeTitles{titles: map[int]string{100: "Consolidate", 1: "One", 2: "Two"}}

	res, err := trunklog.Gather(context.Background(), src, titles, trunklog.Request{Count: 5})
	require.NoError(t, err)

	// Constituents collapse into the merge.
	require.Len(t, res.Commits, 1)
	got := res.Commits[0]
	require.Equal(t, "Consolidate", got.Message) // merge subject replaced by PR title
	require.Equal(t, "FEAT-1", got.StackScope)
	require.Equal(t, []int{1, 2}, got.StackPRs)
	require.Equal(t, map[int]string{1: "One", 2: "Two"}, got.StackPRTitles)

	// PR numbers requested = consolidation PR + constituents, deduped.
	require.ElementsMatch(t, []int{100, 1, 2}, titles.gotPRs)
}

func TestGather_OfflineNoTitles(t *testing.T) {
	t.Parallel()
	src := &fakeSource{recent: []git.RecentCommit{merge("m", 100, "FEAT-1", 1, 2)}}

	res, err := trunklog.Gather(context.Background(), src, nil, trunklog.Request{Count: 5})
	require.NoError(t, err)
	require.Len(t, res.Commits, 1)
	require.Equal(t, "Merge pull request #x", res.Commits[0].Message) // unchanged, no enrichment
	require.Nil(t, res.Commits[0].StackPRTitles)
}

func TestGather_TitleErrorDegradesGracefully(t *testing.T) {
	t.Parallel()
	src := &fakeSource{recent: []git.RecentCommit{merge("m", 100, "", 1, 2)}}
	titles := &fakeTitles{err: errors.New("rate limited")}

	res, err := trunklog.Gather(context.Background(), src, titles, trunklog.Request{Count: 5})
	require.NoError(t, err)
	require.Len(t, res.Commits, 1)
	require.Nil(t, res.Commits[0].StackPRTitles)
}

func TestGather_SourceErrorPropagates(t *testing.T) {
	t.Parallel()
	src := &fakeSource{err: errors.New("boom")}

	_, err := trunklog.Gather(context.Background(), src, nil, trunklog.Request{Count: 5})
	require.Error(t, err)
}
