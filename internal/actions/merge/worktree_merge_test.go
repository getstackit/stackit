package merge_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/internal/actions/merge"
	"stackit.dev/stackit/testhelpers"
	"stackit.dev/stackit/testhelpers/scenario"
)

func TestExecuteInWorktree(t *testing.T) {
	t.Run("successfully merges in worktree", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
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
		err := s.Engine.UpsertPrInfo(branchA, testhelpers.NewTestPrInfo(prA).
			WithBase("main").
			WithURL("https://github.com/owner/repo/pull/101"))
		require.NoError(t, err)

		// Create a remote
		remoteDir := t.TempDir()
		s.RunGit("init", "--bare", remoteDir)
		s.RunGit("remote", "add", "origin", remoteDir).
			RunGit("push", "-u", "origin", "main").
			RunGit("push", "-u", "origin", "branch-a")

		// Create merge plan
		s.Checkout("branch-a")
		s.Context.GitHubClient = githubClient

		plan, validation, err := merge.CreateMergePlan(s.Context.Context, s.Engine, s.Context.Splog, s.Context.GitHubClient, merge.CreatePlanOptions{
			Strategy: merge.StrategyBottomUp,
			Force:    true,
		})
		require.NoError(t, err)
		require.True(t, validation.Valid)

		// Now merge branch-a into main locally and push to simulate the PR merge
		s.Checkout("main").
			RunGit("merge", "branch-a", "--no-ff", "-m", "Merge branch-a").
			RunGit("push", "origin", "main")

		// Switch back to branch-a
		s.Checkout("branch-a")

		// Execute in worktree
		err = merge.ExecuteInWorktree(s.Context, s.Engine, merge.ExecuteOptions{
			Plan:  plan,
			Force: true,
		}, "", "")
		require.NoError(t, err)

		// Verify we have switched to main since branch-a was merged and deleted
		currentBranch := s.Engine.CurrentBranch()
		require.NotNil(t, currentBranch)
		require.Equal(t, "main", currentBranch.GetName())
	})
}
