package merge_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions/merge"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestExecuteInWorktree(t *testing.T) {
	t.Run("successfully merges in worktree", func(t *testing.T) {
		s := scenario.NewRemoteScenario(t).
			WithStack(map[string]string{
				"branch-a": "main",
			})

		// Set up mock GitHub server with PR
		mockConfig := testhelpers.NewMockGitHubServerConfig()
		mockConfig.PRs["branch-a"] = testhelpers.NewSamplePullRequest(testhelpers.SamplePRData{
			Number:  101,
			Title:   "Branch A",
			Head:    "branch-a",
			Base:    "main",
			State:   "open",
			HTMLURL: "https://github.com/owner/repo/pull/101",
		})

		rawClient, owner, repo := testhelpers.NewMockGitHubClient(t, mockConfig)
		githubClient := testhelpers.NewMockGitHubClientInterface(rawClient, owner, repo, mockConfig)

		// Add PR info to engine
		prA := 101
		branchA := s.Engine.GetBranch("branch-a")
		err := s.Engine.UpsertPrInfo(context.Background(), branchA, testhelpers.NewTestPrInfo(prA).
			WithBase("main").
			WithURL("https://github.com/owner/repo/pull/101"))
		require.NoError(t, err)

		// Create a remote
		s.RunGit("push", "-u", "origin", "branch-a")

		// Create merge plan
		s.Checkout("branch-a")
		s.Context.GitHubClient = githubClient

		plan, validation, err := merge.CreateMergePlan(s.Context.Context, s.Engine, s.Context.Output, s.Context.GitHubClient, merge.CreatePlanOptions{
			Strategy: merge.StrategyBottomUp,
			Force:    true,
		})
		require.NoError(t, err)
		require.True(t, validation.Valid)

		// Now merge branch-a into main locally and push to simulate the PR merge
		s.CheckoutQuiet("main").
			RunGit("merge", "branch-a", "--no-ff", "-m", "Merge branch-a").
			RunGit("push", "origin", "main")

		// Switch to main in the main workspace so branch-a can be deleted in the worktree
		s.Checkout("main")

		// Execute in worktree
		err = merge.ExecuteInWorktree(s.Context, s.Engine, merge.ExecuteOptions{
			Plan:  plan,
			Force: true,
		}, "", "")
		require.NoError(t, err)

		// Rebuild the engine to pick up changes from the worktree
		s.Rebuild()

		// Verify the merge branch is gone from the branch list
		allBranches := s.Engine.AllBranches()
		branchNames := make([]string, len(allBranches))
		for i, b := range allBranches {
			branchNames[i] = b.GetName()
		}
		require.NotContains(t, branchNames, "branch-a")

		// Verify your main workspace remains on main
		currentBranch := s.Engine.CurrentBranch()
		require.NotNil(t, currentBranch)
		require.Equal(t, "main", currentBranch.GetName())
	})

	t.Run("deletes branch that is checked out in a separate worktree", func(t *testing.T) {
		// This test verifies that when a branch is checked out in a worktree,
		// the merge execution properly removes the worktree before deleting the branch.
		// Previously, the deletion would fail silently because git refuses to delete
		// a branch that is checked out in any worktree.
		s := scenario.NewRemoteScenario(t).
			WithStack(map[string]string{
				"branch-a": "main",
			})

		// Set up mock GitHub server with PR
		mockConfig := testhelpers.NewMockGitHubServerConfig()
		mockConfig.PRs["branch-a"] = testhelpers.NewSamplePullRequest(testhelpers.SamplePRData{
			Number:  101,
			Title:   "Branch A",
			Head:    "branch-a",
			Base:    "main",
			State:   "open",
			HTMLURL: "https://github.com/owner/repo/pull/101",
		})

		rawClient, owner, repo := testhelpers.NewMockGitHubClient(t, mockConfig)
		githubClient := testhelpers.NewMockGitHubClientInterface(rawClient, owner, repo, mockConfig)

		// Add PR info to engine
		prA := 101
		branchA := s.Engine.GetBranch("branch-a")
		err := s.Engine.UpsertPrInfo(context.Background(), branchA, testhelpers.NewTestPrInfo(prA).
			WithBase("main").
			WithURL("https://github.com/owner/repo/pull/101"))
		require.NoError(t, err)

		// Create a remote
		s.RunGit("push", "-u", "origin", "branch-a")

		// Create merge plan while on branch-a
		s.Checkout("branch-a")
		s.Context.GitHubClient = githubClient

		plan, validation, err := merge.CreateMergePlan(s.Context.Context, s.Engine, s.Context.Output, s.Context.GitHubClient, merge.CreatePlanOptions{
			Strategy: merge.StrategyBottomUp,
			Force:    true,
		})
		require.NoError(t, err)
		require.True(t, validation.Valid)

		// Simulate the PR merge by merging branch-a into main locally and pushing
		s.Checkout("main")
		s.RunGit("merge", "branch-a", "--no-ff", "-m", "Merge branch-a").
			RunGit("push", "origin", "main")

		// Create a separate worktree with branch-a checked out
		// This simulates the scenario where the user has branch-a open in another worktree
		worktreePath := filepath.Join(t.TempDir(), "branch-a-worktree")
		s.RunGit("worktree", "add", worktreePath, "branch-a")

		// Verify the worktree was created with branch-a
		// Note: resolve symlinks because on macOS /var -> /private/var
		worktreeForBranch, err := s.Engine.Git().GetWorktreePathForBranch(s.Context.Context, "branch-a")
		require.NoError(t, err)
		resolvedWorktreePath, _ := filepath.EvalSymlinks(worktreePath)
		resolvedWorktreeForBranch, _ := filepath.EvalSymlinks(worktreeForBranch)
		require.Equal(t, resolvedWorktreePath, resolvedWorktreeForBranch, "branch-a should be checked out in the worktree")

		// Execute the merge plan - this should delete branch-a even though it's in a worktree
		err = merge.ExecuteInWorktree(s.Context, s.Engine, merge.ExecuteOptions{
			Plan:  plan,
			Force: true,
		}, "", "")
		require.NoError(t, err)

		// Rebuild the engine to pick up changes
		s.Rebuild()

		// Verify the merged branch is gone from the branch list
		allBranches := s.Engine.AllBranches()
		branchNames := make([]string, len(allBranches))
		for i, b := range allBranches {
			branchNames[i] = b.GetName()
		}
		require.NotContains(t, branchNames, "branch-a", "branch-a should be deleted after merge")

		// Verify the worktree was removed (branch-a is no longer in any worktree)
		worktreeForBranch, err = s.Engine.Git().GetWorktreePathForBranch(s.Context.Context, "branch-a")
		require.NoError(t, err)
		require.Empty(t, worktreeForBranch, "worktree for branch-a should be removed")
	})
}
