package git_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
)

func TestBatchDiffNumstatMatchesGitDiff(t *testing.T) {
	t.Parallel()
	s := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
	logger := &captureGitLogger{}
	r := git.NewRunnerWithPath(s.Dir, logger)
	ctx := context.Background()
	write := func(name, content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(filepath.Join(s.Dir, name), []byte(content), 0600))
	}
	commit := func() string {
		t.Helper()
		require.NoError(t, s.Repo.RunGitCommand("add", "-A"))
		require.NoError(t, s.Repo.RunGitCommand("commit", "-m", "fixture"))
		sha, err := s.Repo.GetRevision("HEAD")
		require.NoError(t, err)
		return sha
	}
	initial, err := s.Repo.GetRevision("HEAD")
	require.NoError(t, err)
	write("old.txt", "first\nsecond\nthird\n")
	write("binary", "\x00old")
	write("copy-source", "one\ntwo\nthree\nfour\nfive\n")
	base := commit()
	require.NoError(t, os.Rename(filepath.Join(s.Dir, "old.txt"), filepath.Join(s.Dir, "new.txt")))
	write("binary", "\x00new")
	write("tabs\tand\nnewlines", "hello\n")
	write("copy-target", "one\ntwo\nthree\nfour\nfive\n")
	write("copy-source", "one\ntwo\nthree\nfour\nfive\nsix\n")
	head := commit()
	ranges := []git.RevRange{
		{Base: base, Head: head}, {Base: initial, Head: head},
		{Base: head, Head: base}, {Base: head, Head: head},
		{Base: base, Head: head}, // duplicate comparison
	}
	for _, setting := range []string{"true", "false", "copies", ""} {
		require.NoError(t, r.SetConfig("diff.renames", setting))
		before := logger.countDebugContaining("git diff-tree ")
		got, err := r.BatchDiffNumstat(ctx, ranges)
		require.NoError(t, err)
		require.Equal(t, before+1, logger.countDebugContaining("git diff-tree "), "one diff process for all ranges")
		require.Len(t, got, 4)
		for _, rr := range ranges {
			want, err := r.GetDiffNumstat(rr)
			require.NoError(t, err)
			require.Equal(t, want, got[rr], "%s with renames=%s", rr, setting)
		}
	}

	// Different commits with identical trees must still yield an empty result.
	require.NoError(t, s.Repo.RunGitCommand("commit", "--allow-empty", "-m", "empty"))
	empty, err := s.Repo.GetRevision("HEAD")
	require.NoError(t, err)
	got, err := r.BatchDiffNumstat(ctx, []git.RevRange{{Base: head, Head: empty}})
	require.NoError(t, err)
	require.Equal(t, map[git.RevRange]string{{Base: head, Head: empty}: ""}, got)

	// The batch must not return plausible partial stats for an invalid range.
	got, err = r.BatchDiffNumstat(ctx, append(ranges, git.RevRange{Base: base, Head: "no-such-ref"}))
	require.Error(t, err)
	require.Nil(t, got)
	got, err = r.BatchDiffNumstat(ctx, nil)
	require.NoError(t, err)
	require.Empty(t, got)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = r.BatchDiffNumstat(canceled, ranges)
	require.Error(t, err)
}

func TestBatchDiffNumstatSubmoduleConfig(t *testing.T) {
	t.Parallel()
	s := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
	r := git.NewRunnerWithPath(s.Dir, nil)
	ctx := context.Background()
	initial, err := s.Repo.GetRevision("HEAD")
	require.NoError(t, err)
	require.NoError(t, s.Repo.RunGitCommand("update-index", "--add", "--cacheinfo", "160000,"+initial+",sub"))
	require.NoError(t, s.Repo.RunGitCommand("commit", "-m", "add submodule"))
	base, err := s.Repo.GetRevision("HEAD")
	require.NoError(t, err)
	require.NoError(t, s.Repo.RunGitCommand("update-index", "--cacheinfo", "160000,"+base+",sub"))
	require.NoError(t, s.Repo.RunGitCommand("commit", "-m", "advance submodule"))
	head, err := s.Repo.GetRevision("HEAD")
	require.NoError(t, err)
	rr := git.RevRange{Base: base, Head: head}
	for _, setting := range []string{"none", "all", "dirty", "untracked"} {
		require.NoError(t, r.SetConfig("diff.ignoreSubmodules", setting))
		got, err := r.BatchDiffNumstat(ctx, []git.RevRange{rr})
		require.NoError(t, err)
		want, err := r.GetDiffNumstat(rr)
		require.NoError(t, err)
		require.Equal(t, want, got[rr], "ignoreSubmodules=%s", setting)
	}
}
