package actions_test

import (
	"context"
	"testing"

	"github.com/google/go-github/v89/github"
	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/utils"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

// withCapturedBrowser overrides utils.OpenBrowser for the duration of a test
// so PRAction doesn't shell out to a real browser launcher. It returns a
// pointer to the URL that was "opened".
func withCapturedBrowser(t *testing.T) *string {
	t.Helper()
	opened := new(string)
	orig := utils.OpenBrowser
	utils.OpenBrowser = func(url string) error {
		*opened = url
		return nil
	}
	t.Cleanup(func() { utils.OpenBrowser = orig })
	return opened
}

// Subtests below intentionally don't call t.Parallel(): they all override the
// package-level utils.OpenBrowser var via withCapturedBrowser, which would
// race if they ran concurrently.
func TestPRAction(t *testing.T) {
	t.Parallel()

	t.Run("opens the current branch's PR when no argument given", func(t *testing.T) {
		opened := withCapturedBrowser(t)
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithLinearStack3().Checkout("b")

		err := s.Engine.UpsertPrInfo(context.Background(), s.Engine.GetBranch("b"), testhelpers.NewTestPrInfoFull(2, "", "", "OPEN", "a", "https://github.com/o/r/pull/2", false))
		require.NoError(t, err)

		require.NoError(t, actions.PRAction(s.Context, "", actions.PROptions{}))
		require.Equal(t, "https://github.com/o/r/pull/2", *opened)
	})

	t.Run("opens an explicit branch's PR", func(t *testing.T) {
		opened := withCapturedBrowser(t)
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithLinearStack3().Checkout("c")

		err := s.Engine.UpsertPrInfo(context.Background(), s.Engine.GetBranch("a"), testhelpers.NewTestPrInfoFull(1, "", "", "OPEN", "main", "https://github.com/o/r/pull/1", false))
		require.NoError(t, err)

		require.NoError(t, actions.PRAction(s.Context, "a", actions.PROptions{}))
		require.Equal(t, "https://github.com/o/r/pull/1", *opened)
	})

	t.Run("errors with an actionable message when the branch has no PR", func(t *testing.T) {
		withCapturedBrowser(t)
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithLinearStack3().Checkout("b")

		err := actions.PRAction(s.Context, "", actions.PROptions{})
		require.ErrorContains(t, err, `"b" has no associated pull request`)
		require.ErrorContains(t, err, "stackit submit")
	})

	t.Run("--stack opens the root PR of the stack", func(t *testing.T) {
		opened := withCapturedBrowser(t)
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithLinearStack3().Checkout("c")

		err := s.Engine.UpsertPrInfo(context.Background(), s.Engine.GetBranch("a"), testhelpers.NewTestPrInfoFull(1, "", "", "OPEN", "main", "https://github.com/o/r/pull/1", false))
		require.NoError(t, err)

		require.NoError(t, actions.PRAction(s.Context, "c", actions.PROptions{Stack: true}))
		require.Equal(t, "https://github.com/o/r/pull/1", *opened)
	})

	t.Run("resolves a PR number directly from GitHub", func(t *testing.T) {
		opened := withCapturedBrowser(t)
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		ghConfig := testhelpers.NewMockGitHubServerConfig()
		pr := &github.PullRequest{
			Number:  new(123),
			Head:    &github.PullRequestBranch{Ref: new("feature-a")},
			Base:    &github.PullRequestBranch{Ref: new("main")},
			Title:   new("Feature A"),
			HTMLURL: new("https://github.com/o/r/pull/123"),
		}
		ghConfig.CreatedPRs = append(ghConfig.CreatedPRs, pr)
		ghClient, owner, repo := testhelpers.NewMockGitHubClient(t, ghConfig)
		s.Context.GitHubClient = testhelpers.NewMockGitHubClientInterface(ghClient, owner, repo, ghConfig)

		require.NoError(t, actions.PRAction(s.Context, "123", actions.PROptions{}))
		require.Equal(t, "https://github.com/o/r/pull/123", *opened)
	})

	t.Run("errors when resolving a PR number without a GitHub client", func(t *testing.T) {
		withCapturedBrowser(t)
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		err := actions.PRAction(s.Context, "123", actions.PROptions{})
		require.ErrorContains(t, err, "cannot resolve PR #123")
	})
}
