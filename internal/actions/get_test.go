package actions_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestGetAction(t *testing.T) {
	t.Parallel()
	t.Run("reparents an existing branch using GitHub base info without carrying old parent commits", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithLinearStack("a", "b")

		_, err := s.Scene.Repo.CreateBareRemote("origin")
		require.NoError(t, err)
		for _, branch := range []string{"main", "a", "b"} {
			require.NoError(t, s.Scene.Repo.PushBranch("origin", branch))
		}

		mockConfig := testhelpers.NewMockGitHubServerConfig()
		rawClient, owner, repo := testhelpers.NewMockGitHubClient(t, mockConfig)
		s.Context.GitHubClient = testhelpers.NewMockGitHubClientInterface(rawClient, owner, repo, mockConfig)

		prData := testhelpers.DefaultPRData()
		prData.Number = 101
		prData.Head = "b"
		prData.Base = "main"
		mockConfig.PRs["b"] = testhelpers.NewSamplePullRequest(prData)

		s.Checkout("b")

		err = actions.GetAction(s.Context, "b", actions.GetOptions{Restack: true}, &actions.GetNullHandler{})
		require.NoError(t, err)

		parent := s.Engine.GetBranch("b").GetParent()
		require.NotNil(t, parent)
		require.Equal(t, "main", parent.GetName())

		commitCount, err := s.Scene.Repo.GetCommitCount("main", "b")
		require.NoError(t, err)
		require.Equal(t, 1, commitCount)
	})
}
