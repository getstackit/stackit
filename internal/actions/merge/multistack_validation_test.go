package merge

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/testhelpers/scenario"
)

func TestValidateBranchesMatchRemote(t *testing.T) {
	t.Run("passes when all branches match remote", func(t *testing.T) {
		s := scenario.NewRemoteScenario(t)

		// Create a branch and push it
		s.CreateBranchQuiet("feature1").
			TrackBranch("feature1", s.Engine.Trunk().GetName())
		require.NoError(t, s.Scene.Repo.CreateChangeAndCommit("feature1-content", "file1"))
		require.NoError(t, s.Scene.Repo.PushBranch("origin", "feature1"))
		s.Rebuild()

		// Populate remote SHAs so GetBranchRemoteStatus works
		require.NoError(t, s.Engine.PopulateRemoteShas())

		stacks := []MultiStackInfo{
			{RootBranch: "feature1", AllBranches: []string{"feature1"}},
		}

		// Should pass - branch matches remote
		err := validateBranchesMatchRemote(s.Engine, stacks, s.Context.Output)
		assert.NoError(t, err)
	})

	t.Run("fails when branch differs from remote", func(t *testing.T) {
		s := scenario.NewRemoteScenario(t)

		// Create a branch and push it
		s.CreateBranchQuiet("feature1").
			TrackBranch("feature1", s.Engine.Trunk().GetName())
		require.NoError(t, s.Scene.Repo.CreateChangeAndCommit("feature1-content", "file1"))
		require.NoError(t, s.Scene.Repo.PushBranch("origin", "feature1"))
		s.Rebuild()

		// Make a local change (branch now differs from remote)
		require.NoError(t, s.Scene.Repo.CreateChangeAndCommit("local-only-change", "file2"))
		s.Rebuild()

		// Populate remote SHAs
		require.NoError(t, s.Engine.PopulateRemoteShas())

		stacks := []MultiStackInfo{
			{RootBranch: "feature1", AllBranches: []string{"feature1"}},
		}

		// Should fail - branch differs from remote
		err := validateBranchesMatchRemote(s.Engine, stacks, s.Context.Output)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot ship")
		assert.Contains(t, err.Error(), "differ from remote")
		assert.Contains(t, err.Error(), "1 branch")
	})

	t.Run("fails with multiple mismatched branches", func(t *testing.T) {
		s := scenario.NewRemoteScenario(t)

		// Create first branch and push
		s.CreateBranchQuiet("feature1").
			TrackBranch("feature1", s.Engine.Trunk().GetName())
		require.NoError(t, s.Scene.Repo.CreateChangeAndCommit("feature1-content", "file1"))
		require.NoError(t, s.Scene.Repo.PushBranch("origin", "feature1"))
		s.Rebuild()

		// Create second branch and push
		s.CreateBranchQuiet("feature2").
			TrackBranch("feature2", "feature1")
		require.NoError(t, s.Scene.Repo.CreateChangeAndCommit("feature2-content", "file2"))
		require.NoError(t, s.Scene.Repo.PushBranch("origin", "feature2"))
		s.Rebuild()

		// Make local changes to both branches
		s.CheckoutQuiet("feature1")
		require.NoError(t, s.Scene.Repo.CreateChangeAndCommit("local-change-1", "file3"))

		s.CheckoutQuiet("feature2")
		require.NoError(t, s.Scene.Repo.CreateChangeAndCommit("local-change-2", "file4"))
		s.Rebuild()

		// Populate remote SHAs
		require.NoError(t, s.Engine.PopulateRemoteShas())

		stacks := []MultiStackInfo{
			{RootBranch: "feature1", AllBranches: []string{"feature1", "feature2"}},
		}

		// Should fail with count of 2
		err := validateBranchesMatchRemote(s.Engine, stacks, s.Context.Output)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "2 branch")
	})

	t.Run("passes with multiple stacks all matching remote", func(t *testing.T) {
		s := scenario.NewRemoteScenario(t)

		// Create stack1
		s.CreateBranchQuiet("stack1-b1").
			TrackBranch("stack1-b1", s.Engine.Trunk().GetName())
		require.NoError(t, s.Scene.Repo.CreateChangeAndCommit("s1-content", "s1file"))
		require.NoError(t, s.Scene.Repo.PushBranch("origin", "stack1-b1"))
		s.Rebuild().
			CheckoutQuiet(s.Engine.Trunk().GetName())

		// Create stack2
		s.CreateBranchQuiet("stack2-b1").
			TrackBranch("stack2-b1", s.Engine.Trunk().GetName())
		require.NoError(t, s.Scene.Repo.CreateChangeAndCommit("s2-content", "s2file"))
		require.NoError(t, s.Scene.Repo.PushBranch("origin", "stack2-b1"))
		s.Rebuild()

		// Populate remote SHAs
		require.NoError(t, s.Engine.PopulateRemoteShas())

		stacks := []MultiStackInfo{
			{RootBranch: "stack1-b1", AllBranches: []string{"stack1-b1"}},
			{RootBranch: "stack2-b1", AllBranches: []string{"stack2-b1"}},
		}

		// Should pass - all branches match remote
		err := validateBranchesMatchRemote(s.Engine, stacks, s.Context.Output)
		assert.NoError(t, err)
	})

	t.Run("fails when one stack has mismatched branch", func(t *testing.T) {
		s := scenario.NewRemoteScenario(t)

		// Create stack1 - will match remote
		s.CreateBranchQuiet("stack1-b1").
			TrackBranch("stack1-b1", s.Engine.Trunk().GetName())
		require.NoError(t, s.Scene.Repo.CreateChangeAndCommit("s1-content", "s1file"))
		require.NoError(t, s.Scene.Repo.PushBranch("origin", "stack1-b1"))
		s.Rebuild().
			CheckoutQuiet(s.Engine.Trunk().GetName())

		// Create stack2 - will differ from remote
		s.CreateBranchQuiet("stack2-b1").
			TrackBranch("stack2-b1", s.Engine.Trunk().GetName())
		require.NoError(t, s.Scene.Repo.CreateChangeAndCommit("s2-content", "s2file"))
		require.NoError(t, s.Scene.Repo.PushBranch("origin", "stack2-b1"))
		s.Rebuild()

		// Make local change to stack2
		require.NoError(t, s.Scene.Repo.CreateChangeAndCommit("local-change", "s2file2"))
		s.Rebuild()

		// Populate remote SHAs
		require.NoError(t, s.Engine.PopulateRemoteShas())

		stacks := []MultiStackInfo{
			{RootBranch: "stack1-b1", AllBranches: []string{"stack1-b1"}},
			{RootBranch: "stack2-b1", AllBranches: []string{"stack2-b1"}},
		}

		// Should fail - stack2 branch differs from remote
		err := validateBranchesMatchRemote(s.Engine, stacks, s.Context.Output)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "1 branch")
	})

	t.Run("handles branch not on remote gracefully", func(t *testing.T) {
		s := scenario.NewRemoteScenario(t)

		// Create a branch but don't push it
		s.CreateBranchQuiet("feature1").
			TrackBranch("feature1", s.Engine.Trunk().GetName())
		require.NoError(t, s.Scene.Repo.CreateChangeAndCommit("feature1-content", "file1"))
		s.Rebuild()

		// Populate remote SHAs
		require.NoError(t, s.Engine.PopulateRemoteShas())

		stacks := []MultiStackInfo{
			{RootBranch: "feature1", AllBranches: []string{"feature1"}},
		}

		// Should fail - branch doesn't exist on remote (doesn't match)
		err := validateBranchesMatchRemote(s.Engine, stacks, s.Context.Output)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "differ from remote")
	})
}
