package actions

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestQueryStackInfo(t *testing.T) {
	t.Parallel()
	t.Run("returns structured info for the current stack", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
			})

		// Add some commits to have interesting info
		s.Checkout("branch1")
		s.Scene.Repo.CreateChangeAndCommit("c1", "f1.txt")
		s.Checkout("branch2")
		s.Scene.Repo.CreateChangeAndCommit("c2", "f2.txt")

		result, err := QueryStackInfo(context.Background(), s.Engine)
		require.NoError(t, err)
		require.Len(t, result.Branches, 2)

		branchMap := make(map[string]StackInfoBranch)
		for _, b := range result.Branches {
			branchMap[b.Name] = b
		}
		require.Contains(t, branchMap, "branch1")
		require.Contains(t, branchMap, "branch2")

		b1 := branchMap["branch1"]
		require.Equal(t, "main", b1.Parent)
		require.NotEmpty(t, b1.CommitMessages)
		require.GreaterOrEqual(t, b1.DiffStats.FilesChanged, 1)

		b2 := branchMap["branch2"]
		require.Equal(t, "branch1", b2.Parent)
		require.NotEmpty(t, b2.CommitMessages)
		require.GreaterOrEqual(t, b2.DiffStats.FilesChanged, 1)

		// Tree structure: Order holds the stacked (non-trunk) branches; trunk is
		// referenced via Trunk/Parents and is always fixed.
		require.Equal(t, []string{"branch1", "branch2"}, result.Order)
		require.Equal(t, "main", result.Trunk)
		require.Equal(t, "main", result.Parents["branch1"])
		require.True(t, result.UpToDate["main"])
	})

	t.Run("fails when not on a branch", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.RunGit("checkout", "--detach", "main")

		_, err := QueryStackInfo(context.Background(), s.Engine)
		require.Error(t, err)
		require.Contains(t, err.Error(), "not on a branch")
	})

	t.Run("includes lock and frozen state", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
			})
		s.Checkout("branch1")

		branch1 := s.Engine.GetBranch("branch1")
		_, err := s.Engine.SetLocked(context.Background(), engine.BranchesOf(branch1), engine.LockReasonUser)
		require.NoError(t, err)
		_, err = s.Engine.SetFrozen(context.Background(), engine.BranchesOf(branch1), true)
		require.NoError(t, err)

		result, err := QueryStackInfo(context.Background(), s.Engine)
		require.NoError(t, err)
		require.Len(t, result.Branches, 1)
		require.True(t, result.Branches[0].IsLocked)
		require.True(t, result.Branches[0].IsFrozen)
	})
}
