package get_test

import (
	"context"
	"sync"
	"testing"

	"github.com/getstackit/stackit/internal/app/actiondeps"

	"github.com/google/go-github/v93/github"
	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions/get"
	stackitgithub "github.com/getstackit/stackit/internal/github"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

// deps builds get's collaborators from the scenario. Call it after the test
// has installed its GitHub client so the client is the one get sees.
func deps(s *scenario.Scenario) get.Deps {
	ctx := s.Context
	return get.Deps{
		Engine:      ctx.Engine,
		Out:         ctx.Output,
		GitHub:      ctx.GitHub,
		GitHubError: ctx.GitHubError,
		Restack:     actiondeps.Restack(ctx),
	}
}

type countingGitHubClient struct {
	stackitgithub.Client
	mu          sync.Mutex
	branchCalls map[string]int
}

func newCountingGitHubClient(client stackitgithub.Client) *countingGitHubClient {
	return &countingGitHubClient{
		Client:      client,
		branchCalls: make(map[string]int),
	}
}

func (c *countingGitHubClient) GetPullRequestByBranch(ctx context.Context, branchName string) (*stackitgithub.PullRequestInfo, error) {
	c.mu.Lock()
	c.branchCalls[branchName]++
	c.mu.Unlock()

	return c.Client.GetPullRequestByBranch(ctx, branchName)
}

func (c *countingGitHubClient) branchCallCount(branchName string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.branchCalls[branchName]
}

func TestAction(t *testing.T) {
	t.Parallel()
	t.Run("resolves PR number and fetches branches", func(t *testing.T) {
		t.Parallel()
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

		// Run get.Action with PR number
		err := get.Action(t.Context(), deps(s), "123", get.Options{}, &get.NullHandler{})
		require.NoError(t, err)

		// Verify branch feature-a exists and is tracked
		require.True(t, s.Engine.GetBranch("feature-a").IsTracked())
		require.Equal(t, "main", s.Engine.GetBranch("feature-a").Parent().Name())
	})

	t.Run("crawls ancestors via GitHub PRs", func(t *testing.T) {
		t.Parallel()
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
		countingClient := newCountingGitHubClient(testhelpers.NewMockGitHubClientInterface(ghClient, owner, repo, ghConfig))
		s.Context.GitHubClient = countingClient

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

		// Run get.Action for feature-b
		err := get.Action(t.Context(), deps(s), "feature-b", get.Options{}, &get.NullHandler{})
		require.NoError(t, err)

		// Verify both branches are tracked correctly
		require.True(t, s.Engine.GetBranch("feature-a").IsTracked())
		require.True(t, s.Engine.GetBranch("feature-b").IsTracked())
		require.Equal(t, "main", s.Engine.GetBranch("feature-a").Parent().Name())
		require.Equal(t, "feature-a", s.Engine.GetBranch("feature-b").Parent().Name())
		require.Equal(t, 1, countingClient.branchCallCount("feature-a"))
		require.Equal(t, 1, countingClient.branchCallCount("feature-b"))
	})

	t.Run("identifies and syncs local descendants", func(t *testing.T) {
		t.Parallel()
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

		// Run get.Action for feature-a
		err := get.Action(t.Context(), deps(s), "feature-a", get.Options{}, &get.NullHandler{})
		require.NoError(t, err)

		// feature-b should also have been refreshed/checked out because it's upstack of a
		// (though in this unit test it might not do much since remote == local)
	})

	t.Run("fails if uncommitted changes exist", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			WithUncommittedChange("dirty.txt")

		err := get.Action(t.Context(), deps(s), "some-branch", get.Options{}, &get.NullHandler{})
		require.Error(t, err)
		require.Contains(t, err.Error(), "uncommitted changes")
	})
}
