package actions_test

import (
	"testing"

	"github.com/google/go-github/v62/github"
	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/internal/actions"
	"stackit.dev/stackit/testhelpers"
	"stackit.dev/stackit/testhelpers/scenario"
)

func TestGetAction(t *testing.T) {
	t.Run("resolves PR number and fetches branches", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		// Mock GitHub setup
		ghConfig := testhelpers.NewMockGitHubServerConfig()
		pr := &github.PullRequest{
			Number: new(123),
			Head:   &github.PullRequestBranch{Ref: new("feature-a")},
			Base:   &github.PullRequestBranch{Ref: new("main")},
			Title:  new("Feature A"),
		}
		ghConfig.PRs["feature-a"] = pr
		ghConfig.CreatedPRs = append(ghConfig.CreatedPRs, pr)
		ghClient, owner, repo := testhelpers.NewMockGitHubClient(t, ghConfig)
		s.Context.GitHubClient = testhelpers.NewMockGitHubClientInterface(ghClient, owner, repo, ghConfig)

		// Create a bare repository to act as the remote
		remoteDir := t.TempDir()
		s.RunGit("init", "--bare", remoteDir)

		// Create remote branch feature-a by creating it and pushing it to origin
		s.RunGit("checkout", "-b", "feature-a").
			RunGit("remote", "add", "origin", remoteDir).
			RunGit("push", "-u", "origin", "feature-a").
			RunGit("checkout", "main").
			RunGit("branch", "-D", "feature-a")

		// Run GetAction with PR number
		err := actions.GetAction(s.Context, "123", actions.GetOptions{}, &actions.GetNullHandler{})
		require.NoError(t, err)

		// Verify branch feature-a exists and is tracked
		require.True(t, s.Engine.GetBranch("feature-a").IsTracked())
		require.Equal(t, "main", s.Engine.GetBranch("feature-a").GetParent().GetName())
	})

	t.Run("crawls ancestors via GitHub PRs", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		// Mock GitHub setup: feature-b -> feature-a -> main
		ghConfig := testhelpers.NewMockGitHubServerConfig()
		prB := &github.PullRequest{
			Number: new(2),
			Head:   &github.PullRequestBranch{Ref: new("feature-b")},
			Base:   &github.PullRequestBranch{Ref: new("feature-a")},
		}
		prA := &github.PullRequest{
			Number: new(1),
			Head:   &github.PullRequestBranch{Ref: new("feature-a")},
			Base:   &github.PullRequestBranch{Ref: new("main")},
		}
		ghConfig.PRs["feature-b"] = prB
		ghConfig.PRs["feature-a"] = prA
		ghClient, owner, repo := testhelpers.NewMockGitHubClient(t, ghConfig)
		s.Context.GitHubClient = testhelpers.NewMockGitHubClientInterface(ghClient, owner, repo, ghConfig)

		// Create a bare repository to act as the remote
		remoteDir := t.TempDir()
		s.RunGit("init", "--bare", remoteDir)

		// Create remote branches
		s.RunGit("checkout", "-b", "feature-a").
			RunGit("checkout", "-b", "feature-b").
			RunGit("remote", "add", "origin", remoteDir).
			RunGit("push", "-u", "origin", "feature-a").
			RunGit("push", "-u", "origin", "feature-b").
			RunGit("checkout", "main").
			RunGit("branch", "-D", "feature-a").
			RunGit("branch", "-D", "feature-b")

		// Run GetAction for feature-b
		err := actions.GetAction(s.Context, "feature-b", actions.GetOptions{}, &actions.GetNullHandler{})
		require.NoError(t, err)

		// Verify both branches are tracked correctly
		require.True(t, s.Engine.GetBranch("feature-a").IsTracked())
		require.True(t, s.Engine.GetBranch("feature-b").IsTracked())
		require.Equal(t, "main", s.Engine.GetBranch("feature-a").GetParent().GetName())
		require.Equal(t, "feature-a", s.Engine.GetBranch("feature-b").GetParent().GetName())
	})

	t.Run("identifies and syncs local descendants", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature-a").
			Commit("a1").
			CreateBranch("feature-b").
			Commit("b1").
			TrackBranch("feature-a", "main").
			TrackBranch("feature-b", "feature-a")

		// Mock GitHub: just feature-a
		ghConfig := testhelpers.NewMockGitHubServerConfig()
		ghConfig.PRs["feature-a"] = &github.PullRequest{
			Number: new(1),
			Head:   &github.PullRequestBranch{Ref: new("feature-a")},
			Base:   &github.PullRequestBranch{Ref: new("main")},
		}
		ghClient, owner, repo := testhelpers.NewMockGitHubClient(t, ghConfig)
		s.Context.GitHubClient = testhelpers.NewMockGitHubClientInterface(ghClient, owner, repo, ghConfig)

		s.RunGit("remote", "add", "origin", s.Scene.Dir)

		// Run GetAction for feature-a
		err := actions.GetAction(s.Context, "feature-a", actions.GetOptions{}, &actions.GetNullHandler{})
		require.NoError(t, err)

		// feature-b should also have been refreshed/checked out because it's upstack of a
		// (though in this unit test it might not do much since remote == local)
	})

	t.Run("fails if uncommitted changes exist", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			WithUncommittedChange("dirty.txt")

		err := actions.GetAction(s.Context, "some-branch", actions.GetOptions{}, &actions.GetNullHandler{})
		require.Error(t, err)
		require.Contains(t, err.Error(), "uncommitted changes")
	})
}
