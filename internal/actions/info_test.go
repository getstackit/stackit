package actions

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

// TestSingleBranchInfoFilesChangedUsesDivergenceBase verifies the single-branch
// info path counts changed files against the branch's divergence point — the
// same base as its additions/deletions — not the parent's current tip, so the
// count stays stable when the parent advances without a restack.
func TestSingleBranchInfoFilesChangedUsesDivergenceBase(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
	s.WithLinearStack3() // main -> a -> b -> c

	before := singleBranchFilesChanged(t, s, "b")
	require.GreaterOrEqual(t, before, 1)

	// Advance b's parent (a) with a commit touching a different file, without
	// restacking b, so a's current tip moves past b's divergence point.
	s.Checkout("a").CommitChange("a-advanced.txt", "advance a past b's divergence")

	after := singleBranchFilesChanged(t, s, "b")
	require.Equal(t, before, after,
		"single-branch files-changed must use the divergence base, not the parent's current tip")
}

func singleBranchFilesChanged(t *testing.T, s *scenario.Scenario, name string) int {
	t.Helper()
	info, err := QueryBranchInfo(context.Background(), s.Engine, BranchInfoQueryOptions{
		BranchName: name,
	}, nil)
	require.NoError(t, err)
	return info.DiffStats.FilesChanged
}

func TestQueryBranchInfo(t *testing.T) {
	t.Run("returns structured branch info for a tracked branch", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.InitialCommitSceneSetup)

		require.NoError(t, s.Scene.Repo.CreateAndCheckoutBranch("feature"))
		require.NoError(t, s.Scene.Repo.CreateChange("feature change", "feature.txt", false))
		require.NoError(t, s.Scene.Repo.RunGitCommand("add", "."))
		require.NoError(t, s.Scene.Repo.RunGitCommand("commit", "-m", "feature change"))
		require.NoError(t, s.Scene.Repo.CreateAndCheckoutBranch("child"))
		require.NoError(t, s.Scene.Repo.CreateChange("child change", "child.txt", false))
		require.NoError(t, s.Scene.Repo.RunGitCommand("add", "."))
		require.NoError(t, s.Scene.Repo.RunGitCommand("commit", "-m", "child change"))
		s.Rebuild()

		require.NoError(t, s.Engine.TrackBranch(context.Background(), "feature", "main"))
		require.NoError(t, s.Engine.TrackBranch(context.Background(), "child", "feature"))
		require.NoError(t, s.Engine.SetScope(context.Background(), s.Engine.GetBranch("feature"), engine.NewScope("PROJ-123")))
		s.Checkout("feature")

		info, err := QueryBranchInfo(context.Background(), s.Engine, BranchInfoQueryOptions{
			BranchName: "feature",
		}, nil)
		require.NoError(t, err)

		require.Equal(t, "feature", info.Name)
		require.True(t, info.IsCurrent)
		require.False(t, info.IsTrunk)
		require.Equal(t, "PROJ-123", info.Scope)
		require.Equal(t, "main", info.Parent)
		require.Contains(t, info.Children, "child")
		require.Contains(t, strings.Join(info.CommitMessages, "\n"), "feature change")
		require.GreaterOrEqual(t, info.DiffStats.FilesChanged, 1)
		require.NotEmpty(t, info.CommitDate)
	})

	t.Run("includes diff and patch output when requested", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.InitialCommitSceneSetup)

		require.NoError(t, s.Scene.Repo.CreateAndCheckoutBranch("feature"))
		require.NoError(t, s.Scene.Repo.CreateChange("feature change", "feature.txt", false))
		require.NoError(t, s.Scene.Repo.RunGitCommand("add", "."))
		require.NoError(t, s.Scene.Repo.RunGitCommand("commit", "-m", "feature change"))
		s.Rebuild()
		require.NoError(t, s.Engine.TrackBranch(context.Background(), "feature", "main"))

		info, err := QueryBranchInfo(context.Background(), s.Engine, BranchInfoQueryOptions{
			BranchName: "feature",
			Patch:      true,
		}, nil)
		require.NoError(t, err)
		require.NotEmpty(t, info.PatchOutput)
		require.Empty(t, info.DiffOutput)
	})

	t.Run("errors for a missing branch", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.InitialCommitSceneSetup)

		_, err := QueryBranchInfo(context.Background(), s.Engine, BranchInfoQueryOptions{
			BranchName: "missing",
		}, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "does not exist")
	})
}
