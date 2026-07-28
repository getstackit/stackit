package actions_test

import (
	"testing"

	"github.com/google/go-github/v89/github"
	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestPrAction(t *testing.T) {
	t.Parallel()

	t.Run("resolves the current branch's pull request", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithLinearStack3()

		require.NoError(t, s.Engine.UpsertPrInfo(s.Context.Context, s.Engine.GetBranch("c"), engine.NewPrInfo(
			new(9), "Add c", "", git.PRStateOpen, "b", "https://github.com/acme/widgets/pull/9", false,
		)))

		result, err := actions.PrAction(s.Context, actions.PrOptions{})
		require.NoError(t, err)
		require.Equal(t, "c", result.BranchName)
		require.Equal(t, 9, result.PRNumber)
		require.Equal(t, "https://github.com/acme/widgets/pull/9", result.URL)
	})

	t.Run("resolves an explicit branch's pull request", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithLinearStack3()

		require.NoError(t, s.Engine.UpsertPrInfo(s.Context.Context, s.Engine.GetBranch("a"), engine.NewPrInfo(
			new(1), "Add a", "", git.PRStateOpen, "main", "https://github.com/acme/widgets/pull/1", false,
		)))

		result, err := actions.PrAction(s.Context, actions.PrOptions{BranchOrPR: "a"})
		require.NoError(t, err)
		require.Equal(t, "a", result.BranchName)
		require.Equal(t, 1, result.PRNumber)
	})

	t.Run("--stack resolves the root pull request of the stack", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithLinearStack3()

		require.NoError(t, s.Engine.UpsertPrInfo(s.Context.Context, s.Engine.GetBranch("a"), engine.NewPrInfo(
			new(1), "Add a", "", git.PRStateOpen, "main", "https://github.com/acme/widgets/pull/1", false,
		)))
		require.NoError(t, s.Engine.UpsertPrInfo(s.Context.Context, s.Engine.GetBranch("c"), engine.NewPrInfo(
			new(3), "Add c", "", git.PRStateOpen, "b", "https://github.com/acme/widgets/pull/3", false,
		)))

		result, err := actions.PrAction(s.Context, actions.PrOptions{Stack: true})
		require.NoError(t, err)
		require.Equal(t, "a", result.BranchName)
		require.Equal(t, 1, result.PRNumber)
	})

	t.Run("resolves a PR number via the GitHub API", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		ghConfig := testhelpers.NewMockGitHubServerConfig()
		pr := &github.PullRequest{
			Number:  new(123),
			Head:    &github.PullRequestBranch{Ref: new("feature-a")},
			Base:    &github.PullRequestBranch{Ref: new("main")},
			Title:   new("Feature A"),
			HTMLURL: new("https://github.com/acme/widgets/pull/123"),
		}
		ghConfig.PRs["feature-a"] = pr
		ghConfig.CreatedPRs = append(ghConfig.CreatedPRs, pr)
		ghClient, owner, repo := testhelpers.NewMockGitHubClient(t, ghConfig)
		s.Context.GitHubClient = testhelpers.NewMockGitHubClientInterface(ghClient, owner, repo, ghConfig)

		result, err := actions.PrAction(s.Context, actions.PrOptions{BranchOrPR: "123"})
		require.NoError(t, err)
		require.Equal(t, 123, result.PRNumber)
		require.Equal(t, "https://github.com/acme/widgets/pull/123", result.URL)
	})

	t.Run("errors on trunk", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithLinearStack3()
		s.Checkout("main")

		_, err := actions.PrAction(s.Context, actions.PrOptions{})
		require.ErrorContains(t, err, "trunk branch")
	})

	t.Run("errors when the branch is not tracked", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithLinearStack3()

		_, err := actions.PrAction(s.Context, actions.PrOptions{BranchOrPR: "does-not-exist"})
		require.ErrorContains(t, err, "not tracked")
	})

	t.Run("errors when the branch has no pull request", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithLinearStack3()

		_, err := actions.PrAction(s.Context, actions.PrOptions{BranchOrPR: "a"})
		require.ErrorContains(t, err, "no associated pull request")
	})
}
