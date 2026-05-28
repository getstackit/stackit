package github_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
	githubpkg "github.com/getstackit/stackit/internal/github"
	"github.com/getstackit/stackit/testhelpers"
)

func TestCreatePullRequest(t *testing.T) {
	t.Run("creates a pull request successfully", func(t *testing.T) {
		config := testhelpers.NewMockGitHubServerConfig()
		client, owner, repo := testhelpers.NewMockGitHubClient(t, config)

		opts := githubpkg.CreatePROptions{
			Title: "Test PR",
			Body:  "This is a test PR",
			Head:  "feature-branch",
			Base:  "main",
			Draft: false,
		}

		_, pr, err := githubpkg.CreatePullRequest(context.Background(), client, owner, repo, opts)
		require.NoError(t, err)
		require.NotNil(t, pr)
		require.NotNil(t, pr.Number)
		require.Equal(t, 1, *pr.Number)
		require.Equal(t, opts.Title, *pr.Title)
		require.Equal(t, opts.Body, *pr.Body)
		require.Equal(t, opts.Head, *pr.Head.Ref)
		require.Equal(t, opts.Base, *pr.Base.Ref)
		require.Equal(t, opts.Draft, *pr.Draft)
		require.NotEmpty(t, *pr.HTMLURL)
	})

	t.Run("creates a draft pull request", func(t *testing.T) {
		config := testhelpers.NewMockGitHubServerConfig()
		client, owner, repo := testhelpers.NewMockGitHubClient(t, config)

		opts := githubpkg.CreatePROptions{
			Title: "Draft PR",
			Head:  "feature-branch",
			Base:  "main",
			Draft: true,
		}

		_, pr, err := githubpkg.CreatePullRequest(context.Background(), client, owner, repo, opts)
		require.NoError(t, err)
		require.NotNil(t, pr)
		require.True(t, *pr.Draft)
	})

	t.Run("creates PR with reviewers", func(t *testing.T) {
		config := testhelpers.NewMockGitHubServerConfig()
		client, owner, repo := testhelpers.NewMockGitHubClient(t, config)

		opts := githubpkg.CreatePROptions{
			Title:     "PR with reviewers",
			Head:      "feature-branch",
			Base:      "main",
			Reviewers: []string{"reviewer1", "reviewer2"},
		}

		_, pr, err := githubpkg.CreatePullRequest(context.Background(), client, owner, repo, opts)
		require.NoError(t, err)
		require.NotNil(t, pr)
		// Reviewers are requested (non-fatal if it fails, so we just check PR was created)
		require.NotNil(t, pr.Number)
	})

	t.Run("creates PR with team reviewers", func(t *testing.T) {
		config := testhelpers.NewMockGitHubServerConfig()
		client, owner, repo := testhelpers.NewMockGitHubClient(t, config)

		opts := githubpkg.CreatePROptions{
			Title:         "PR with team reviewers",
			Head:          "feature-branch",
			Base:          "main",
			TeamReviewers: []string{"team1", "team2"},
		}

		_, pr, err := githubpkg.CreatePullRequest(context.Background(), client, owner, repo, opts)
		require.NoError(t, err)
		require.NotNil(t, pr)
		require.NotNil(t, pr.Number)
	})
}

func TestUpdatePullRequest(t *testing.T) {
	t.Run("updates PR title and body", func(t *testing.T) {
		config := testhelpers.NewMockGitHubServerConfig()
		client, owner, repo := testhelpers.NewMockGitHubClient(t, config)

		// First create a PR
		createOpts := githubpkg.CreatePROptions{
			Title: "Original Title",
			Body:  "Original Body",
			Head:  "feature-branch",
			Base:  "main",
		}
		_, createdPR, err := githubpkg.CreatePullRequest(context.Background(), client, owner, repo, createOpts)
		require.NoError(t, err)
		require.NotNil(t, createdPR.Number)

		// Update the PR
		newTitle := "Updated Title"
		newBody := "Updated Body"
		updateOpts := githubpkg.UpdatePROptions{
			Title: &newTitle,
			Body:  &newBody,
		}

		_, err = githubpkg.UpdatePullRequest(context.Background(), client, git.NewRunner(nil), owner, repo, *createdPR.Number, updateOpts)
		require.NoError(t, err)

		// Verify the update
		updatedPR, _, err := client.PullRequests.Get(context.Background(), owner, repo, *createdPR.Number)
		require.NoError(t, err)
		require.Equal(t, newTitle, *updatedPR.Title)
		require.Equal(t, newBody, *updatedPR.Body)
	})

	t.Run("updates PR base branch", func(t *testing.T) {
		config := testhelpers.NewMockGitHubServerConfig()
		client, owner, repo := testhelpers.NewMockGitHubClient(t, config)

		// Create a PR
		createOpts := githubpkg.CreatePROptions{
			Title: "Test PR",
			Head:  "feature-branch",
			Base:  "main",
		}
		_, createdPR, err := githubpkg.CreatePullRequest(context.Background(), client, owner, repo, createOpts)
		require.NoError(t, err)

		// Update base branch
		newBase := "develop"
		updateOpts := githubpkg.UpdatePROptions{
			Base: &newBase,
		}

		_, err = githubpkg.UpdatePullRequest(context.Background(), client, git.NewRunner(nil), owner, repo, *createdPR.Number, updateOpts)
		require.NoError(t, err)

		// Verify the update
		updatedPR, _, err := client.PullRequests.Get(context.Background(), owner, repo, *createdPR.Number)
		require.NoError(t, err)
		require.Equal(t, newBase, *updatedPR.Base.Ref)
	})

	// Note: Updating draft status via the REST API is not supported by GitHub.
	// To convert a draft PR to ready-for-review, you need to use the GraphQL API's
	// markPullRequestReadyForReview mutation. The Draft field in UpdatePROptions
	// is kept for potential future GraphQL support but won't work with the REST API.

	t.Run("updates PR with reviewers", func(t *testing.T) {
		config := testhelpers.NewMockGitHubServerConfig()
		client, owner, repo := testhelpers.NewMockGitHubClient(t, config)

		// Create a PR
		createOpts := githubpkg.CreatePROptions{
			Title: "Test PR",
			Head:  "feature-branch",
			Base:  "main",
		}
		_, createdPR, err := githubpkg.CreatePullRequest(context.Background(), client, owner, repo, createOpts)
		require.NoError(t, err)

		// Update with reviewers
		updateOpts := githubpkg.UpdatePROptions{
			Reviewers: []string{"reviewer1"},
		}

		_, err = githubpkg.UpdatePullRequest(context.Background(), client, git.NewRunner(nil), owner, repo, *createdPR.Number, updateOpts)
		require.NoError(t, err)
	})
}

func TestGetPullRequestByBranch(t *testing.T) {
	t.Run("gets PR for existing branch", func(t *testing.T) {
		config := testhelpers.NewMockGitHubServerConfig()
		client, owner, repo := testhelpers.NewMockGitHubClient(t, config)

		// Create a PR
		branchName := "feature-branch"
		createOpts := githubpkg.CreatePROptions{
			Title: "Test PR",
			Head:  branchName,
			Base:  "main",
		}
		_, createdPR, err := githubpkg.CreatePullRequest(context.Background(), client, owner, repo, createOpts)
		require.NoError(t, err)

		// Get PR by branch
		pr, err := githubpkg.GetPullRequestByBranch(context.Background(), client, owner, repo, branchName)
		require.NoError(t, err)
		require.NotNil(t, pr)
		require.Equal(t, *createdPR.Number, *pr.Number)
		require.Equal(t, branchName, *pr.Head.Ref)
	})

	t.Run("returns nil for non-existent branch", func(t *testing.T) {
		config := testhelpers.NewMockGitHubServerConfig()
		client, owner, repo := testhelpers.NewMockGitHubClient(t, config)

		// Try to get PR for non-existent branch
		pr, err := githubpkg.GetPullRequestByBranch(context.Background(), client, owner, repo, "non-existent-branch")
		require.NoError(t, err)
		require.Nil(t, pr)
	})
}
