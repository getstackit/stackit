package git_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
)

func TestReadDiffs(t *testing.T) {
	t.Parallel()
	scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
	logger := &traceCaptureLogger{}
	runner := git.NewRunnerWithPath(scene.Dir, logger)
	ctx := t.Context()
	write := func(name, content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(filepath.Join(scene.Dir, name), []byte(content), 0600))
	}
	commit := func(message string) {
		t.Helper()
		require.NoError(t, scene.Repo.RunGitCommand("add", "-A"))
		require.NoError(t, scene.Repo.RunGitCommand("commit", "-m", message))
	}
	write("old\tname\n.txt", "rename me\n")
	write("text.txt", "before\n")
	commit("base")
	base, err := runner.ReadRevisions(ctx, "HEAD").One()
	require.NoError(t, err)
	require.NoError(t, os.Rename(filepath.Join(scene.Dir, "old\tname\n.txt"), filepath.Join(scene.Dir, "new\tname\n.txt")))
	write("text.txt", "after\nextra\n")
	write("binary.dat", "\x00binary\xff")
	// A path shaped like a tree-pair header must remain a filename.
	tree, err := runner.ReadRevisions(ctx, "HEAD^{tree}").One()
	require.NoError(t, err)
	oddPath := tree + " " + tree + "\n"
	write(oddPath, "strange path\n")
	commit("head")
	head, err := runner.ReadRevisions(ctx, "HEAD").One()
	require.NoError(t, err)
	require.NoError(t, scene.Repo.RunGitCommand("tag", "-a", "tip-tag", "-m", "tip"))
	require.NoError(t, scene.Repo.RunGitCommand("commit", "--allow-empty", "-m", "same tree"))
	ranges := []git.RevRange{
		{Base: base, Head: head}, {Base: head, Head: base},
		{Base: base, Head: "tip-tag"}, {Base: head, Head: "HEAD"},
		{Base: "missing-ref", Head: head}, {Base: base, Head: "missing-ref"},
		{Base: base, Head: head}, // Repeated requests and aliased trees are deduplicated.
	}
	for _, mode := range []git.DiffReadMode{git.DiffCheckOnly, git.DiffNames, git.DiffStats} {
		logger.calls = 0
		got := runner.ReadDiffs(ctx, mode, ranges...)
		wantCalls := 2
		if mode == git.DiffCheckOnly {
			wantCalls = 1
		}
		require.Equal(t, wantCalls, logger.calls, "process count must not scale with ranges")
		require.Len(t, got.Failures(), 2)
		for _, rr := range ranges[:4] {
			diff, err := got.Get(rr.String())
			require.NoError(t, err)
			single, err := runner.ReadDiffs(ctx, mode, rr).One()
			require.NoError(t, err)
			require.Equal(t, single, diff, "one and many must agree for %s", rr)
			if mode == git.DiffCheckOnly {
				continue
			}
			oracle, err := runner.RunGitCommandRawWithContext(ctx, "diff", "--name-only", "-z", "-M", rr.Base, rr.Head, "--")
			require.NoError(t, err)
			want := []string{}
			if oracle != "" {
				want = strings.Split(strings.TrimSuffix(oracle, "\x00"), "\x00")
			}
			slices.Sort(want)
			require.Equal(t, want, diff.Files)
		}
		empty, err := got.Get(ranges[3].String())
		require.NoError(t, err)
		require.True(t, empty.Empty, "distinct commits with equal trees are empty")
		if mode == git.DiffStats {
			forward, err := got.Get(ranges[0].String())
			require.NoError(t, err)
			require.Equal(t, 3, forward.Added)
			require.Equal(t, 1, forward.Deleted)
			reverse, err := got.Get(ranges[1].String())
			require.NoError(t, err)
			require.Equal(t, 1, reverse.Added)
			require.Equal(t, 3, reverse.Deleted)
		}
	}
}

func TestReadDiffsEmptyInvalidAndCanceled(t *testing.T) {
	t.Parallel()
	scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
	logger := &traceCaptureLogger{}
	runner := git.NewRunnerWithPath(scene.Dir, logger)
	require.Empty(t, runner.ReadDiffs(t.Context(), git.DiffStats).Values())
	require.Zero(t, logger.calls)
	for _, mode := range []git.DiffReadMode{git.DiffCheckOnly, git.DiffNames, git.DiffStats, -1} {
		_, err := runner.ReadDiffs(t.Context(), mode, git.RevRange{}).One()
		require.Error(t, err)
	}
	require.Zero(t, logger.calls)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, mode := range []git.DiffReadMode{git.DiffCheckOnly, git.DiffNames, git.DiffStats} {
		_, err := runner.ReadDiffs(ctx, mode, git.RevRange{Base: "main", Head: "HEAD"}).One()
		require.Error(t, err)
	}
}
