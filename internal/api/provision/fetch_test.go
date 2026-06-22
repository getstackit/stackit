package provision_test

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/getstackit/stackit/internal/api/provision"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/stretchr/testify/require"
)

func requireBranch(t *testing.T, repoRoot, branch string, present bool) {
	t.Helper()
	err := exec.Command("git", "-C", repoRoot, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch).Run()
	if present {
		require.NoError(t, err, "expected branch %q to exist", branch)
	} else {
		require.Error(t, err, "expected branch %q to be pruned", branch)
	}
}

func requireRef(t *testing.T, repoRoot, ref string, present bool) {
	t.Helper()
	err := exec.Command("git", "-C", repoRoot, "rev-parse", "--verify", "--quiet", ref).Run()
	if present {
		require.NoError(t, err, "expected ref %q to exist", ref)
	} else {
		require.Error(t, err, "expected ref %q to be pruned", ref)
	}
}

func TestMirrorFetchPullsNewBranchesAndDetachesHead(t *testing.T) {
	t.Parallel()
	src := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
	dest := filepath.Join(t.TempDir(), "mirror")
	require.NoError(t, provision.EnsureClone(context.Background(), provision.CloneParams{CloneURL: src.Dir, Dest: dest}))

	// A new branch appears upstream.
	require.NoError(t, src.Repo.RunGitCommand("checkout", "-b", "feature"))
	require.NoError(t, src.Repo.CreateChangeAndCommit("more", "more"))

	require.NoError(t, provision.MirrorFetch(context.Background(), dest, "origin", ""))

	requireBranch(t, dest, "feature", true)

	// A mirror leaves HEAD detached so future fetches can update every head.
	err := exec.Command("git", "-C", dest, "symbolic-ref", "-q", "HEAD").Run()
	require.Error(t, err, "HEAD should be detached after a mirror fetch")
}

func TestMirrorFetchPrunesDeletedBranches(t *testing.T) {
	t.Parallel()
	src := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
	dest := filepath.Join(t.TempDir(), "mirror")
	require.NoError(t, provision.EnsureClone(context.Background(), provision.CloneParams{CloneURL: src.Dir, Dest: dest}))

	require.NoError(t, src.Repo.RunGitCommand("checkout", "-b", "feature"))
	require.NoError(t, src.Repo.CreateChangeAndCommit("x", "x"))
	require.NoError(t, provision.MirrorFetch(context.Background(), dest, "origin", ""))
	requireBranch(t, dest, "feature", true)

	// Delete it upstream (switch off it first), then re-mirror.
	require.NoError(t, src.Repo.RunGitCommand("checkout", "-"))
	require.NoError(t, src.Repo.RunGitCommand("branch", "-D", "feature"))
	require.NoError(t, provision.MirrorFetch(context.Background(), dest, "origin", ""))

	requireBranch(t, dest, "feature", false)
}

func TestMirrorFetchPullsStackMetadata(t *testing.T) {
	t.Parallel()
	src := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
	dest := filepath.Join(t.TempDir(), "mirror")
	require.NoError(t, provision.EnsureClone(context.Background(), provision.CloneParams{CloneURL: src.Dir, Dest: dest}))

	blob, err := src.Repo.RunGitCommandAndGetOutput("hash-object", "-w", "--stdin")
	require.NoError(t, err)
	require.NoError(t, src.Repo.RunGitCommand("update-ref", "refs/stackit/stacks/test-stack", blob))

	require.NoError(t, provision.MirrorFetch(context.Background(), dest, "origin", ""))

	requireRef(t, dest, "refs/stackit/stacks/test-stack", true)
}
