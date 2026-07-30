package engine

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
)

// newOptimisticLockTestRepo creates a minimal repo (avoiding global git
// config, per the convention in internal/git/bench_test.go) with trunk
// "main" and a branch "feature" whose parent has since moved, so restacking
// it exercises the real rebase + optimistic-locking ref-update path.
func newOptimisticLockTestRepo(t *testing.T) (dir string, g git.Runner) {
	t.Helper()
	dir = t.TempDir()

	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...) //nolint:gosec // test helper, fixed argv
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}
	writeAndCommit := func(name, content, message string) {
		t.Helper()
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
		runGit("add", name)
		runGit("commit", "-m", message)
	}

	require.NoError(t, exec.Command("git", "-c", "init.defaultBranch=main", "init", dir, "-b", "main").Run())
	runGit("config", "user.name", "Test User")
	runGit("config", "user.email", "test@example.com")

	writeAndCommit("seed.txt", "seed", "initial commit")
	runGit("checkout", "-b", "feature")
	writeAndCommit("feature.txt", "feature content", "feature commit")
	runGit("checkout", "main")
	writeAndCommit("seed.txt", "seed-2", "main advances")

	return dir, git.NewRunnerWithPath(dir, nil)
}

// flakyRevisionRunner wraps a real git.Runner and fails GetRevision for a
// single named branch, simulating the transient read failure that #1487
// fixed restackBranch/applyBranchAndMetadata for. Every other call
// delegates to the embedded real runner.
type flakyRevisionRunner struct {
	git.Runner
	failFor string
}

func (r *flakyRevisionRunner) GetRevision(branchName string) (string, error) {
	if branchName == r.failFor {
		return "", errors.New("injected: transient revision read failure")
	}
	return r.Runner.GetRevision(branchName)
}

// TestRestackBranchPropagatesRevisionReadFailure regression-tests #1487
// ("don't silently bypass optimistic locking on restack"). Before the fix,
// restackBranch discarded the error from branch.GetRevision() when capturing
// the branch's current SHA for the optimistic-locking ref update, so
// oldBranchSHA silently became "" -- and UpdateRefsBatch treats an empty
// OldSHA as "skip verification" rather than "expect empty ref", disabling
// the concurrent-modification check entirely. Post-fix, the error must
// surface as RestackConflict and the metadata ref must stay untouched.
func TestRestackBranchPropagatesRevisionReadFailure(t *testing.T) {
	t.Parallel()

	dir, realGit := newOptimisticLockTestRepo(t)
	ctx := context.Background()

	// Track "feature" with the real runner first: TrackBranch's divergence
	// computation reads revisions too, and we only want the fault to fire
	// once restackBranch itself asks for the branch's current SHA.
	trackingEngine, err := NewEngine(Options{RepoRoot: dir, Trunk: "main", Git: realGit})
	require.NoError(t, err)
	trackingImpl, ok := trackingEngine.(*engineImpl)
	require.True(t, ok)
	require.NoError(t, trackingImpl.TrackBranch(ctx, "feature", "main"))

	metaRef := git.MetadataRefPrefix + "feature"
	realMetadataSHA, err := realGit.GetRef(metaRef)
	require.NoError(t, err)
	realBranchSHA, err := realGit.GetRevision("feature")
	require.NoError(t, err)

	flakyGit := &flakyRevisionRunner{Runner: realGit, failFor: "feature"}
	eng, err := NewEngine(Options{RepoRoot: dir, Trunk: "main", Git: flakyGit})
	require.NoError(t, err)
	e, ok := eng.(*engineImpl)
	require.True(t, ok)

	snap := &restackSnapshot{metaRefSHAs: map[string]string{"feature": realMetadataSHA}}
	result, err := e.restackBranch(ctx, e.GetBranch("feature"), snap, git.NewSquashMergeCache())
	require.Error(t, err, "a failed branch-revision read must fail the restack, not silently bypass the optimistic lock")
	require.Equal(t, RestackConflict, result.Result)

	afterMetadataSHA, getErr := realGit.GetRef(metaRef)
	require.NoError(t, getErr)
	require.Equal(t, realMetadataSHA, afterMetadataSHA, "metadata ref must be untouched when the branch revision can't be verified")

	// The branch's own commits are rebased by git.Rebase before the failed
	// revision read is reached (see git.Runner.Rebase, which updates the
	// branch ref unconditionally). What optimistic locking protects here is
	// the metadata ref, not whether the rebase itself ran -- so the branch
	// tip having moved on is expected, not a sign the lock was bypassed.
	afterBranchSHA, getErr := realGit.GetRevision("feature")
	require.NoError(t, getErr)
	require.NotEqual(t, realBranchSHA, afterBranchSHA, "sanity check: the rebase itself should have run")
}

// TestApplyBranchAndMetadataPropagatesRevisionReadFailure covers the second
// call site #1487 fixed (applyBranchAndMetadata, used by the
// validated-rebase restack path). Same invariant as
// TestRestackBranchPropagatesRevisionReadFailure: a failed branch-revision
// read must reject the update rather than silently drop the optimistic lock.
func TestApplyBranchAndMetadataPropagatesRevisionReadFailure(t *testing.T) {
	t.Parallel()

	dir, realGit := newOptimisticLockTestRepo(t)
	ctx := context.Background()

	trackingEngine, err := NewEngine(Options{RepoRoot: dir, Trunk: "main", Git: realGit})
	require.NoError(t, err)
	trackingImpl, ok := trackingEngine.(*engineImpl)
	require.True(t, ok)
	require.NoError(t, trackingImpl.TrackBranch(ctx, "feature", "main"))

	metaRef := git.MetadataRefPrefix + "feature"
	realMetadataSHA, err := realGit.GetRef(metaRef)
	require.NoError(t, err)
	featureSHA, err := realGit.GetRevision("feature")
	require.NoError(t, err)
	mainSHA, err := realGit.GetRevision("main")
	require.NoError(t, err)

	flakyGit := &flakyRevisionRunner{Runner: realGit, failFor: "feature"}
	eng, err := NewEngine(Options{RepoRoot: dir, Trunk: "main", Git: flakyGit})
	require.NoError(t, err)
	e, ok := eng.(*engineImpl)
	require.True(t, ok)

	snap := &restackSnapshot{metaRefSHAs: map[string]string{"feature": realMetadataSHA}}
	item := RestackPlanItem{Branch: "feature", NewParent: "main"}
	result, err := e.applyBranchAndMetadata(ctx, e.GetBranch("feature"), item, featureSHA, mainSHA, snap)
	require.Error(t, err, "a failed branch-revision read must fail the update, not silently bypass the optimistic lock")
	require.Equal(t, RestackConflict, result.Result)

	afterMetadataSHA, getErr := realGit.GetRef(metaRef)
	require.NoError(t, getErr)
	require.Equal(t, realMetadataSHA, afterMetadataSHA, "metadata ref must be untouched when the branch revision can't be verified")

	afterBranchSHA, getErr := realGit.GetRevision("feature")
	require.NoError(t, getErr)
	require.Equal(t, featureSHA, afterBranchSHA, "branch ref must be untouched: applyBranchAndMetadata never rebases, it only applies the given SHAs")
}
