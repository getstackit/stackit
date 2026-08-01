package engine_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestIsInManagedWorktree_MainRepo(t *testing.T) {
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
	s.WithInitialCommit()

	// When running from the main repo, IsInManagedWorktree should return false
	isManaged, info, err := s.Engine.IsInManagedWorktree()
	require.NoError(t, err)
	assert.False(t, isManaged)
	assert.Nil(t, info)
}

func TestWorktreeRegistry_StackRootForBranch(t *testing.T) {
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
	s.WithInitialCommit()

	// Create a stack: main -> branch1 -> branch2
	s.CreateBranch("branch1").Commit("branch1 change")
	s.TrackBranch("branch1", "main")
	s.CreateBranch("branch2").Commit("branch2 change")
	s.TrackBranch("branch2", "branch1")

	// Register a worktree for branch1 (as if it were the stack root)
	err := s.Engine.RegisterWorktree("branch1", "/tmp/test-worktree-branch1")
	require.NoError(t, err)

	// Stack root for branch2 should be branch1 (the first branch whose parent is trunk)
	branch2 := s.Engine.GetBranch("branch2")
	stackRoot := s.Engine.GetStackRootForBranch(branch2)
	assert.Equal(t, "branch1", stackRoot)

	// Stack root for branch1 should be branch1 itself (its parent is trunk)
	branch1 := s.Engine.GetBranch("branch1")
	stackRoot = s.Engine.GetStackRootForBranch(branch1)
	assert.Equal(t, "branch1", stackRoot)

	// Clean up
	_ = s.Engine.UnregisterWorktree(s.Context, "branch1")
}

func TestWorktreeRegistry_OwningWorktree(t *testing.T) {
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
	s.WithInitialCommit()

	s.CreateBranch("feature").Commit("feature change")
	s.TrackBranch("feature", "main")
	s.CreateBranch("feature-child").Commit("feature child change")
	s.TrackBranch("feature-child", "feature")

	require.NoError(t, s.Engine.RegisterWorktreeWithName("feature", "/tmp/feature-worktree", "feature"))
	t.Cleanup(func() { _ = s.Engine.UnregisterWorktree(s.Context, "feature") })

	owner, err := s.Engine.OwningWorktree(s.Engine.GetBranch("feature-child"))
	require.NoError(t, err)
	require.NotNil(t, owner)
	assert.Equal(t, "feature", owner.AnchorBranch)
	assert.Equal(t, "/tmp/feature-worktree", owner.Path)

	owner, err = s.Engine.OwningWorktree(s.Engine.Trunk())
	require.NoError(t, err)
	assert.Nil(t, owner)
}

func TestCreateTemporaryWorktree(t *testing.T) {
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

	// Create a branch to use for the worktree
	s.CreateBranch("feature").
		Commit("feature change").
		Checkout("main")

	ctx := context.Background()
	worktreePath, cleanup, err := s.Engine.CreateTemporaryWorktree(ctx, "feature", "stackit-test-*")
	require.NoError(t, err)
	require.NotEmpty(t, worktreePath)
	require.NotNil(t, cleanup)

	// Verify worktree exists
	info, err := os.Stat(worktreePath)
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	// Verify it's a git worktree by checking for .git file (not directory)
	gitFile, err := os.Stat(worktreePath + "/.git")
	require.NoError(t, err)
	assert.False(t, gitFile.IsDir())

	// Run cleanup
	cleanup()

	// Verify worktree and temp dir are gone
	_, err = os.Stat(worktreePath)
	assert.True(t, os.IsNotExist(err))

	// The parent directory of worktreePath (which is the temp dir) should also be gone
	// worktreePath is filepath.Join(tmpDir, "worktree")
	tmpDir := worktreePath[:len(worktreePath)-9] // "/worktree" is 9 chars
	_, err = os.Stat(tmpDir)
	assert.True(t, os.IsNotExist(err))
}

func TestCreateTemporaryWorktree_FastCleanup_AllowsNextCreate(t *testing.T) {
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
	s.CreateBranch("feature").
		Commit("feature change").
		Checkout("main")

	ctx := context.Background()
	worktreePath, cleanup, err := s.Engine.CreateTemporaryWorktree(ctx, "feature", "stackit-test-*")
	require.NoError(t, err)
	require.NotEmpty(t, worktreePath)
	cleanup()

	worktreePath2, cleanup2, err := s.Engine.CreateTemporaryWorktree(ctx, "feature", "stackit-test-*")
	require.NoError(t, err)
	require.NotEmpty(t, worktreePath2)
	cleanup2()
}

func TestCreateTemporaryWorktree_RetryAfterStaleEntry(t *testing.T) {
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
	s.CreateBranch("feature").
		Commit("feature change").
		Checkout("main")

	ctx := context.Background()
	worktreePath, _, err := s.Engine.CreateTemporaryWorktree(ctx, "feature", "stackit-test-*")
	require.NoError(t, err)
	require.NotEmpty(t, worktreePath)

	// Simulate an abrupt deletion without cleanup to leave a stale git worktree entry.
	tmpDir := filepath.Dir(worktreePath)
	require.NoError(t, os.RemoveAll(tmpDir))

	worktreePath2, cleanup2, err := s.Engine.CreateTemporaryWorktreeSkipPrune(ctx, "feature", "stackit-test-*")
	require.NoError(t, err)
	require.NotEmpty(t, worktreePath2)
	cleanup2()
}
