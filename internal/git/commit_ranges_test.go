package git_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
)

func TestReadCommitRangesBatch(t *testing.T) {
	t.Parallel()
	scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
	logger := &traceCaptureLogger{}
	runner := git.NewRunnerWithPath(scene.Dir, logger)
	ctx := t.Context()
	base, err := runner.ReadRevisions(ctx, "HEAD").One()
	require.NoError(t, err)
	tips := make([]string, 1, 51)
	tips[0] = base
	for i := range 50 {
		require.NoError(t, scene.Repo.RunGitCommand("commit", "--allow-empty", "-m", fmt.Sprintf("subject %d\n\nbody\n", i)))
		sha, err := runner.ReadRevisions(ctx, "HEAD").One()
		require.NoError(t, err)
		tips = append(tips, sha)
	}
	for _, width := range []int{1, 5} {
		ranges := make([]git.RevRange, 0)
		for i := width; i < len(tips); i += width {
			ranges = append(ranges, git.RevRange{Base: tips[i-width], Head: tips[i]})
		}
		{
			// Full metadata and topology use the same bounded traversal.
			logger.calls = 0
			data := runner.ReadCommitRanges(ctx, ranges...)
			require.Empty(t, data.Failures())
			require.LessOrEqual(t, logger.calls, width+1, "processes scale with commits per branch, not branches")
			for _, rr := range ranges {
				want, err := runner.ReadCommitRanges(ctx, rr).One()
				require.NoError(t, err)
				got, err := data.Get(rr.String())
				require.NoError(t, err)
				require.Equal(t, want, got)
			}
		}
		logger.calls = 0
		nodes := runner.ReadCommitNodes(ctx, ranges...)
		require.Empty(t, nodes.Failures())
		require.LessOrEqual(t, logger.calls, width+1)
		for _, rr := range ranges {
			want, err := runner.ReadCommitNodes(ctx, rr).One()
			require.NoError(t, err)
			got, err := nodes.Get(rr.String())
			require.NoError(t, err)
			require.Equal(t, want, got)
		}
		logger.calls = 0
		counts := runner.ReadCommitCounts(ctx, ranges...)
		require.Empty(t, counts.Failures())
		require.LessOrEqual(t, logger.calls, width+1)
		for _, rr := range ranges {
			count, err := counts.Get(rr.String())
			require.NoError(t, err)
			require.Equal(t, width, count)
		}
		logger.calls = 0
		ancestry := runner.ReadAncestry(ctx, ranges...)
		require.Empty(t, ancestry.Failures())
		require.LessOrEqual(t, logger.calls, width+1)
		for _, rr := range ranges {
			value, err := ancestry.Get(rr.String())
			require.NoError(t, err)
			require.True(t, value)
		}
	}
	// Long, overlapping, duplicate, equal, unbounded, reverse, and invalid
	// ranges must behave like independent native Git walks.
	ranges := []git.RevRange{
		{Base: base, Head: "HEAD"}, {Base: "HEAD~4", Head: "HEAD"},
		{Base: "HEAD~4", Head: "HEAD~2"}, {Base: "HEAD", Head: "HEAD"},
		{Base: "HEAD", Head: base}, {Head: "HEAD"},
		{Base: base, Head: "HEAD"}, {Base: "missing", Head: "HEAD"},
		{Base: base, Head: "missing"},
	}
	data := runner.ReadCommitRanges(ctx, ranges...)
	counts := runner.ReadCommitCounts(ctx, ranges...)
	for _, rr := range ranges {
		want, wantErr := runner.ReadCommitRanges(ctx, rr).One()
		got, err := data.Get(rr.String())
		if wantErr != nil {
			require.Error(t, err)
			_, err = counts.Get(rr.String())
			require.Error(t, err)
		} else {
			require.NoError(t, err)
			require.Equal(t, want, got, rr.String())
			count, err := counts.Get(rr.String())
			require.NoError(t, err)
			require.Equal(t, len(want), count)
		}
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	require.NotEmpty(t, runner.ReadCommitRanges(canceled, ranges...).Failures())
	require.Empty(t, runner.ReadCommitRanges(ctx).Values())
}

func TestCommitBatchMergeAndAliases(t *testing.T) {
	t.Parallel()
	scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
	runner := git.NewRunnerWithPath(scene.Dir, nil)
	ctx := t.Context()
	command := func(args ...string) string {
		t.Helper()
		out, err := runner.RunGitCommandWithContext(ctx, args...)
		require.NoError(t, err)
		return out
	}
	base := command("rev-parse", "HEAD")
	command("checkout", "-b", "side")
	command("commit", "--allow-empty", "-m", "side\nwrapped subject\n\nbody")
	command("checkout", "-b", "other", base)
	command("commit", "--allow-empty", "-m", "other")
	command("merge", "--no-ff", "side", "-m", "merge")
	command("tag", "-a", "tip-tag", "-m", "tag")
	orphan := command("commit-tree", "HEAD^{tree}", "-m", "unrelated root")
	ranges := []git.RevRange{
		{Base: base, Head: "HEAD"}, {Base: "side", Head: "tip-tag"},
		{Base: orphan, Head: "HEAD"}, {Base: "HEAD", Head: "side"},
	}
	data := runner.ReadCommitRanges(ctx, ranges...)
	ancestry := runner.ReadAncestry(ctx, ranges...)
	for _, rr := range ranges {
		want, err := runner.ReadCommitRanges(ctx, rr).One()
		require.NoError(t, err)
		got, err := data.Get(rr.String())
		require.NoError(t, err)
		require.Equal(t, want, got)
		wantAncestor, err := runner.IsAncestor(ctx, rr.Base, rr.Head)
		require.NoError(t, err)
		gotAncestor, err := ancestry.Get(rr.String())
		require.NoError(t, err)
		require.Equal(t, wantAncestor, gotAncestor)
	}
	refs := runner.ReadCommits(ctx, "HEAD", "tip-tag", "side", "missing", "HEAD^{tree}")
	head, err := refs.Get("HEAD")
	require.NoError(t, err)
	alias, err := refs.Get("tip-tag")
	require.NoError(t, err)
	require.Equal(t, head, alias)
	require.Len(t, head.Parents, 2)
	side, err := refs.Get("side")
	require.NoError(t, err)
	require.Equal(t, "side wrapped subject", side.Subject)
	require.Len(t, refs.Failures(), 2)
}
