package merge_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/internal/actions/merge"
	"stackit.dev/stackit/testhelpers"
	"stackit.dev/stackit/testhelpers/scenario"
)

func TestAction(t *testing.T) {
	t.Run("fails when not on a branch", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// Detach HEAD
		s.RunGit("checkout", "HEAD~0").Rebuild()

		err := merge.Action(s.Context, merge.Options{
			DryRun:   false,
			Confirm:  false,
			Strategy: merge.StrategyBottomUp,
		})
		require.Error(t, err)
		// Either error is acceptable as it indicates merge is not allowed in detached state
		errorMessage := err.Error()
		require.True(t,
			errorMessage == "not on a branch" ||
				errorMessage == "cannot merge from trunk. You must be on a branch that has a PR",
			"unexpected error message: %s", errorMessage)
	})

	t.Run("fails when on trunk", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// Make sure we're on main
		s.Checkout("main")

		// Verify we're on trunk
		require.True(t, s.Engine.CurrentBranch().IsTrunk())

		err := merge.Action(s.Context, merge.Options{
			DryRun:   false,
			Confirm:  false,
			Strategy: merge.StrategyBottomUp,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot merge from trunk")
	})

	t.Run("fails when branch is not tracked", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			CreateBranch("untracked")

		// Verify branch is not tracked
		require.False(t, s.Engine.GetBranch("untracked").IsTracked())

		err := merge.Action(s.Context, merge.Options{
			DryRun:   false,
			Confirm:  false,
			Strategy: merge.StrategyBottomUp,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "not tracked")
	})

	t.Run("returns early when no PRs to merge", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
			})

		// Switch to branch1
		s.Checkout("branch1")

		// Verify branch is tracked
		require.True(t, s.Engine.GetBranch("branch1").IsTracked())

		err := merge.Action(s.Context, merge.Options{
			DryRun:   false,
			Confirm:  false,
			Strategy: merge.StrategyBottomUp,
		})
		// Should fail because no PRs found
		require.Error(t, err)
		require.Contains(t, err.Error(), "no open PRs found")
	})

	t.Run("dry run mode reports PRs without merging", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
			})

		// Add PR info
		prNumber := 123
		prInfo := testhelpers.NewTestPrInfo(prNumber).
			WithURL("https://github.com/owner/repo/pull/123")
		branch1 := s.Engine.GetBranch("branch1")
		err := s.Engine.UpsertPrInfo(branch1, prInfo)
		require.NoError(t, err)

		// Switch to branch1
		s.Checkout("branch1")

		// Verify branch is tracked and has PR info
		require.True(t, branch1.IsTracked())
		prInfo, err = branch1.GetPrInfo()
		require.NoError(t, err)
		require.NotNil(t, prInfo)

		err = merge.Action(s.Context, merge.Options{
			DryRun:   true,
			Confirm:  false,
			Strategy: merge.StrategyBottomUp,
		})
		require.NoError(t, err)
	})

	t.Run("preserves stack structure when merging bottom PR", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch-a": "main",
				"branch-b": "branch-a",
				"branch-c": "branch-b",
			})

		// Set up mock GitHub server with PRs
		mockConfig := testhelpers.NewMockGitHubServerConfig()
		mockConfig.PRs["branch-a"] = testhelpers.NewSamplePullRequest(testhelpers.SamplePRData{
			Number:  101,
			Title:   "Branch A",
			Head:    "branch-a",
			Base:    "main",
			State:   "open",
			HTMLURL: "https://github.com/owner/repo/pull/101",
		})
		mockConfig.PRs["branch-b"] = testhelpers.NewSamplePullRequest(testhelpers.SamplePRData{
			Number:  102,
			Title:   "Branch B",
			Head:    "branch-b",
			Base:    "branch-a",
			State:   "open",
			HTMLURL: "https://github.com/owner/repo/pull/102",
		})
		mockConfig.PRs["branch-c"] = testhelpers.NewSamplePullRequest(testhelpers.SamplePRData{
			Number:  103,
			Title:   "Branch C",
			Head:    "branch-c",
			Base:    "branch-b",
			State:   "open",
			HTMLURL: "https://github.com/owner/repo/pull/103",
		})

		rawClient, owner, repo := testhelpers.NewMockGitHubClient(t, mockConfig)
		githubClient := testhelpers.NewMockGitHubClientInterface(rawClient, owner, repo, mockConfig)

		// Add PR info to engine
		prA := 101
		branchA := s.Engine.GetBranch("branch-a")
		branchB := s.Engine.GetBranch("branch-b")
		branchC := s.Engine.GetBranch("branch-c")
		err := s.Engine.UpsertPrInfo(branchA, testhelpers.NewTestPrInfo(prA).
			WithBase("main").
			WithURL("https://github.com/owner/repo/pull/101"))
		require.NoError(t, err)

		prB := 102
		err = s.Engine.UpsertPrInfo(branchB, testhelpers.NewTestPrInfo(prB).
			WithBase("branch-a").
			WithURL("https://github.com/owner/repo/pull/102"))
		require.NoError(t, err)

		prC := 103
		err = s.Engine.UpsertPrInfo(branchC, testhelpers.NewTestPrInfo(prC).
			WithBase("branch-b").
			WithURL("https://github.com/owner/repo/pull/103"))
		require.NoError(t, err)

		// Switch to branch-a (the bottom PR we'll merge)
		s.Checkout("branch-a")

		// Create context with GitHub client
		s.Context.GitHubClient = githubClient

		// Execute merge plan (merge branch-a)
		plan, validation, err := merge.CreateMergePlan(s.Context.Context, s.Engine, s.Context.Splog, s.Context.GitHubClient, merge.CreatePlanOptions{
			Strategy: merge.StrategyBottomUp,
			Force:    true,
		})
		require.NoError(t, err)
		require.NotNil(t, plan)
		require.True(t, validation.Valid)

		// Verify plan includes restacking upstack branches
		require.Contains(t, plan.UpstackBranches, "branch-b")
		require.Contains(t, plan.UpstackBranches, "branch-c")

		// Set up a remote so PullTrunk can work
		remoteDir := t.TempDir()

		s.RunGit("init", "--bare", remoteDir)

		// Add remote and push main
		s.RunGit("remote", "add", "origin", remoteDir).
			RunGit("push", "-u", "origin", "main").
			RunGit("push", "-u", "origin", "branch-a").
			RunGit("push", "-u", "origin", "branch-b").
			RunGit("push", "-u", "origin", "branch-c")

		// Now merge branch-a into main locally and push to simulate the PR merge
		s.Checkout("main").
			RunGit("merge", "branch-a", "--no-ff", "-m", "Merge branch-a").
			RunGit("push", "origin", "main")

		// Switch back to branch-a for the merge execution
		s.Checkout("branch-a")

		// Execute the merge plan
		err = merge.Execute(s.Context, s.Engine, merge.ExecuteOptions{
			Plan:  plan,
			Force: true,
		})
		require.NoError(t, err)

		// Verify PR base branches were updated correctly
		// branch-b should point to main (since branch-a was merged)
		updatedPRB, exists := mockConfig.UpdatedPRs[102]
		require.True(t, exists, "branch-b PR should have been updated")
		require.NotNil(t, updatedPRB.Base)
		require.NotNil(t, updatedPRB.Base.Ref)
		require.Equal(t, "main", *updatedPRB.Base.Ref, "branch-b PR base should be main after branch-a is merged")

		// branch-c should point to branch-b (not main - this is the bug fix!)
		updatedPRC, exists := mockConfig.UpdatedPRs[103]
		require.True(t, exists, "branch-c PR should have been updated")
		require.NotNil(t, updatedPRC.Base)
		require.NotNil(t, updatedPRC.Base.Ref)
		require.Equal(t, "branch-b", *updatedPRC.Base.Ref, "branch-c PR base should be branch-b (not main) to preserve stack structure")
	})
}

type mockHandler struct {
	merge.NullHandler
	completed bool
}

func (h *mockHandler) Complete(_ *merge.ConsolidationResult) {
	h.completed = true
}

func TestExecute_AlwaysCallsComplete(t *testing.T) {
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

	t.Run("calls Complete on success", func(t *testing.T) {
		handler := &mockHandler{}
		err := merge.Execute(s.Context, s.Engine, merge.ExecuteOptions{
			Plan:    &merge.Plan{Steps: []merge.PlanStep{}},
			Handler: handler,
		})
		require.NoError(t, err)
		require.True(t, handler.completed)
	})

	t.Run("calls Complete on failure", func(t *testing.T) {
		handler := &mockHandler{}
		err := merge.Execute(s.Context, s.Engine, merge.ExecuteOptions{
			Plan: &merge.Plan{Steps: []merge.PlanStep{
				{StepType: merge.StepMergePR, BranchName: "non-existent", Description: "test failure"},
			}},
			Handler: handler,
		})
		require.Error(t, err)
		require.True(t, handler.completed)
	})
}
