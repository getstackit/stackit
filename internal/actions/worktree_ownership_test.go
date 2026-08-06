package actions

import (
	"context"
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
	require.NoError(t, s.Engine.RegisterWorktreeWithName(context.Background(), "feature", "/tmp/feature-worktree", "feature-wt"))
	t.Cleanup(func() { _ = s.Engine.UnregisterWorktree(s.Context, "feature") })

	t.Run("refuses managed stack mutation from main repository", func(t *testing.T) {
		err := EnsureCanModifyHere(s.Context, s.Engine.GetBranch("feature"))
		require.Error(t, err)
		assert.ErrorContains(t, err, "belongs to worktree feature-wt")
		assert.ErrorContains(t, err, "cd /tmp/feature-worktree")
	})

	t.Run("allows mutation from owning worktree", func(t *testing.T) {
		ctx := *s.Context
		ctx.InManagedWorktree = true
		ctx.WorktreeInfo = &engine.WorktreeInfo{
			Name:         "feature-wt",
			Path:         "/tmp/feature-worktree",
			AnchorBranch: "feature",
		}
		require.NoError(t, EnsureCanModifyHere(&ctx, s.Engine.GetBranch("feature")))
	})

	t.Run("refuses main stack mutation from a managed worktree", func(t *testing.T) {
		ctx := *s.Context
		ctx.InManagedWorktree = true
		ctx.WorktreeInfo = &engine.WorktreeInfo{
			Name:         "feature-wt",
			Path:         "/tmp/feature-worktree",
			AnchorBranch: "feature",
			MainRepoDir:  s.Context.RepoRoot,
		}
		err := EnsureCanModifyHere(&ctx, s.Engine.Trunk())
		require.Error(t, err)
		assert.ErrorContains(t, err, "belongs to the main repository stack")
	})
}
