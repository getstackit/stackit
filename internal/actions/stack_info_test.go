package actions

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/testhelpers"
	"stackit.dev/stackit/testhelpers/scenario"
)

func TestStackInfoAction(t *testing.T) {
	t.Run("returns JSON info for the current stack", func(t *testing.T) {
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

		err := StackInfoAction(s.Context, StackInfoOptions{JSON: true})
		output := s.Output.String()

		require.NoError(t, err)
		require.NotEmpty(t, output)

		var info []StackBranchInfo
		err = json.Unmarshal([]byte(output), &info)
		require.NoError(t, err)

		// Check branches
		require.Len(t, info, 2)

		// Map by name for easier checking
		branchMap := make(map[string]StackBranchInfo)
		for _, b := range info {
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
	})

	t.Run("fails when not on a branch", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		// Detach HEAD
		s.RunGit("checkout", "--detach", "main")

		err := StackInfoAction(s.Context, StackInfoOptions{JSON: true})
		require.Error(t, err)
		require.Contains(t, err.Error(), "not on a branch")
	})

	t.Run("returns tree view with commit messages when JSON flag is false", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
			})
		s.Checkout("branch1")

		err := StackInfoAction(s.Context, StackInfoOptions{JSON: false})
		output := s.Output.String()

		require.NoError(t, err)
		// Tree format includes branch name with symbol
		require.Contains(t, output, "branch1")
		// Tree format shows trunk at bottom
		require.Contains(t, output, "main")
		// Tree format shows commit messages
		require.Contains(t, output, "change on branch1")
	})

	t.Run("includes lock and frozen state", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
			})
		s.Checkout("branch1")

		branch1 := s.Engine.GetBranch("branch1")
		_, err := s.Engine.SetLocked(context.Background(), []engine.Branch{branch1}, engine.LockReasonUser)
		require.NoError(t, err)
		_, err = s.Engine.SetFrozen(context.Background(), []engine.Branch{branch1}, true)
		require.NoError(t, err)

		err = StackInfoAction(s.Context, StackInfoOptions{JSON: true})
		output := s.Output.String()

		require.NoError(t, err)

		var info []StackBranchInfo
		err = json.Unmarshal([]byte(output), &info)
		require.NoError(t, err)
		require.Len(t, info, 1)
		require.True(t, info[0].IsLocked)
		require.True(t, info[0].IsFrozen)
	})
}
