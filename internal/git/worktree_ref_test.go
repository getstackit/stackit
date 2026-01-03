package git_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/internal/git"
	"stackit.dev/stackit/testhelpers"
)

func TestResolveRefInWorktree(t *testing.T) {
	scene := testhelpers.NewScene(t, func(s *testhelpers.Scene) error {
		if err := s.Repo.CreateChangeAndCommit("initial", "main"); err != nil {
			return err
		}
		if err := s.Repo.CreateBranch("feature"); err != nil {
			return err
		}
		// Ensure we are on main, so feature can be checked out in a worktree
		return s.Repo.CheckoutBranch("main")
	})

	mainSHA, err := scene.Repo.GetRevision("main")
	require.NoError(t, err)

	// Create a temporary directory for the worktree
	tmpDir := t.TempDir()
	// Normalize path for macOS
	tmpDir, err = filepath.EvalSymlinks(tmpDir)
	require.NoError(t, err)
	worktreePath := filepath.Join(tmpDir, "worktree")

	// Add worktree for feature branch using the main runner
	mainRunner := git.NewRunnerWithPath(scene.Repo.Dir)
	err = mainRunner.AddWorktree(context.Background(), worktreePath, "feature", false)
	require.NoError(t, err)

	// Create a NEW runner pointing to the worktree
	worktreeRunner := git.NewRunnerWithPath(worktreePath)
	err = worktreeRunner.InitDefaultRepo()
	require.NoError(t, err)

	// Try to resolve 'main' from the worktree runner.
	// This exercises the fix where we fall back to 'git rev-parse' if go-git
	// fails to resolve a reference in a worktree.
	resolvedSHA, err := worktreeRunner.GetRevision("main")
	require.NoError(t, err, "Should be able to resolve 'main' from a worktree")
	require.Equal(t, mainSHA, resolvedSHA)

	// Also test resolving by full ref name
	resolvedSHA2, err := worktreeRunner.GetRevision("refs/heads/main")
	require.NoError(t, err)
	require.Equal(t, mainSHA, resolvedSHA2)

	// Test resolving HEAD
	headSHA, err := worktreeRunner.GetRevision("HEAD")
	require.NoError(t, err)
	featureSHA, err := scene.Repo.GetRevision("feature")
	require.NoError(t, err)
	require.Equal(t, featureSHA, headSHA)
}
