package actions

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestEnsureCanModifyHere(t *testing.T) {
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
	s.WithInitialCommit()
	s.CreateBranch("feature").Commit("feature change")
	s.TrackBranch("feature", "main")
	worktreePath := t.TempDir()
	require.NoError(t, s.Engine.RegisterWorktree(context.Background(), engine.WorktreeRegistration{AnchorBranch: "feature", Path: engine.WorktreePath(worktreePath), Name: "feature-wt"}))
	t.Cleanup(func() { _ = s.Engine.UnregisterWorktree(s.Context, "feature") })

	t.Run("refuses managed stack mutation from main repository", func(t *testing.T) {
		err := EnsureCanModifyHere(s.Context, s.Engine.GetBranch("feature"))
		require.Error(t, err)
		assert.ErrorContains(t, err, "belongs to worktree feature-wt")
		assert.ErrorContains(t, err, "cd "+worktreePath)
	})

	// Telling someone to cd into a directory that no longer exists strands
	// them: the branch is owned by a path they cannot go to, and only detaching
	// the registration releases it.
	t.Run("points at detach when the worktree directory is gone", func(t *testing.T) {
		gone := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		gone.WithInitialCommit()
		gone.CreateBranch("feature").Commit("feature change")
		gone.TrackBranch("feature", "main")
		missing := filepath.Join(t.TempDir(), "deleted-worktree")
		require.NoError(t, gone.Engine.RegisterWorktree(context.Background(), engine.WorktreeRegistration{AnchorBranch: "feature", Path: engine.WorktreePath(missing), Name: "feature-wt"}))

		err := EnsureCanModifyHere(gone.Context, gone.Engine.GetBranch("feature"))
		require.Error(t, err)
		assert.ErrorContains(t, err, "no longer exists")
		assert.ErrorContains(t, err, "worktree detach feature-wt")
		assert.NotContains(t, err.Error(), "cd "+missing)
	})

	t.Run("allows mutation from owning worktree", func(t *testing.T) {
		ctx := *s.Context
		ctx.InManagedWorktree = true
		ctx.WorktreeInfo = &engine.WorktreeInfo{
			Name:         "feature-wt",
			Path:         engine.WorktreePath(worktreePath),
			AnchorBranch: "feature",
		}
		require.NoError(t, EnsureCanModifyHere(&ctx, s.Engine.GetBranch("feature")))
	})

	t.Run("refuses main stack mutation from a managed worktree", func(t *testing.T) {
		ctx := *s.Context
		ctx.InManagedWorktree = true
		ctx.WorktreeInfo = &engine.WorktreeInfo{
			Name:         "feature-wt",
			Path:         engine.WorktreePath(worktreePath),
			AnchorBranch: "feature",
			MainRepoDir:  s.Context.RepoRoot,
		}
		err := EnsureCanModifyHere(&ctx, s.Engine.Trunk())
		require.Error(t, err)
		assert.ErrorContains(t, err, "belongs to the main repository stack")
	})
}
