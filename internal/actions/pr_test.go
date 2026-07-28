package actions_test

import (
	"context"
	"testing"

	"github.com/google/go-github/v89/github"
	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestPROpenAction(t *testing.T) {
	t.Parallel()

	t.Run("errors when not on a branch and no target given", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.RunGit("checkout", "--detach", "main")

		_, err := actions.PROpenAction(s.Context, actions.PROpenOptions{})
		require.Error(t, err)
		require.Contains(t, err.Error(), "not on a branch")
	})

	t.Run("resolves from local metadata without calling GitHub", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature-a").
			Commit("a1").
			TrackBranch("feature-a", "main")

		branch := s.Engine.GetBranch("feature-a")
		prInfo := testhelpers.NewTestPrInfoFull(42, "Feature A", "", "OPEN", "main", "https://github.com/owner/repo/pull/42", false)
		require.NoError(t, s.Engine.UpsertPrInfo(context.Background(), branch, prInfo))

		result, err := actions.PROpenAction(s.Context, actions.PROpenOptions{BranchOrPR: "feature-a"})
		require.NoError(t, err)
		require.Equal(t, "feature-a", result.Branch)
		require.Equal(t, 42, *result.Number)
		require.Equal(t, "https://github.com/owner/repo/pull/42", result.URL)
	})

	t.Run("falls back to GitHub when local metadata has no PR", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature-a").
			Commit("a1").
			TrackBranch("feature-a", "main")

		ghConfig := testhelpers.NewMockGitHubServerConfig()
		pr := &github.PullRequest{
			Number:  new(7),
			Head:    &github.PullRequestBranch{Ref: new("feature-a")},
			Base:    &github.PullRequestBranch{Ref: new("main")},
			Title:   new("Feature A"),
			HTMLURL: new("https://github.com/owner/repo/pull/7"),
		}
		ghConfig.PRs["feature-a"] = pr
		ghClient, owner, repo := testhelpers.NewMockGitHubClient(t, ghConfig)
		s.Context.GitHubClient = testhelpers.NewMockGitHubClientInterface(ghClient, owner, repo, ghConfig)

		result, err := actions.PROpenAction(s.Context, actions.PROpenOptions{BranchOrPR: "feature-a"})
		require.NoError(t, err)
		require.Equal(t, 7, *result.Number)
		require.Equal(t, "https://github.com/owner/repo/pull/7", result.URL)
	})

	t.Run("errors when no pull request exists anywhere", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature-a").
			Commit("a1").
			TrackBranch("feature-a", "main")

		_, err := actions.PROpenAction(s.Context, actions.PROpenOptions{BranchOrPR: "feature-a"})
		require.Error(t, err)
		require.Contains(t, err.Error(), `no pull request found for "feature-a"`)
	})

	t.Run("resolves a PR number via GitHub", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		ghConfig := testhelpers.NewMockGitHubServerConfig()
		pr := &github.PullRequest{
			Number:  new(123),
			Head:    &github.PullRequestBranch{Ref: new("feature-a")},
			Base:    &github.PullRequestBranch{Ref: new("main")},
			Title:   new("Feature A"),
			HTMLURL: new("https://github.com/owner/repo/pull/123"),
		}
		ghConfig.CreatedPRs = append(ghConfig.CreatedPRs, pr)
		ghClient, owner, repo := testhelpers.NewMockGitHubClient(t, ghConfig)
		s.Context.GitHubClient = testhelpers.NewMockGitHubClientInterface(ghClient, owner, repo, ghConfig)

		result, err := actions.PROpenAction(s.Context, actions.PROpenOptions{BranchOrPR: "123"})
		require.NoError(t, err)
		require.Equal(t, "feature-a", result.Branch)
		require.Equal(t, 123, *result.Number)
		require.Equal(t, "https://github.com/owner/repo/pull/123", result.URL)
	})

	t.Run("errors resolving a PR number without a GitHub client", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		_, err := actions.PROpenAction(s.Context, actions.PROpenOptions{BranchOrPR: "123"})
		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot resolve PR #123")
	})

	t.Run("--stack resolves to the root branch of the stack", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature-a").
			Commit("a1").
			CreateBranch("feature-b").
			Commit("b1").
			TrackBranch("feature-a", "main").
			TrackBranch("feature-b", "feature-a")

		rootPR := testhelpers.NewTestPrInfoFull(1, "Feature A", "", "OPEN", "main", "https://github.com/owner/repo/pull/1", false)
		require.NoError(t, s.Engine.UpsertPrInfo(context.Background(), s.Engine.GetBranch("feature-a"), rootPR))

		result, err := actions.PROpenAction(s.Context, actions.PROpenOptions{BranchOrPR: "feature-b", Stack: true})
		require.NoError(t, err)
		require.Equal(t, "feature-a", result.Branch)
		require.Equal(t, "https://github.com/owner/repo/pull/1", result.URL)
	})
}
