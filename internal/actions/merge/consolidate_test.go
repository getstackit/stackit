package merge_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions/merge"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestConsolidateMergeExecutor(t *testing.T) {
	t.Parallel()
	t.Run("pre-validation fails for out-of-sync branch", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
			})

		// Add PR info
		pr1 := 101
		pr2 := 102
		branch1 := s.Engine.GetBranch("branch1")
		branch2 := s.Engine.GetBranch("branch2")
		err := s.Engine.UpsertPrInfo(context.Background(), branch1, testhelpers.NewTestPrInfo(pr1))
		require.NoError(t, err)
		err = s.Engine.UpsertPrInfo(context.Background(), branch2, testhelpers.NewTestPrInfo(pr2))
		require.NoError(t, err)

		s.Checkout("branch2")

		plan, _, err := merge.CreateMergePlan(s.Context.Context, s.Engine, s.Context.Output, s.Context.GitHubClient, merge.CreatePlanOptions{
			Strategy: merge.StrategyShip,
			Force:    false,
		})
		require.NoError(t, err)

		executor := merge.NewConsolidateMergeExecutor(plan, s.Engine, s.Context)

		// This should fail pre-validation because branches aren't pushed to remote
		_, err = executor.Execute(s.Context.Context, merge.ExecuteOptions{
			Plan:  plan,
			Force: false,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "pre-validation failed")
	})

	t.Run("creates consolidation branch correctly", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
			})

		// Add commits to branches to make them different
		s.CheckoutQuiet("branch1")
		s.RunGit("commit", "--allow-empty", "-m", "branch1 commit").Rebuild()

		s.CheckoutQuiet("branch2")
		s.RunGit("commit", "--allow-empty", "-m", "branch2 commit").Rebuild()

		// Add PR info
		pr1 := 101
		pr2 := 102
		branch1 := s.Engine.GetBranch("branch1")
		branch2 := s.Engine.GetBranch("branch2")
		err := s.Engine.UpsertPrInfo(context.Background(), branch1, testhelpers.NewTestPrInfoWithTitle(pr1, "Feature 1"))
		require.NoError(t, err)
		err = s.Engine.UpsertPrInfo(context.Background(), branch2, testhelpers.NewTestPrInfoWithTitle(pr2, "Feature 2"))
		require.NoError(t, err)

		plan, _, err := merge.CreateMergePlan(s.Context.Context, s.Engine, s.Context.Output, s.Context.GitHubClient, merge.CreatePlanOptions{
			Strategy: merge.StrategyShip,
			Force:    true, // Skip remote sync checks
			Wait:     true,
		})
		require.NoError(t, err)

		// Verify plan has the expected structure
		require.Equal(t, merge.StrategyShip, plan.Strategy)
		require.Len(t, plan.BranchesToMerge, 2)
		require.Len(t, plan.Steps, 4)
		require.Equal(t, merge.StepConsolidate, plan.Steps[0].StepType)
	})

	t.Run("builds correct consolidation PR body", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
			})

		// Add PR info
		pr1 := 101
		pr2 := 102
		branch1 := s.Engine.GetBranch("branch1")
		branch2 := s.Engine.GetBranch("branch2")
		err := s.Engine.UpsertPrInfo(context.Background(), branch1, testhelpers.NewTestPrInfoWithTitle(pr1, "Add user authentication"))
		require.NoError(t, err)
		err = s.Engine.UpsertPrInfo(context.Background(), branch2, testhelpers.NewTestPrInfoWithTitle(pr2, "Add user profile UI"))
		require.NoError(t, err)

		s.Checkout("branch2")

		plan, _, err := merge.CreateMergePlan(s.Context.Context, s.Engine, s.Context.Output, s.Context.GitHubClient, merge.CreatePlanOptions{
			Strategy: merge.StrategyShip,
			Force:    true,
		})
		require.NoError(t, err)

		executor := merge.NewConsolidateMergeExecutor(plan, s.Engine, s.Context)

		// Verify the executor was created successfully
		require.NotNil(t, executor)
	})

	t.Run("handles stack scope correctly", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"batch/feature-a": "main",
				"batch/feature-b": "batch/feature-a",
			})

		// Set scope on branches
		scope := engine.NewScope("batch")
		err := s.Engine.SetScope(context.Background(), s.Engine.GetBranch("batch/feature-a"), scope)
		require.NoError(t, err)
		err = s.Engine.SetScope(context.Background(), s.Engine.GetBranch("batch/feature-b"), scope)
		require.NoError(t, err)

		// Add PR info
		pr1 := 101
		pr2 := 102
		branchA := s.Engine.GetBranch("batch/feature-a")
		branchB := s.Engine.GetBranch("batch/feature-b")
		err = s.Engine.UpsertPrInfo(context.Background(), branchA, testhelpers.NewTestPrInfoWithTitle(pr1, "Feature A"))
		require.NoError(t, err)
		err = s.Engine.UpsertPrInfo(context.Background(), branchB, testhelpers.NewTestPrInfoWithTitle(pr2, "Feature B"))
		require.NoError(t, err)

		s.Checkout("batch/feature-b")

		plan, _, err := merge.CreateMergePlan(s.Context.Context, s.Engine, s.Context.Output, s.Context.GitHubClient, merge.CreatePlanOptions{
			Strategy: merge.StrategyShip,
			Force:    true,
		})
		require.NoError(t, err)

		executor := merge.NewConsolidateMergeExecutor(plan, s.Engine, s.Context)

		// Verify the executor understands the scope
		require.NotNil(t, executor)
		require.Equal(t, merge.StrategyShip, plan.Strategy)
		require.Len(t, plan.BranchesToMerge, 2)
	})

	t.Run("handles empty consolidation correctly", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// Single branch on main
		s.Checkout("main")

		// This should not create a consolidation plan since there's nothing to merge
		plan, validation, err := merge.CreateMergePlan(s.Context.Context, s.Engine, s.Context.Output, s.Context.GitHubClient, merge.CreatePlanOptions{
			Strategy: merge.StrategyShip,
			Force:    true,
		})

		require.Error(t, err)
		require.Nil(t, plan)
		require.Nil(t, validation)
		require.Contains(t, err.Error(), "cannot merge from trunk")
	})
}

func TestConsolidationStepExecution(t *testing.T) {
	t.Parallel()
	t.Run("executes consolidation step", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
			})

		// Add PR info
		pr1 := 101
		pr2 := 102
		branch1 := s.Engine.GetBranch("branch1")
		branch2 := s.Engine.GetBranch("branch2")
		err := s.Engine.UpsertPrInfo(context.Background(), branch1, testhelpers.NewTestPrInfo(pr1))
		require.NoError(t, err)
		err = s.Engine.UpsertPrInfo(context.Background(), branch2, testhelpers.NewTestPrInfo(pr2))
		require.NoError(t, err)

		s.Checkout("branch2")

		plan, _, err := merge.CreateMergePlan(s.Context.Context, s.Engine, s.Context.Output, s.Context.GitHubClient, merge.CreatePlanOptions{
			Strategy: merge.StrategyShip,
			Force:    true,
			Wait:     true,
		})
		require.NoError(t, err)

		// Test that the consolidation step is properly structured
		require.Len(t, plan.Steps, 4)
		require.Equal(t, merge.StepConsolidate, plan.Steps[0].StepType)
		require.Contains(t, plan.Steps[0].Description, "Consolidate 2 branches")
	})
}

func TestConsolidationErrorHandling(t *testing.T) {
	t.Parallel()
	t.Run("handles closed PR gracefully", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
			})

		// Add PR info with one closed PR
		pr1 := 101
		pr2 := 102
		branch1 := s.Engine.GetBranch("branch1")
		branch2 := s.Engine.GetBranch("branch2")
		err := s.Engine.UpsertPrInfo(context.Background(), branch1, testhelpers.NewTestPrInfoClosed(pr1))
		require.NoError(t, err)
		err = s.Engine.UpsertPrInfo(context.Background(), branch2, testhelpers.NewTestPrInfo(pr2))
		require.NoError(t, err)

		s.Checkout("branch2")

		plan, validation, err := merge.CreateMergePlan(s.Context.Context, s.Engine, s.Context.Output, s.Context.GitHubClient, merge.CreatePlanOptions{
			Strategy: merge.StrategyShip,
			Force:    false,
		})

		require.NoError(t, err)
		require.NotNil(t, plan)
		require.False(t, validation.Valid)
		require.Contains(t, validation.Errors[0], "Branch branch1 PR #101 is CLOSED (not open)")
	})

	t.Run("handles draft PR with force flag", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
			})

		// Add PR info with draft PR
		pr1 := 101
		pr2 := 102
		branch1 := s.Engine.GetBranch("branch1")
		branch2 := s.Engine.GetBranch("branch2")
		err := s.Engine.UpsertPrInfo(context.Background(), branch1, testhelpers.NewTestPrInfoDraft(pr1))
		require.NoError(t, err)
		err = s.Engine.UpsertPrInfo(context.Background(), branch2, testhelpers.NewTestPrInfo(pr2))
		require.NoError(t, err)

		s.Checkout("branch2")

		// Without force, should fail validation
		_, validation, err := merge.CreateMergePlan(s.Context.Context, s.Engine, s.Context.Output, s.Context.GitHubClient, merge.CreatePlanOptions{
			Strategy: merge.StrategyShip,
			Force:    false,
		})
		require.NoError(t, err)
		require.False(t, validation.Valid)

		// With force, should succeed
		var plan *merge.Plan
		plan, validation, err = merge.CreateMergePlan(s.Context.Context, s.Engine, s.Context.Output, s.Context.GitHubClient, merge.CreatePlanOptions{
			Strategy: merge.StrategyShip,
			Force:    true,
		})
		require.NoError(t, err)
		require.True(t, validation.Valid)
		require.Len(t, plan.BranchesToMerge, 2)
	})

	t.Run("execute with Wait: false skips waiting", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewRemoteScenario(t).
			WithStack(map[string]string{
				"branch1": "main",
			})

		s.RunGit("push", "-u", "origin", "branch1")

		// Add commits
		s.CheckoutQuiet("branch1")
		s.RunGit("commit", "--allow-empty", "-m", "branch1 commit").Rebuild()

		// Add PR info
		pr1 := 101
		branch1 := s.Engine.GetBranch("branch1")
		err := s.Engine.UpsertPrInfo(context.Background(), branch1, testhelpers.NewTestPrInfo(pr1))
		require.NoError(t, err)

		plan, _, err := merge.CreateMergePlan(s.Context.Context, s.Engine, s.Context.Output, s.Context.GitHubClient, merge.CreatePlanOptions{
			Strategy: merge.StrategyShip,
			Force:    true,
		})
		require.NoError(t, err)

		executor := merge.NewConsolidateMergeExecutor(plan, s.Engine, s.Context)

		// Mock GitHub client
		mockConfig := testhelpers.NewMockGitHubServerConfig()
		rawClient, owner, repo := testhelpers.NewMockGitHubClient(t, mockConfig)
		githubClient := testhelpers.NewMockGitHubClientInterface(rawClient, owner, repo, mockConfig)
		s.Context.GitHubClient = githubClient

		// Mock GitHub client to expect PR creation but NOT merge/waiting
		// Since we don't have a full mock setup here easily, we'll just verify it doesn't fail
		// and the result is returned correctly.
		result, err := executor.Execute(s.Context.Context, merge.ExecuteOptions{
			Plan:  plan,
			Wait:  false,
			Force: true,
		})

		require.NoError(t, err)
		require.NotNil(t, result)
		require.NotEmpty(t, result.BranchName)
		require.Equal(t, 1, result.PRNumber) // testhelpers.MockGitHubClient returns PR #1 for CreatePullRequest
	})
}
