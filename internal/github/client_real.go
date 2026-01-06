// Package github provides a client for interacting with the GitHub API.
package github

import (
	"context"
	"fmt"

	"github.com/google/go-github/v62/github"

	"stackit.dev/stackit/internal/git"
)

// StackitGitHubClient implements Client using the real GitHub API
type StackitGitHubClient struct {
	client *github.Client
	runner git.Runner
	owner  string
	repo   string
}

// NewGitHubClient creates a new RealGitHubClient
func NewGitHubClient(ctx context.Context, runner git.Runner) (*StackitGitHubClient, error) {
	token, err := getGitHubToken(runner)
	if err != nil {
		return nil, fmt.Errorf("failed to get GitHub token: %w", err)
	}

	repoInfo, err := getRepoInfoWithHostname(ctx, runner)
	if err != nil {
		return nil, fmt.Errorf("failed to get repository info: %w", err)
	}

	client, err := createGitHubClient(ctx, repoInfo.Hostname, token)
	if err != nil {
		return nil, fmt.Errorf("failed to create GitHub client: %w", err)
	}

	return &StackitGitHubClient{
		client: client,
		runner: runner,
		owner:  repoInfo.Owner,
		repo:   repoInfo.Repo,
	}, nil
}

// GetOwnerRepo returns the repository owner and name
func (c *StackitGitHubClient) GetOwnerRepo() (string, string) {
	return c.owner, c.repo
}

// CreatePullRequest creates a new pull request
func (c *StackitGitHubClient) CreatePullRequest(ctx context.Context, owner, repo string, opts CreatePROptions) (*PullRequestInfo, error) {
	pr := &github.NewPullRequest{
		Title: github.String(opts.Title),
		Head:  github.String(opts.Head),
		Base:  github.String(opts.Base),
		Draft: github.Bool(opts.Draft),
	}

	if opts.Body != "" {
		pr.Body = github.String(opts.Body)
	}

	createdPR, _, err := c.client.PullRequests.Create(ctx, owner, repo, pr)
	if err != nil {
		return nil, fmt.Errorf("failed to create pull request: %w", err)
	}

	// Add reviewers if specified
	if len(opts.Reviewers) > 0 || len(opts.TeamReviewers) > 0 {
		_, _, _ = c.client.PullRequests.RequestReviewers(ctx, owner, repo, *createdPR.Number, github.ReviewersRequest{
			Reviewers:     opts.Reviewers,
			TeamReviewers: opts.TeamReviewers,
		})
	}

	return ToPullRequestInfo(createdPR), nil
}

// UpdatePullRequest updates an existing pull request
func (c *StackitGitHubClient) UpdatePullRequest(ctx context.Context, owner, repo string, prNumber int, opts UpdatePROptions) error {
	return UpdatePullRequest(ctx, c.client, c.runner, owner, repo, prNumber, opts)
}

// GetPullRequestByBranch gets a pull request for a branch
func (c *StackitGitHubClient) GetPullRequestByBranch(ctx context.Context, owner, repo, branchName string) (*PullRequestInfo, error) {
	prs, _, err := c.client.PullRequests.List(ctx, owner, repo, &github.PullRequestListOptions{
		Head:  fmt.Sprintf("%s:%s", owner, branchName),
		State: "all",
		ListOptions: github.ListOptions{
			PerPage: 1,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list pull requests: %w", err)
	}

	if len(prs) == 0 {
		return nil, nil
	}

	return ToPullRequestInfo(prs[0]), nil
}

// GetPullRequest gets a pull request by number
func (c *StackitGitHubClient) GetPullRequest(ctx context.Context, owner, repo string, prNumber int) (*PullRequestInfo, error) {
	pr, _, err := c.client.PullRequests.Get(ctx, owner, repo, prNumber)
	if err != nil {
		return nil, fmt.Errorf("failed to get pull request %d: %w", prNumber, err)
	}

	return ToPullRequestInfo(pr), nil
}

// MergePullRequest merges a pull request using the specified merge method
func (c *StackitGitHubClient) MergePullRequest(ctx context.Context, branchName string, method MergeMethod) error {
	return MergePullRequest(ctx, c.client, c.owner, c.repo, branchName, method)
}

// GetAllowedMergeMethods returns the allowed merge methods for the repository
func (c *StackitGitHubClient) GetAllowedMergeMethods(ctx context.Context) (*MergeMethodSettings, error) {
	repo, _, err := c.client.Repositories.Get(ctx, c.owner, c.repo)
	if err != nil {
		return nil, fmt.Errorf("failed to get repository settings: %w", err)
	}

	return &MergeMethodSettings{
		AllowMergeCommit: repo.GetAllowMergeCommit(),
		AllowSquashMerge: repo.GetAllowSquashMerge(),
		AllowRebaseMerge: repo.GetAllowRebaseMerge(),
	}, nil
}

// GetPRChecksStatus returns the check status for a single branch
func (c *StackitGitHubClient) GetPRChecksStatus(ctx context.Context, branchName string) (*CheckStatus, error) {
	statuses, err := c.BatchGetPRChecksStatus(ctx, []string{branchName})
	if err != nil {
		return nil, err
	}
	return statuses[branchName], nil
}

// BatchGetPRChecksStatus returns the check status for multiple branches
func (c *StackitGitHubClient) BatchGetPRChecksStatus(ctx context.Context, branchNames []string) (map[string]*CheckStatus, error) {
	// Use GraphQL for efficiency and rate limit safety
	return BatchGetPRChecksStatusGraphQL(ctx, c.runner, c.owner, c.repo, branchNames)
}
