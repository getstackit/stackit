// Package github provides a client for interacting with the GitHub API.
package github

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/google/go-github/v62/github"
	"golang.org/x/oauth2"

	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/utils"
)

// SyncPrInfo syncs PR information for branches from GitHub
func SyncPrInfo(ctx context.Context, runner git.Runner, branchNames []string, repoOwner, repoName string, onUpdate func(string, *PullRequestInfo)) error {
	// Get GitHub token
	token, err := getGitHubToken(runner)
	if err != nil {
		// If no token, skip PR syncing (non-fatal)
		return nil //nolint:nilerr
	}

	// Get repository info if not provided
	var repoInfo *RepoInfo
	if repoOwner == "" || repoName == "" {
		repoInfo, err = getRepoInfoWithHostname(ctx, runner)
		if err != nil {
			return nil //nolint:nilerr // Skip if can't determine repo
		}
		repoOwner = repoInfo.Owner
		repoName = repoInfo.Repo
	} else {
		// Still need hostname for client configuration
		repoInfo, err = getRepoInfoWithHostname(ctx, runner)
		if err != nil {
			return nil //nolint:nilerr // Skip if can't determine repo
		}
	}

	// Create GitHub client with Enterprise support
	client, err := createGitHubClient(ctx, repoInfo.Hostname, token)
	if err != nil {
		return nil //nolint:nilerr // Skip if can't create client
	}

	// Fetch PR info for each branch in parallel using a worker pool
	if len(branchNames) == 0 {
		return nil
	}

	utils.Run(branchNames, func(name string) {
		pr, err := getPRInfoForBranch(ctx, client, repoOwner, repoName, name)
		if err != nil {
			return
		}

		if pr != nil {
			info := ToPullRequestInfo(pr)
			if onUpdate != nil {
				onUpdate(name, info)
			}
		}
	})

	return nil
}

// getPRInfoForBranch gets PR info for a branch
func getPRInfoForBranch(ctx context.Context, client *github.Client, owner, repo, branchName string) (*github.PullRequest, error) {
	// List PRs for this branch
	prs, _, err := client.PullRequests.List(ctx, owner, repo, &github.PullRequestListOptions{
		Head:  fmt.Sprintf("%s:%s", owner, branchName),
		State: "all",
		ListOptions: github.ListOptions{
			PerPage: 1,
		},
	})
	if err != nil {
		return nil, err
	}

	if len(prs) == 0 {
		return nil, nil
	}

	return prs[0], nil
}

// createGitHubClient creates a GitHub client configured for the given hostname
// Supports both github.com and GitHub Enterprise instances
func createGitHubClient(ctx context.Context, hostname, token string) (*github.Client, error) {
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)

	// Configure for GitHub Enterprise if not github.com
	if hostname != "github.com" {
		// GitHub Enterprise API endpoints
		// REST API: https://hostname/api/v3/
		// Upload API: https://hostname/api/uploads/
		baseURL, err := url.Parse(fmt.Sprintf("https://%s/api/v3/", hostname))
		if err != nil {
			return nil, fmt.Errorf("failed to parse base URL for hostname %s: %w", hostname, err)
		}
		uploadURL, err := url.Parse(fmt.Sprintf("https://%s/api/uploads/", hostname))
		if err != nil {
			return nil, fmt.Errorf("failed to parse upload URL for hostname %s: %w", hostname, err)
		}

		client.BaseURL = baseURL
		client.UploadURL = uploadURL
	}
	// For github.com, the default URLs are already correct

	return client, nil
}

// getGitHubToken gets GitHub token from environment or gh CLI
func getGitHubToken(runner git.Runner) (string, error) {
	// Try environment variable first
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		// Trim whitespace to handle cases where secrets might have leading/trailing spaces
		token = strings.TrimSpace(token)
		if token != "" {
			return token, nil
		}
	}

	// Try gh CLI
	output, err := runner.RunGHCommandWithContext(context.Background(), "auth", "token")
	if err != nil {
		return "", fmt.Errorf("failed to get GitHub token: %w", err)
	}

	token := strings.TrimSpace(output)
	if token == "" {
		return "", fmt.Errorf("empty GitHub token")
	}

	return token, nil
}

// RepoInfo contains parsed information from a git remote URL
type RepoInfo struct {
	Hostname string
	Owner    string
	Repo     string
}

// ParseGitHubRemoteURL parses a git remote URL and extracts hostname, owner, and repo
// Supports both github.com and GitHub Enterprise URLs
// Examples:
//   - https://github.com/owner/repo.git
//   - git@github.com:owner/repo.git
//   - https://github.company.com/owner/repo.git
//   - git@github.company.com:owner/repo.git
func ParseGitHubRemoteURL(remoteURL string) (*RepoInfo, error) {
	remoteURL = strings.TrimSpace(remoteURL)
	remoteURL = strings.TrimSuffix(remoteURL, ".git")

	var hostname, owner, repo string

	if strings.Contains(remoteURL, "@") {
		// SSH format: git@hostname:owner/repo or git@hostname/owner/repo
		// Split on @ first
		parts := strings.SplitN(remoteURL, "@", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid SSH remote URL format")
		}

		// Get hostname and path
		hostAndPath := parts[1]

		// Handle both : and / separators after hostname
		var path string
		if strings.Contains(hostAndPath, ":") {
			// Format: git@hostname:owner/repo
			hostPathParts := strings.SplitN(hostAndPath, ":", 2)
			hostname = hostPathParts[0]
			path = hostPathParts[1]
		} else {
			// Format: git@hostname/owner/repo (less common)
			pathParts := strings.SplitN(hostAndPath, "/", 2)
			if len(pathParts) < 2 {
				return nil, fmt.Errorf("invalid SSH remote URL: missing path")
			}
			hostname = pathParts[0]
			path = pathParts[1]
		}

		// Parse owner/repo from path
		pathParts := strings.Split(path, "/")
		if len(pathParts) < 2 {
			return nil, fmt.Errorf("invalid SSH remote URL: path must be owner/repo")
		}
		owner = pathParts[0]
		repo = pathParts[len(pathParts)-1]
	} else {
		// HTTPS format: https://hostname/owner/repo
		// Remove protocol
		remoteURL = strings.TrimPrefix(remoteURL, "https://")
		remoteURL = strings.TrimPrefix(remoteURL, "http://")

		parts := strings.Split(remoteURL, "/")
		if len(parts) < 3 {
			return nil, fmt.Errorf("invalid HTTPS remote URL: must be protocol://hostname/owner/repo")
		}

		hostname = parts[0]
		owner = parts[len(parts)-2]
		repo = parts[len(parts)-1]
	}

	if hostname == "" || owner == "" || repo == "" {
		return nil, fmt.Errorf("failed to parse hostname, owner, or repo from remote URL")
	}

	return &RepoInfo{
		Hostname: hostname,
		Owner:    owner,
		Repo:     repo,
	}, nil
}

// getRepoInfoWithHostname gets repository hostname, owner, and name from git remote
func getRepoInfoWithHostname(_ context.Context, runner git.Runner) (*RepoInfo, error) {
	// Get remote URL
	remoteURL, err := runner.GetConfig("remote.origin.url")
	if err != nil {
		return nil, fmt.Errorf("failed to get remote URL: %w", err)
	}

	return ParseGitHubRemoteURL(remoteURL)
}
