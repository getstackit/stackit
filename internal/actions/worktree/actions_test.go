package worktree_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions/worktree"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestListAction(t *testing.T) {
	t.Parallel()

	t.Run("returns empty list when no worktrees registered", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		result, err := worktree.ListAction(s.Context, worktree.ListOptions{})
		require.NoError(t, err)
		require.Empty(t, result.Worktrees)
	})

	t.Run("lists registered worktrees", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		// Register a fake worktree
		err := s.Engine.RegisterWorktree("feature-stack", "/tmp/fake-worktree")
		require.NoError(t, err)

		result, err := worktree.ListAction(s.Context, worktree.ListOptions{})
		require.NoError(t, err)
		require.Len(t, result.Worktrees, 1)
		require.Equal(t, "feature-stack", result.Worktrees[0].AnchorBranch)
		require.Equal(t, "/tmp/fake-worktree", result.Worktrees[0].Path)
		require.False(t, result.Worktrees[0].Exists) // Path doesn't actually exist
		require.Equal(t, worktree.RegistrationStateInvalid, result.Worktrees[0].RegistrationState)
		require.True(t, result.Worktrees[0].NeedsRepair)

		// Clean up
		_ = s.Engine.UnregisterWorktree(s.Context, "feature-stack")
	})

	t.Run("marks legacy registrations", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		s.RunGit("checkout", "-b", "feature")
		require.NoError(t, s.Engine.TrackBranch(context.Background(), "feature", "main"))

		worktreeDir := t.TempDir()
		require.NoError(t, s.Engine.RegisterWorktreeWithName("feature", worktreeDir, "feature"))

		result, err := worktree.ListAction(s.Context, worktree.ListOptions{})
		require.NoError(t, err)
		require.Len(t, result.Worktrees, 1)
		require.Equal(t, worktree.RegistrationStateLegacy, result.Worktrees[0].RegistrationState)
		require.True(t, result.Worktrees[0].NeedsRepair)
		require.Equal(t, []string{"feature"}, result.Worktrees[0].RootBranches)

		_ = s.Engine.UnregisterWorktree(s.Context, "feature")
	})
}

func TestRemoveAction(t *testing.T) {
	t.Parallel()

	t.Run("fails when worktree not found", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		err := worktree.RemoveAction(s.Context, worktree.RemoveOptions{
			AnchorBranch: "nonexistent-stack",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "no worktree found")
	})

	t.Run("removes worktree registration", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		// Register a fake worktree (path doesn't exist)
		err := s.Engine.RegisterWorktree("feature-stack", "/tmp/nonexistent-worktree")
		require.NoError(t, err)

		// Verify it's registered
		wt, err := s.Engine.GetWorktreeForStack("feature-stack")
		require.NoError(t, err)
		require.NotNil(t, wt)

		// Remove it
		err = worktree.RemoveAction(s.Context, worktree.RemoveOptions{
			AnchorBranch: "feature-stack",
		})
		require.NoError(t, err)

		// Verify it's gone
		wt, err = s.Engine.GetWorktreeForStack("feature-stack")
		require.NoError(t, err)
		require.Nil(t, wt)
	})

	t.Run("removes stale invalid registration without repair", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		err := s.Engine.RegisterWorktree("feature-stack", "/tmp/nonexistent-worktree")
		require.NoError(t, err)

		err = worktree.RemoveAction(s.Context, worktree.RemoveOptions{
			AnchorBranch: "feature-stack",
		})
		require.NoError(t, err)

		wt, err := s.Engine.GetWorktreeForStack("feature-stack")
		require.NoError(t, err)
		require.Nil(t, wt)
	})
}

func TestOpenAction(t *testing.T) {
	t.Parallel()

	t.Run("fails when worktree not found", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		_, err := worktree.OpenAction(s.Context, worktree.OpenOptions{
			AnchorBranch: "nonexistent-stack",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "no worktree found")
	})

	t.Run("fails when path does not exist", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		// Register a fake worktree with non-existent path
		err := s.Engine.RegisterWorktree("feature-stack", "/tmp/nonexistent-path-12345")
		require.NoError(t, err)

		_, err = worktree.OpenAction(s.Context, worktree.OpenOptions{
			AnchorBranch: "feature-stack",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "does not exist")

		// Clean up
		_ = s.Engine.UnregisterWorktree(s.Context, "feature-stack")
	})

	t.Run("returns path when worktree exists", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		// Use a path that exists (the repo root)
		repoRoot := s.Context.RepoRoot
		err := s.Engine.RegisterWorktree("feature-stack", repoRoot)
		require.NoError(t, err)

		path, err := worktree.OpenAction(s.Context, worktree.OpenOptions{
			AnchorBranch: "feature-stack",
		})
		require.NoError(t, err)
		require.Equal(t, repoRoot, path)

		// Clean up
		_ = s.Engine.UnregisterWorktree(s.Context, "feature-stack")
	})
}

func TestCreateAction(t *testing.T) {
	t.Parallel()

	t.Run("succeeds when not on trunk and creates worktree from trunk", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		// Create and checkout a non-trunk branch
		s.RunGit("checkout", "-b", "feature")

		result, err := worktree.CreateAction(s.Context, worktree.CreateOptions{
			Name: "my-worktree",
		})
		require.NoError(t, err)
		require.NotNil(t, result)

		// Verify parent is trunk (worktree should be created from trunk regardless of current branch)
		anchorBranch := s.Engine.GetBranch(result.AnchorBranch)
		parent := anchorBranch.GetParent()
		require.NotNil(t, parent)
		require.True(t, parent.IsTrunk())

		// Clean up worktree
		_ = s.Engine.RemoveWorktree(s.Context.Context, result.Path)
		_ = s.Engine.UnregisterWorktree(s.Context, result.AnchorBranch)
	})

	t.Run("fails when name is empty", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		_, err := worktree.CreateAction(s.Context, worktree.CreateOptions{
			Name: "",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "name is required")
	})

	t.Run("fails when name contains invalid characters", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		_, err := worktree.CreateAction(s.Context, worktree.CreateOptions{
			Name: "my/worktree",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "path separators")
	})

	t.Run("fails when worktree name already exists", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		// Create first worktree
		result, err := worktree.CreateAction(s.Context, worktree.CreateOptions{
			Name: "duplicate-name",
		})
		require.NoError(t, err)

		// Try to create second worktree with same name
		_, err = worktree.CreateAction(s.Context, worktree.CreateOptions{
			Name: "duplicate-name",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "already exists")

		// Clean up
		_ = s.Engine.RemoveWorktree(s.Context.Context, result.Path)
		_ = s.Engine.UnregisterWorktree(s.Context, result.AnchorBranch)
	})

	t.Run("creates worktree with anchor branch", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		result, err := worktree.CreateAction(s.Context, worktree.CreateOptions{
			Name: "my-feature",
		})
		require.NoError(t, err)
		require.NotNil(t, result)

		// Verify result fields
		require.Equal(t, "my-feature", result.Name)
		require.NotEmpty(t, result.AnchorBranch)
		require.Contains(t, result.AnchorBranch, "-wt")
		require.NotEmpty(t, result.Path)

		// Verify the anchor branch exists and is a worktree anchor
		anchorBranch := s.Engine.GetBranch(result.AnchorBranch)
		require.True(t, anchorBranch.IsTracked())
		require.True(t, anchorBranch.IsWorktreeAnchor())

		// Verify parent is trunk
		parent := anchorBranch.GetParent()
		require.NotNil(t, parent)
		require.True(t, parent.IsTrunk())

		// Clean up worktree
		_ = s.Engine.RemoveWorktree(s.Context.Context, result.Path)
		_ = s.Engine.UnregisterWorktree(s.Context, result.AnchorBranch)
	})

	t.Run("creates worktree with scope", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		result, err := worktree.CreateAction(s.Context, worktree.CreateOptions{
			Name:  "scoped-feature",
			Scope: "backend",
		})
		require.NoError(t, err)
		require.NotNil(t, result)

		// Verify scope is set on anchor branch
		anchorBranch := s.Engine.GetBranch(result.AnchorBranch)
		scope := s.Engine.GetScope(anchorBranch)
		require.Equal(t, "backend", scope.String())

		// Clean up worktree
		_ = s.Engine.RemoveWorktree(s.Context.Context, result.Path)
		_ = s.Engine.UnregisterWorktree(s.Context, result.AnchorBranch)
	})
}

func TestRepairAction(t *testing.T) {
	t.Parallel()

	t.Run("converts legacy registration to hidden anchor", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		s.RunGit("checkout", "-b", "feature")
		require.NoError(t, s.Engine.TrackBranch(context.Background(), "feature", "main"))

		worktreeDir := t.TempDir()
		require.NoError(t, s.Engine.RegisterWorktreeWithName("feature", worktreeDir, "legacy-wt"))

		result, err := worktree.RepairAction(s.Context, worktree.RepairOptions{NameOrBranch: "legacy-wt"})
		require.NoError(t, err)
		require.Len(t, result.Repaired, 1)
		require.Equal(t, "converted legacy registration to hidden anchor", result.Repaired[0].Action)
		require.NotEmpty(t, result.Repaired[0].AnchorBranch)
		require.NotEqual(t, "feature", result.Repaired[0].AnchorBranch)

		legacy, err := s.Engine.GetWorktreeForStack("feature")
		require.NoError(t, err)
		require.Nil(t, legacy)

		repaired, err := s.Engine.GetWorktreeForStack(result.Repaired[0].AnchorBranch)
		require.NoError(t, err)
		require.NotNil(t, repaired)
		require.Equal(t, worktreeDir, repaired.Path)
		require.True(t, s.Engine.GetBranch(result.Repaired[0].AnchorBranch).IsWorktreeAnchor())
		require.Equal(t, result.Repaired[0].AnchorBranch, s.Engine.GetBranch("feature").GetParent().GetName())

		_ = s.Engine.UnregisterWorktree(s.Context, result.Repaired[0].AnchorBranch)
	})
}
