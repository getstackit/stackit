package git_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
)

func TestBatchCommitInfo(t *testing.T) {
	t.Parallel()
	scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

	err := scene.Repo.CreateAndCheckoutBranch("branch1")
	require.NoError(t, err)
	err = scene.Repo.CreateChangeAndCommit("branch1 change", "b1")
	require.NoError(t, err)

	runner := git.NewRunnerWithPath(scene.Dir, nil)

	wantMainDate, err := runner.RunGitCommandWithContext(context.Background(), "log", "-1", "--format=%aI", "main")
	require.NoError(t, err)
	wantMainAuthor, err := runner.RunGitCommandWithContext(context.Background(), "log", "-1", "--format=%an", "main")
	require.NoError(t, err)
	wantBranch1Date, err := runner.RunGitCommandWithContext(context.Background(), "log", "-1", "--format=%aI", "branch1")
	require.NoError(t, err)
	wantBranch1Author, err := runner.RunGitCommandWithContext(context.Background(), "log", "-1", "--format=%an", "branch1")
	require.NoError(t, err)

	got := runner.BatchCommitInfo([]string{"main", "branch1", "no-such-branch"})

	require.Len(t, got, 2, "unmatched branch should be omitted, not errored")
	require.Equal(t, wantMainDate, got["main"].Date.Format(time.RFC3339))
	require.Equal(t, wantMainAuthor, got["main"].Author)
	require.Equal(t, wantBranch1Date, got["branch1"].Date.Format(time.RFC3339))
	require.Equal(t, wantBranch1Author, got["branch1"].Author)
}

// A tag sharing a branch's name makes git disambiguate %(refname:short) to
// "heads/<name>", which used to miss the caller's bare-branch-name lookup and
// silently return a zero CommitInfo.
func TestBatchCommitInfo_TagSharesBranchName(t *testing.T) {
	t.Parallel()
	scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

	err := scene.Repo.CreateAndCheckoutBranch("shadowed")
	require.NoError(t, err)
	err = scene.Repo.CreateChangeAndCommit("shadowed change", "s1")
	require.NoError(t, err)
	require.NoError(t, scene.Repo.RunGitCommand("tag", "shadowed", "main"))

	runner := git.NewRunnerWithPath(scene.Dir, nil)
	wantDate, err := runner.RunGitCommandWithContext(context.Background(), "log", "-1", "--format=%aI", "refs/heads/shadowed")
	require.NoError(t, err)

	got := runner.BatchCommitInfo([]string{"shadowed"})

	require.Contains(t, got, "shadowed", "result must be keyed by the bare branch name")
	require.Equal(t, wantDate, got["shadowed"].Date.Format(time.RFC3339))
	require.NotEmpty(t, got["shadowed"].Author)
}

func TestBatchCommitInfo_Empty(t *testing.T) {
	t.Parallel()
	scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

	runner := git.NewRunnerWithPath(scene.Dir, nil)
	got := runner.BatchCommitInfo(nil)
	require.Empty(t, got)
}

func TestBatchCommitInfoArbitraryRefs(t *testing.T) {
	t.Parallel()
	s := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
	require.NoError(t, s.Repo.CreateChangeAndCommit("another commit", "second"))
	require.NoError(t, s.Repo.RunGitCommand("tag", "-a", "annotated", "-m", "tag", "HEAD~1"))
	require.NoError(t, s.Repo.RunGitCommand("tag", "lightweight", "HEAD"))
	require.NoError(t, s.Repo.RunGitCommand("branch", "prefix/child"))
	sha, err := s.Repo.GetRevision("HEAD")
	require.NoError(t, err)
	logger := &captureGitLogger{}
	r := git.NewRunnerWithPath(s.Dir, logger)
	refs := []string{"main", "HEAD", "HEAD~1", "annotated", "lightweight", "refs/heads/main", sha, sha[:12], "missing", "HEAD^{tree}", "prefix", "HEAD", "bad\nref"}
	got := r.BatchCommitInfo(refs)
	require.Len(t, got, 8)
	for _, ref := range refs[:8] {
		want, err := r.RunGitCommandWithContext(context.Background(), "log", "-1", "--format=%aI", ref)
		require.NoError(t, err)
		require.Equal(t, want, got[ref].Date.Format(time.RFC3339), ref)
		require.NotEmpty(t, got[ref].Author, ref)
	}
	require.Equal(t, 1, logger.countDebugContaining("git cat-file --batch-check="))
	require.Equal(t, 1, logger.countDebugContaining("git log --no-walk="))
	require.NoError(t, s.Repo.RunGitCommand("checkout", "--detach", "HEAD~1"))
	detached := r.BatchCommitInfo([]string{"HEAD", "annotated"})
	require.Equal(t, detached["annotated"], detached["HEAD"])
	// A normal branch-only read still needs no object or log subprocesses.
	r.BatchCommitInfo([]string{"main"})
	require.Equal(t, 2, logger.countDebugContaining("git cat-file --batch-check="))
	require.Equal(t, 2, logger.countDebugContaining("git log --no-walk="))
}

func TestGetRemoteRevision_UsesConfiguredRemote(t *testing.T) {
	t.Parallel()
	scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

	// Point main's configured remote at a name other than "origin" and stand
	// in a local branch for that remote's tracking ref.
	require.NoError(t, scene.Repo.RunGitCommand("config", "branch.main.remote", "upstream"))
	mainSHA, err := scene.Repo.GetRevision("main")
	require.NoError(t, err)
	require.NoError(t, scene.Repo.RunGitCommand("branch", "upstream/main", mainSHA))

	runner := git.NewRunnerWithPath(scene.Dir, nil)
	got, err := runner.GetRemoteRevision("main")
	require.NoError(t, err)
	require.Equal(t, mainSHA, got)
}
