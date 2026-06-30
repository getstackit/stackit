package git_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
)

func TestIsMerged(t *testing.T) {
	t.Run("returns false for unmerged branch", func(t *testing.T) {
		scene := testhelpers.NewScene(t, testhelpers.InitialCommitSceneSetup)

		// Create branch
		err := scene.Repo.CreateAndCheckoutBranch("branch1")
		require.NoError(t, err)
		err = scene.Repo.CreateChangeAndCommit("branch1 change", "b1")
		require.NoError(t, err)
		err = scene.Repo.CheckoutBranch("main")
		require.NoError(t, err)

		// Initialize git repo
		runner := git.NewRunnerWithPath(scene.Repo.Dir, nil)

		// Branch is not merged
		merged, err := runner.IsMerged(context.Background(), "branch1", "main")
		require.NoError(t, err)
		require.False(t, merged)
	})

	t.Run("returns true for merged branch", func(t *testing.T) {
		scene := testhelpers.NewScene(t, testhelpers.InitialCommitSceneSetup)

		// Create branch
		err := scene.Repo.CreateAndCheckoutBranch("branch1")
		require.NoError(t, err)
		err = scene.Repo.CreateChangeAndCommit("branch1 change", "b1")
		require.NoError(t, err)

		// Merge branch into main
		err = scene.Repo.CheckoutBranch("main")
		require.NoError(t, err)
		err = scene.Repo.MergeBranch("main", "branch1")
		require.NoError(t, err)

		// Initialize git repo
		runner := git.NewRunnerWithPath(scene.Repo.Dir, nil)

		// Branch should be merged
		merged, err := runner.IsMerged(context.Background(), "branch1", "main")
		require.NoError(t, err)
		require.True(t, merged)
	})

	t.Run("returns true for rebase-merged branch", func(t *testing.T) {
		scene := testhelpers.NewScene(t, testhelpers.InitialCommitSceneSetup)

		err := scene.Repo.CreateAndCheckoutBranch("branch1")
		require.NoError(t, err)
		err = scene.Repo.CreateChangeAndCommit("branch1 change 1", "b1")
		require.NoError(t, err)
		err = scene.Repo.CreateChangeAndCommit("branch1 change 2", "b2")
		require.NoError(t, err)

		commitsOutput, err := scene.Repo.RunGitCommandAndGetOutput("rev-list", "--reverse", "main..branch1")
		require.NoError(t, err)
		commits := strings.Fields(commitsOutput)
		require.Len(t, commits, 2)

		err = scene.Repo.CheckoutBranch("main")
		require.NoError(t, err)
		err = scene.Repo.CreateChangeAndCommit("unrelated trunk change", "trunk")
		require.NoError(t, err)
		for _, commit := range commits {
			err = scene.Repo.RunGitCommand("cherry-pick", commit)
			require.NoError(t, err)
		}

		runner := git.NewRunnerWithPath(scene.Repo.Dir, nil)
		merged, err := runner.IsMerged(context.Background(), "branch1", "main")
		require.NoError(t, err)
		require.True(t, merged)
	})

	// A multi-commit squash matches no individual branch commit, so the cheap
	// per-commit IsMerged cannot see it — only the aggregate-patch-id
	// IsSquashMerged can. This split keeps the expensive scan off the hot
	// IsMerged path.
	t.Run("multi-commit squash: IsMerged false, IsSquashMerged true", func(t *testing.T) {
		scene := testhelpers.NewScene(t, testhelpers.InitialCommitSceneSetup)

		err := scene.Repo.CreateAndCheckoutBranch("branch1")
		require.NoError(t, err)
		err = scene.Repo.CreateChangeAndCommit("v1", "feature")
		require.NoError(t, err)
		err = scene.Repo.CreateChangeAndCommit("v1\nv2", "feature")
		require.NoError(t, err)

		err = scene.Repo.CheckoutBranch("main")
		require.NoError(t, err)
		err = scene.Repo.CreateChangeAndCommit("unrelated trunk change", "trunk")
		require.NoError(t, err)
		err = scene.Repo.RunGitCommand("merge", "--squash", "branch1")
		require.NoError(t, err)
		err = scene.Repo.RunGitCommand("commit", "-m", "squash branch1")
		require.NoError(t, err)

		runner := git.NewRunnerWithPath(scene.Repo.Dir, nil)

		merged, err := runner.IsMerged(context.Background(), "branch1", "main")
		require.NoError(t, err)
		require.False(t, merged, "cheap IsMerged must not detect a multi-commit squash")

		squashMerged, err := runner.IsSquashMerged(context.Background(), "branch1", "main", git.NewSquashMergeCache())
		require.NoError(t, err)
		require.True(t, squashMerged, "IsSquashMerged must detect the aggregate squash")
	})
}

type captureGitLogger struct {
	mu    sync.Mutex
	debug []string
}

func (l *captureGitLogger) Debug(msg string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.debug = append(l.debug, fmt.Sprintf(msg, args...))
}

func (l *captureGitLogger) Info(string, ...any) {}

func (l *captureGitLogger) countDebugContaining(needle string) int {
	l.mu.Lock()
	defer l.mu.Unlock()

	count := 0
	for _, line := range l.debug {
		if strings.Contains(line, needle) {
			count++
		}
	}
	return count
}

func TestIsSquashMergedCachesTargetCommitPatchIDs(t *testing.T) {
	scene := testhelpers.NewScene(t, testhelpers.InitialCommitSceneSetup)

	err := scene.Repo.CreateAndCheckoutBranch("branch1")
	require.NoError(t, err)
	err = scene.Repo.CreateChangeAndCommit("branch1 change", "branch1")
	require.NoError(t, err)

	err = scene.Repo.CheckoutBranch("main")
	require.NoError(t, err)
	err = scene.Repo.CreateAndCheckoutBranch("branch2")
	require.NoError(t, err)
	err = scene.Repo.CreateChangeAndCommit("branch2 change", "branch2")
	require.NoError(t, err)

	err = scene.Repo.CheckoutBranch("main")
	require.NoError(t, err)
	for _, name := range []string{"target1", "target2", "target3"} {
		err = scene.Repo.CreateChangeAndCommit(name+" change", name)
		require.NoError(t, err)
	}

	targetCommitsOutput, err := scene.Repo.RunGitCommandAndGetOutput("rev-list", "--reverse", "branch1..main")
	require.NoError(t, err)
	targetCommits := strings.Fields(targetCommitsOutput)
	require.Len(t, targetCommits, 3)

	logger := &captureGitLogger{}
	runner := git.NewRunnerWithPath(scene.Repo.Dir, logger)

	cache := git.NewSquashMergeCache()
	merged, err := runner.IsSquashMerged(context.Background(), "branch1", "main", cache)
	require.NoError(t, err)
	require.False(t, merged)

	merged, err = runner.IsSquashMerged(context.Background(), "branch2", "main", cache)
	require.NoError(t, err)
	require.False(t, merged)

	for _, commit := range targetCommits {
		require.Equal(t, 1, logger.countDebugContaining("git show --format= --no-ext-diff --full-index "+commit))
	}
}
