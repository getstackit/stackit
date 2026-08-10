// Package github provides a client for interacting with the GitHub API.
package github

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/google/go-github/v90/github"
	"golang.org/x/oauth2"

	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/utils"
)

// SyncPrInfo syncs PR information for branches from GitHub. It fetches every
// branch's PR in a single GraphQL query rather than one REST call per branch.
func SyncPrInfo(ctx context.Context, runner GitCommandRunner, branchNames []string, repo Repo, onUpdate func(string, *PullRequestInfo)) error {
	return syncPrInfo(ctx, runner, branchNames, repo, nil, onUpdate)
}

// SyncPrInfoWithKnownPRNumbers also looks up stored PR numbers when a branch
// no longer resolves on GitHub. GitHub removes the head ref when a user closes
// a PR and chooses to delete its branch, but the PR itself remains available by
// number. Looking it up preserves the normal closed-PR cleanup path without
// treating a missing remote branch alone as permission to delete local work.
func SyncPrInfoWithKnownPRNumbers(ctx context.Context, runner GitCommandRunner, branchNames []string, repo Repo, knownPRNumbers map[string]int, onUpdate func(string, *PullRequestInfo)) error {
	return syncPrInfo(ctx, runner, branchNames, repo, knownPRNumbers, onUpdate)
}

func syncPrInfo(ctx context.Context, runner GitCommandRunner, branchNames []string, repo Repo, knownPRNumbers map[string]int, onUpdate func(string, *PullRequestInfo)) error {
	if len(branchNames) == 0 {
		return nil
	}

	// If no token, skip PR syncing (non-fatal).
	token, err := getGitHubToken(runner)
	if err != nil {
		return nil //nolint:nilerr
	}

	// Resolve repository info. Even when owner/repo are provided, the hostname is
	// needed for the REST fallback on GitHub Enterprise.
	remoteRepo, err := getRemoteRepository(ctx, runner)
	if err != nil {
		return nil //nolint:nilerr // Skip if can't determine repo
	}
	if repo.Owner == "" || repo.Name == "" {
		repo = Repo{Owner: remoteRepo.Owner, Name: remoteRepo.Name}
	}

	infos, err := batchGetPRInfoByBranchGraphQL(ctx, runner, repo, branchNames)
	if err == nil {
		// A deleted head ref is returned as null and therefore absent from infos.
		// Resolve only those branches through their locally recorded PR number.
		// This is best-effort: a lookup failure must not discard successful
		// branch-based updates or make sync destructive.
		_ = supplementPRInfoByNumberGraphQL(ctx, runner, repo, infos, knownPRNumbers)
		for name, info := range infos {
			if info != nil && onUpdate != nil {
				onUpdate(name, info)
			}
		}
		return nil
	}

	client, err := createGitHubClient(ctx, remoteRepo.Host, token)
	if err != nil {
		return nil //nolint:nilerr // Preserve best-effort PR sync behavior.
	}

	utils.Run(branchNames, func(name string) {
		pr, err := getPRInfoForBranch(ctx, client, repo, name)
		if err != nil || pr == nil || onUpdate == nil {
			return
		}
		onUpdate(name, ToPullRequestInfo(pr))
	})

	return nil
}

// getPRInfoForBranch gets PR info for a branch using the REST API. This is the
// compatibility fallback when the batched GraphQL sync fails.
func getPRInfoForBranch(ctx context.Context, client *github.Client, repo Repo, branchName string) (*github.PullRequest, error) {
	prs, _, err := client.PullRequests.List(ctx, repo.Owner, repo.Name, &github.PullRequestListOptions{
		Head:  fmt.Sprintf("%s:%s", repo.Owner, branchName),
		State: prStateAll,
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

	opts := []github.ClientOptionsFunc{github.WithHTTPClient(tc)}

	// Configure for GitHub Enterprise if not github.com
	if hostname != "github.com" {
		opts = append(opts, github.WithEnterpriseURLs(
			fmt.Sprintf("https://%s/api/v3/", hostname),
			fmt.Sprintf("https://%s/api/uploads/", hostname),
		))
	}

	client, err := github.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("create github client for %s: %w", hostname, err)
	}
	return client, nil
}

// getGitHubToken gets GitHub token from environment or gh CLI
func getGitHubToken(runner GitCommandRunner) (string, error) {
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

// getRemoteRepository gets the hosted repository identity from the default
// Git remote. GitHub-specific callers retain their established error messages
// while sharing the canonical parser in the git package.
func getRemoteRepository(_ context.Context, runner GitCommandRunner) (git.RemoteRepository, error) {
	// Get remote URL
	remoteURL, err := runner.GetConfig("remote.origin.url")
	if err != nil {
		return git.RemoteRepository{}, fmt.Errorf("failed to get remote URL: %w", err)
	}

	repo, err := git.ParseRemoteRepository(remoteURL)
	if err != nil {
		if strings.HasPrefix(strings.TrimSpace(remoteURL), "http://") || strings.HasPrefix(strings.TrimSpace(remoteURL), "https://") {
			return git.RemoteRepository{}, fmt.Errorf("invalid HTTPS remote URL: %w", err)
		}
		return git.RemoteRepository{}, err
	}
	if !repo.IsHosted() {
		return git.RemoteRepository{}, fmt.Errorf("failed to parse hostname, owner, or repo from remote URL")
	}
	return repo, nil
}
