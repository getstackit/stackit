package worktree_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions/worktree"
	"github.com/getstackit/stackit/internal/engine"
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
		err := s.Engine.RegisterWorktree(context.Background(), engine.WorktreeRegistration{AnchorBranch: "feature-stack", Path: "/tmp/fake-worktree"})
		require.NoError(t, err)

		result, err := worktree.ListAction(s.Context, worktree.ListOptions{})
		require.NoError(t, err)
		require.Len(t, result.Worktrees, 1)
		require.Equal(t, "feature-stack", result.Worktrees[0].AnchorBranch)
		require.Equal(t, "/tmp/fake-worktree", result.Worktrees[0].Path.String())
		require.Equal(t, worktree.WorktreeMissing, result.Worktrees[0].Lifecycle.Presence) // Path doesn't actually exist
		require.Equal(t, worktree.RegistrationStateInvalid, result.Worktrees[0].Lifecycle.Registration)
		require.True(t, result.Worktrees[0].Lifecycle.NeedsRepair())

		// Clean up
		_ = s.Engine.UnregisterWorktree(s.Context, "feature-stack")
	})

	t.Run("filters by name or anchor branch", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		require.NoError(t, s.Engine.RegisterWorktree(context.Background(), engine.WorktreeRegistration{AnchorBranch: "first-anchor", Path: engine.WorktreePath(t.TempDir()), Name: "first"}))
		require.NoError(t, s.Engine.RegisterWorktree(context.Background(), engine.WorktreeRegistration{AnchorBranch: "second-anchor", Path: engine.WorktreePath(t.TempDir()), Name: "second"}))
		t.Cleanup(func() {
			_ = s.Engine.UnregisterWorktree(s.Context, "first-anchor")
			_ = s.Engine.UnregisterWorktree(s.Context, "second-anchor")
		})

		byName, err := worktree.ListAction(s.Context, worktree.ListOptions{Selector: "second"})
		require.NoError(t, err)
		require.Len(t, byName.Worktrees, 1)
		assert.Equal(t, "second-anchor", byName.Worktrees[0].AnchorBranch)

		byAnchor, err := worktree.ListAction(s.Context, worktree.ListOptions{Selector: "first-anchor"})
		require.NoError(t, err)
		require.Len(t, byAnchor.Worktrees, 1)
		assert.Equal(t, "first", byAnchor.Worktrees[0].Name.String())
	})

	t.Run("marks legacy registrations", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		s.RunGit("checkout", "-b", "feature")
		require.NoError(t, s.Engine.TrackBranch(context.Background(), "feature", "main"))

		worktreeDir := t.TempDir()
		require.NoError(t, s.Engine.RegisterWorktree(context.Background(), engine.WorktreeRegistration{AnchorBranch: "feature", Path: engine.WorktreePath(worktreeDir), Name: "feature"}))

		result, err := worktree.ListAction(s.Context, worktree.ListOptions{})
		require.NoError(t, err)
		require.Len(t, result.Worktrees, 1)
		require.Equal(t, worktree.RegistrationStateLegacy, result.Worktrees[0].Lifecycle.Registration)
		require.True(t, result.Worktrees[0].Lifecycle.NeedsRepair())
		require.Equal(t, []string{"feature"}, result.Worktrees[0].RootBranches)

		_ = s.Engine.UnregisterWorktree(s.Context, "feature")
	})

	t.Run("reports branch checked out outside its owning worktree", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()
		s.CreateBranch("feature").Commit("feature change")
		s.TrackBranch("feature", "main")
		s.Checkout("main")

		worktreeDir := t.TempDir()
		require.NoError(t, s.Engine.RegisterWorktree(context.Background(), engine.WorktreeRegistration{AnchorBranch: "feature", Path: engine.WorktreePath(worktreeDir), Name: "feature-wt"}))
		t.Cleanup(func() { _ = s.Engine.UnregisterWorktree(s.Context, "feature") })

		// Simulate raw `git checkout feature` from the main repository, bypassing
		// Stackit's checkout redirect.
		s.Checkout("feature")

		result, err := worktree.ListAction(s.Context, worktree.ListOptions{})
		require.NoError(t, err)
		require.Len(t, result.OwnershipWarnings, 1)
		assert.Contains(t, result.OwnershipWarnings[0], "branch feature belongs to managed worktree feature-wt")
		assert.Contains(t, result.OwnershipWarnings[0], "but is checked out at")
	})
}

func TestRemoveAction(t *testing.T) {
	t.Parallel()

	t.Run("fails when worktree not found", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		err := worktree.RemoveAction(s.Context, worktree.RemoveOptions{
			Selector: "nonexistent-stack",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "no worktree found")
	})

	t.Run("removes worktree registration", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		// Register a fake worktree (path doesn't exist)
		err := s.Engine.RegisterWorktree(context.Background(), engine.WorktreeRegistration{AnchorBranch: "feature-stack", Path: "/tmp/nonexistent-worktree"})
		require.NoError(t, err)

		// Verify it's registered
		wt, err := s.Engine.GetWorktreeForStack("feature-stack")
		require.NoError(t, err)
		require.NotNil(t, wt)

		// Remove it
		err = worktree.RemoveAction(s.Context, worktree.RemoveOptions{
			Selector: "feature-stack",
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

		err := s.Engine.RegisterWorktree(context.Background(), engine.WorktreeRegistration{AnchorBranch: "feature-stack", Path: "/tmp/nonexistent-worktree"})
		require.NoError(t, err)

		err = worktree.RemoveAction(s.Context, worktree.RemoveOptions{
			Selector: "feature-stack",
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
			Selector: "nonexistent-stack",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "no worktree found")
	})

	t.Run("fails when path does not exist", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		// Register a fake worktree with non-existent path
		err := s.Engine.RegisterWorktree(context.Background(), engine.WorktreeRegistration{AnchorBranch: "feature-stack", Path: "/tmp/nonexistent-path-12345"})
		require.NoError(t, err)

		_, err = worktree.OpenAction(s.Context, worktree.OpenOptions{
			Selector: "feature-stack",
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
		err := s.Engine.RegisterWorktree(context.Background(), engine.WorktreeRegistration{AnchorBranch: "feature-stack", Path: engine.WorktreePath(repoRoot)})
		require.NoError(t, err)

		path, err := worktree.OpenAction(s.Context, worktree.OpenOptions{
			Selector: "feature-stack",
		})
		require.NoError(t, err)
		canonicalRepoRoot, err := filepath.EvalSymlinks(repoRoot)
		require.NoError(t, err)
		canonicalPath, err := filepath.EvalSymlinks(path.String())
		require.NoError(t, err)
		require.Equal(t, canonicalRepoRoot, canonicalPath)

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
		_ = s.Engine.RemoveWorktree(s.Context.Context, result.Path.String())
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
		_ = s.Engine.RemoveWorktree(s.Context.Context, result.Path.String())
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
		_ = s.Engine.RemoveWorktree(s.Context.Context, result.Path.String())
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
		_ = s.Engine.RemoveWorktree(s.Context.Context, result.Path.String())
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
		require.NoError(t, s.Engine.RegisterWorktree(context.Background(), engine.WorktreeRegistration{AnchorBranch: "feature", Path: engine.WorktreePath(worktreeDir), Name: "legacy-wt"}))

		result, err := worktree.RepairAction(s.Context, worktree.RepairOptions{Selector: "legacy-wt"})
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
		canonicalWorktreeDir, err := filepath.EvalSymlinks(worktreeDir)
		require.NoError(t, err)
		canonicalRepairedPath, err := filepath.EvalSymlinks(repaired.Path.String())
		require.NoError(t, err)
		require.Equal(t, canonicalWorktreeDir, canonicalRepairedPath)
		require.True(t, s.Engine.GetBranch(result.Repaired[0].AnchorBranch).IsWorktreeAnchor())
		require.Equal(t, result.Repaired[0].AnchorBranch, s.Engine.GetBranch("feature").GetParent().GetName())

		_ = s.Engine.UnregisterWorktree(s.Context, result.Repaired[0].AnchorBranch)
	})
}
