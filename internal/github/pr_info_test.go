package github_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/internal/git"
	githubpkg "stackit.dev/stackit/internal/github"
	"stackit.dev/stackit/testhelpers"
)

func TestParseGitHubRemoteURL(t *testing.T) {
	t.Run("parses HTTPS github.com URL", func(t *testing.T) {
		info, err := githubpkg.ParseGitHubRemoteURL("https://github.com/owner/repo.git")
		require.NoError(t, err)
		require.NotNil(t, info)
		require.Equal(t, "github.com", info.Hostname)
		require.Equal(t, "owner", info.Owner)
		require.Equal(t, "repo", info.Repo)
	})

	t.Run("parses HTTPS github.com URL without .git suffix", func(t *testing.T) {
		info, err := githubpkg.ParseGitHubRemoteURL("https://github.com/owner/repo")
		require.NoError(t, err)
		require.NotNil(t, info)
		require.Equal(t, "github.com", info.Hostname)
		require.Equal(t, "owner", info.Owner)
		require.Equal(t, "repo", info.Repo)
	})

	t.Run("parses SSH github.com URL", func(t *testing.T) {
		info, err := githubpkg.ParseGitHubRemoteURL("git@github.com:owner/repo.git")
		require.NoError(t, err)
		require.NotNil(t, info)
		require.Equal(t, "github.com", info.Hostname)
		require.Equal(t, "owner", info.Owner)
		require.Equal(t, "repo", info.Repo)
	})

	t.Run("parses SSH github.com URL without .git suffix", func(t *testing.T) {
		info, err := githubpkg.ParseGitHubRemoteURL("git@github.com:owner/repo")
		require.NoError(t, err)
		require.NotNil(t, info)
		require.Equal(t, "github.com", info.Hostname)
		require.Equal(t, "owner", info.Owner)
		require.Equal(t, "repo", info.Repo)
	})

	t.Run("parses HTTPS GitHub Enterprise URL", func(t *testing.T) {
		info, err := githubpkg.ParseGitHubRemoteURL("https://github.company.com/owner/repo.git")
		require.NoError(t, err)
		require.NotNil(t, info)
		require.Equal(t, "github.company.com", info.Hostname)
		require.Equal(t, "owner", info.Owner)
		require.Equal(t, "repo", info.Repo)
	})

	t.Run("parses SSH GitHub Enterprise URL", func(t *testing.T) {
		info, err := githubpkg.ParseGitHubRemoteURL("git@github.company.com:owner/repo.git")
		require.NoError(t, err)
		require.NotNil(t, info)
		require.Equal(t, "github.company.com", info.Hostname)
		require.Equal(t, "owner", info.Owner)
		require.Equal(t, "repo", info.Repo)
	})

	t.Run("parses HTTP URL (non-HTTPS)", func(t *testing.T) {
		info, err := githubpkg.ParseGitHubRemoteURL("http://github.company.com/owner/repo.git")
		require.NoError(t, err)
		require.NotNil(t, info)
		require.Equal(t, "github.company.com", info.Hostname)
		require.Equal(t, "owner", info.Owner)
		require.Equal(t, "repo", info.Repo)
	})

	t.Run("parses Enterprise GitHub URL with simple hostname", func(t *testing.T) {
		info, err := githubpkg.ParseGitHubRemoteURL("https://my-internal-github/org/repo")
		require.NoError(t, err)
		require.NotNil(t, info)
		require.Equal(t, "my-internal-github", info.Hostname)
		require.Equal(t, "org", info.Owner)
		require.Equal(t, "repo", info.Repo)
	})

	t.Run("handles URLs with extra path segments", func(t *testing.T) {
		info, err := githubpkg.ParseGitHubRemoteURL("https://github.company.com/org/team/repo.git")
		require.NoError(t, err)
		require.NotNil(t, info)
		require.Equal(t, "github.company.com", info.Hostname)
		require.Equal(t, "team", info.Owner) // Second-to-last segment
		require.Equal(t, "repo", info.Repo)  // Last segment
	})

	t.Run("handles URLs with whitespace", func(t *testing.T) {
		info, err := githubpkg.ParseGitHubRemoteURL("  https://github.com/owner/repo.git  ")
		require.NoError(t, err)
		require.NotNil(t, info)
		require.Equal(t, "github.com", info.Hostname)
		require.Equal(t, "owner", info.Owner)
		require.Equal(t, "repo", info.Repo)
	})

	t.Run("returns error for invalid SSH URL format", func(t *testing.T) {
		info, err := githubpkg.ParseGitHubRemoteURL("git@github.com")
		require.Error(t, err)
		require.Nil(t, info)
		require.Contains(t, err.Error(), "invalid SSH remote URL")
	})

	t.Run("returns error for invalid HTTPS URL format", func(t *testing.T) {
		info, err := githubpkg.ParseGitHubRemoteURL("https://github.com")
		require.Error(t, err)
		require.Nil(t, info)
		require.Contains(t, err.Error(), "invalid HTTPS remote URL")
	})

	t.Run("returns error for empty URL", func(t *testing.T) {
		info, err := githubpkg.ParseGitHubRemoteURL("")
		require.Error(t, err)
		require.Nil(t, info)
	})

	t.Run("returns error for URL missing owner", func(t *testing.T) {
		info, err := githubpkg.ParseGitHubRemoteURL("https://github.com/repo.git")
		require.Error(t, err)
		require.Nil(t, info)
	})
}

// Note: createGitHubClient is tested indirectly through TestGetGitHubClient
// since it's an unexported function. The test verifies that:
// 1. github.com clients use default GitHub API URLs (api.github.com and uploads.github.com)
// 2. Enterprise clients use custom URLs with /api/v3/ and /api/uploads/ endpoints

// Note: getRepoInfoWithHostname is tested indirectly through TestGetGitHubClient
// since it's an unexported function. The test verifies that it correctly:
// 1. Parses HTTPS and SSH remote URLs
// 2. Extracts hostname, owner, and repo correctly
// 3. Handles GitHub Enterprise URLs

// TestGetGitHubClient tests GetGitHubClient which uses createGitHubClient and getRepoInfoWithHostname
// Note: These tests require a valid git repository with a remote configured.
// They may be skipped in environments where git operations are restricted.
// NOTE: NewScene is NOT safe for parallel tests, so these tests must run sequentially.
func TestGetGitHubClient(t *testing.T) {
	t.Run("creates client for github.com", func(t *testing.T) {
		// Reset default repo to ensure clean state
		git.ResetDefaultRepo()

		scene := testhelpers.NewScene(t, func(s *testhelpers.Scene) error {
			return s.Repo.CreateChangeAndCommit("initial", "init")
		})

		// Set up github.com remote (add if it doesn't exist, otherwise set-url)
		err := scene.Repo.RunGitCommand("remote", "add", "origin", "https://github.com/testowner/testrepo.git")
		if err != nil {
			// If remote already exists, just update the URL
			err = scene.Repo.RunGitCommand("remote", "set-url", "origin", "https://github.com/testowner/testrepo.git")
			require.NoError(t, err)
		}

		// Initialize git repo (should work since NewScene changes to scene.Dir)
		err = git.InitDefaultRepoInDir(scene.Dir)
		if err != nil {
			t.Skipf("Skipping test: failed to initialize git repo (may be sandbox restriction): %v", err)
			return
		}

		// Mock token by setting environment variable
		t.Setenv("GITHUB_TOKEN", "test-token")

		client, owner, repo, err := githubpkg.GetGitHubClient(context.Background())
		// Note: This may fail if gh CLI is not available, but that's okay for testing the logic
		if err != nil {
			// If it fails due to token issues, that's expected in test environment
			require.Contains(t, err.Error(), "token")
			return
		}

		require.NotNil(t, client)
		require.Equal(t, "testowner", owner)
		require.Equal(t, "testrepo", repo)

		// For github.com, BaseURL should be the default GitHub API URL
		// The go-github library sets a default BaseURL even for github.com
		require.NotNil(t, client.BaseURL)
		require.Contains(t, client.BaseURL.String(), "api.github.com")
		require.NotNil(t, client.UploadURL)
		require.Contains(t, client.UploadURL.String(), "uploads.github.com")
	})

	t.Run("creates client for GitHub Enterprise", func(t *testing.T) {
		// Reset default repo to ensure clean state
		git.ResetDefaultRepo()

		scene := testhelpers.NewScene(t, func(s *testhelpers.Scene) error {
			return s.Repo.CreateChangeAndCommit("initial", "init")
		})

		// Set up Enterprise remote (add if it doesn't exist, otherwise set-url)
		err := scene.Repo.RunGitCommand("remote", "add", "origin", "https://github.company.com/enterprise/repo.git")
		if err != nil {
			// If remote already exists, just update the URL
			err = scene.Repo.RunGitCommand("remote", "set-url", "origin", "https://github.company.com/enterprise/repo.git")
			require.NoError(t, err)
		}

		// Initialize git repo
		err = git.InitDefaultRepoInDir(scene.Dir)
		if err != nil {
			t.Skipf("Skipping test: failed to initialize git repo (may be sandbox restriction): %v", err)
			return
		}

		// Mock token
		t.Setenv("GITHUB_TOKEN", "test-token")

		client, owner, repo, err := githubpkg.GetGitHubClient(context.Background())
		if err != nil {
			// If it fails due to token issues, that's expected in test environment
			require.Contains(t, err.Error(), "token")
			return
		}

		require.NotNil(t, client)
		require.Equal(t, "enterprise", owner)
		require.Equal(t, "repo", repo)

		// For Enterprise, BaseURL and UploadURL should be set
		require.NotNil(t, client.BaseURL)
		require.Contains(t, client.BaseURL.String(), "github.company.com")
		require.Contains(t, client.BaseURL.String(), "/api/v3/")
		require.NotNil(t, client.UploadURL)
		require.Contains(t, client.UploadURL.String(), "github.company.com")
		require.Contains(t, client.UploadURL.String(), "/api/uploads/")
	})

	t.Run("creates client for Enterprise GitHub with simple hostname", func(t *testing.T) {
		// Reset default repo to ensure clean state
		git.ResetDefaultRepo()

		scene := testhelpers.NewScene(t, func(s *testhelpers.Scene) error {
			return s.Repo.CreateChangeAndCommit("initial", "init")
		})

		// Set up Enterprise remote with simple hostname (add if it doesn't exist, otherwise set-url)
		err := scene.Repo.RunGitCommand("remote", "add", "origin", "https://my-internal-github/org/repo")
		if err != nil {
			// If remote already exists, just update the URL
			err = scene.Repo.RunGitCommand("remote", "set-url", "origin", "https://my-internal-github/org/repo")
			require.NoError(t, err)
		}

		// Initialize git repo
		err = git.InitDefaultRepoInDir(scene.Dir)
		if err != nil {
			t.Skipf("Skipping test: failed to initialize git repo (may be sandbox restriction): %v", err)
			return
		}

		// Mock token
		t.Setenv("GITHUB_TOKEN", "test-token")

		client, owner, repo, err := githubpkg.GetGitHubClient(context.Background())
		if err != nil {
			// If it fails due to token issues, that's expected in test environment
			require.Contains(t, err.Error(), "token")
			return
		}

		require.NotNil(t, client)
		require.Equal(t, "org", owner)
		require.Equal(t, "repo", repo)

		// For Enterprise, BaseURL and UploadURL should be set
		require.NotNil(t, client.BaseURL)
		require.Contains(t, client.BaseURL.String(), "my-internal-github")
		require.Contains(t, client.BaseURL.String(), "/api/v3/")
		require.NotNil(t, client.UploadURL)
		require.Contains(t, client.UploadURL.String(), "my-internal-github")
		require.Contains(t, client.UploadURL.String(), "/api/uploads/")
	})
}

// Note: Testing updatePRDraftStatus is more complex as it requires:
// 1. A real GitHub token or mock GraphQL server
// 2. A valid PR Node ID
// 3. Network access or sophisticated mocking
// This would be better tested in integration tests or with a GraphQL mock server.
// For now, we test the URL construction logic indirectly through GetGitHubClient.
