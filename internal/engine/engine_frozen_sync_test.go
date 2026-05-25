package engine_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestRestackFrozenBranch(t *testing.T) {
	t.Run("hard resets frozen branch to remote instead of rebase", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// 1. Create a parent branch and a child branch
		s.CreateBranch("parent").
			Commit("parent change").
			CreateBranch("child").
			Commit("child change")

		// 2. Track them
		require.NoError(t, s.Engine.TrackBranch(context.Background(), "parent", "main"))
		require.NoError(t, s.Engine.TrackBranch(context.Background(), "child", "parent"))

		// 3. Freeze the child
		childBranch := s.Engine.GetBranch("child")
		_, err := s.Engine.SetFrozen(context.Background(), engine.BranchesOf(childBranch), true)
		require.NoError(t, err)

		// 4. Simulate remote state: child has a different SHA on remote
		s.CreateBranch("temp").
			Commit("remote change")
		remoteSha, err := s.Engine.GetBranch("temp").GetRevision()
		require.NoError(t, err)

		// Create the remote ref manually as a local branch named 'origin/child'
		// so resolveRefHashInternal can find it when looking for 'origin/child'.
		s.RunGit("update-ref", "refs/heads/origin/child", remoteSha)

		// 5. Restack the branch
		batchRes, err := s.Engine.RestackBranches(context.Background(), engine.BranchesOf(childBranch))
		require.NoError(t, err)
		require.Equal(t, engine.RestackDone, batchRes.Results["child"].Result)

		// Rebuild the engine to refresh its internal cache
		require.NoError(t, s.Engine.Rebuild("main"))

		// 6. Verify it was hard reset to remoteSha
		newLocalSha, err := s.Scene.Repo.GetBranchSHA("child")
		require.NoError(t, err)
		require.Equal(t, remoteSha, newLocalSha)
	})

	t.Run("skips restack if frozen branch matches remote", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		s.CreateBranch("parent").
			Commit("parent change").
			CreateBranch("child").
			Commit("child change")

		require.NoError(t, s.Engine.TrackBranch(context.Background(), "parent", "main"))
		require.NoError(t, s.Engine.TrackBranch(context.Background(), "child", "parent"))

		childBranch := s.Engine.GetBranch("child")
		_, err := s.Engine.SetFrozen(context.Background(), engine.BranchesOf(childBranch), true)
		require.NoError(t, err)

		localSha, _ := childBranch.GetRevision()
		s.RunGit("update-ref", "refs/remotes/origin/child", localSha)

		batchRes, err := s.Engine.RestackBranches(context.Background(), engine.BranchesOf(childBranch))
		require.NoError(t, err)
		require.Equal(t, engine.RestackUnneeded, batchRes.Results["child"].Result)
	})
}
