// Package github provides a client for interacting with the GitHub API.
package github

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/go-github/v89/github"
)

// StackitGitHubClient implements Client using the real GitHub API
type StackitGitHubClient struct {
	client *github.Client
	runner GitCommandRunner
	repo   Repo
}

// NewGitHubClient creates a new RealGitHubClient
func NewGitHubClient(ctx context.Context, runner GitCommandRunner) (*StackitGitHubClient, error) {
	token, err := getGitHubToken(runner)
	if err != nil {
		return nil, fmt.Errorf("failed to get GitHub token: %w", err)
	}

	repo, err := getRemoteRepository(ctx, runner)
	if err != nil {
		return nil, fmt.Errorf("failed to get repository info: %w", err)
	}

	client, err := createGitHubClient(ctx, repo.Host, token)
	if err != nil {
		return nil, fmt.Errorf("failed to create GitHub client: %w", err)
	}

	return &StackitGitHubClient{
		client: client,
		runner: runner,
		repo:   Repo{Owner: repo.Owner, Name: repo.Name},
	}, nil
}

// Repo returns the repository the client is bound to
func (c *StackitGitHubClient) Repo() Repo {
	return c.repo
}

// CreatePullRequest creates a new pull request
func (c *StackitGitHubClient) CreatePullRequest(ctx context.Context, opts CreatePROptions) (*PullRequestInfo, error) {
	warnings, createdPR, err := CreatePullRequest(ctx, c.client, c.repo, opts)
	if err != nil {
		return nil, err
	}
	result := ToPullRequestInfo(createdPR)
	result.Warnings = warnings
	return result, nil
}

// UpdatePullRequest updates an existing pull request
func (c *StackitGitHubClient) UpdatePullRequest(ctx context.Context, prNumber int, opts UpdatePROptions) ([]string, error) {
	return UpdatePullRequest(ctx, c.client, c.runner, c.repo, prNumber, opts)
}

// GetPullRequestByBranch gets a pull request for a branch
func (c *StackitGitHubClient) GetPullRequestByBranch(ctx context.Context, branchName string) (*PullRequestInfo, error) {
	prs, _, err := c.client.PullRequests.List(ctx, c.repo.Owner, c.repo.Name, &github.PullRequestListOptions{
		Head:  fmt.Sprintf("%s:%s", c.repo.Owner, branchName),
		State: prStateAll,
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
func (c *StackitGitHubClient) GetPullRequest(ctx context.Context, prNumber int) (*PullRequestInfo, error) {
	pr, _, err := c.client.PullRequests.Get(ctx, c.repo.Owner, c.repo.Name, prNumber)
	if err != nil {
		return nil, fmt.Errorf("failed to get pull request %d: %w", prNumber, err)
	}

	return ToPullRequestInfo(pr), nil
}

// MergePullRequest merges a pull request using the specified merge method
func (c *StackitGitHubClient) MergePullRequest(ctx context.Context, branchName string, opts MergePROptions) error {
	return MergePullRequest(ctx, c.client, c.repo, branchName, opts)
}

// GetAllowedMergeMethods returns the allowed merge methods for the repository
func (c *StackitGitHubClient) GetAllowedMergeMethods(ctx context.Context) (*MergeMethodSettings, error) {
	repo, _, err := c.client.Repositories.Get(ctx, c.repo.Owner, c.repo.Name)
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
func (c *StackitGitHubClient) BatchGetPRChecksStatus(ctx context.Context, branchNames []string) (ChecksByBranch, error) {
	// Use GraphQL for efficiency and rate limit safety
	return BatchGetPRChecksStatusGraphQL(ctx, c.runner, c.repo, branchNames)
}

// BatchGetPRTitles returns titles for multiple PRs by number
func (c *StackitGitHubClient) BatchGetPRTitles(ctx context.Context, prNumbers []int) (map[int]string, error) {
	return BatchGetPRTitlesGraphQL(ctx, c.runner, c.repo, prNumbers)
}

// ClosePullRequest closes a pull request
func (c *StackitGitHubClient) ClosePullRequest(ctx context.Context, prNumber int) error {
	state := "closed"
	_, _, err := c.client.PullRequests.Edit(ctx, c.repo.Owner, c.repo.Name, prNumber, &github.PullRequest{State: &state})
	if err != nil {
		return fmt.Errorf("failed to close PR #%d: %w", prNumber, err)
	}
	return nil
}

// CreatePRComment creates a new comment on a pull request
func (c *StackitGitHubClient) CreatePRComment(ctx context.Context, prNumber int, body string) (int64, error) {
	comment, _, err := c.client.Issues.CreateComment(ctx, c.repo.Owner, c.repo.Name, prNumber, &github.IssueComment{
		Body: new(body),
	})
	if err != nil {
		return 0, fmt.Errorf("failed to create comment on PR #%d: %w", prNumber, err)
	}
	return comment.GetID(), nil
}

// UpdatePRComment updates an existing pull request comment
func (c *StackitGitHubClient) UpdatePRComment(ctx context.Context, commentID int64, body string) error {
	_, _, err := c.client.Issues.EditComment(ctx, c.repo.Owner, c.repo.Name, commentID, &github.IssueComment{
		Body: new(body),
	})
	if err != nil {
		return fmt.Errorf("failed to update comment %d: %w", commentID, err)
	}
	return nil
}

// DeletePRComment deletes a pull request comment
func (c *StackitGitHubClient) DeletePRComment(ctx context.Context, commentID int64) error {
	_, err := c.client.Issues.DeleteComment(ctx, c.repo.Owner, c.repo.Name, commentID)
	if err != nil {
		return fmt.Errorf("failed to delete comment %d: %w", commentID, err)
	}
	return nil
}

// ListPRComments lists all comments on a pull request with pagination
func (c *StackitGitHubClient) ListPRComments(ctx context.Context, prNumber int) ([]PRComment, error) {
	var allComments []PRComment
	opts := &github.IssueListCommentsOptions{
		ListOptions: github.ListOptions{
			PerPage: 100,
		},
	}

	for {
		comments, resp, err := c.client.Issues.ListComments(ctx, c.repo.Owner, c.repo.Name, prNumber, opts)
		if err != nil {
			return nil, fmt.Errorf("failed to list comments on PR #%d: %w", prNumber, err)
		}

		for _, comment := range comments {
			allComments = append(allComments, PRComment{
				ID:   comment.GetID(),
				Body: comment.GetBody(),
			})
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return allComments, nil
}

// GetCurrentUser returns the authenticated GitHub username
func (c *StackitGitHubClient) GetCurrentUser(ctx context.Context) (string, error) {
	output, err := c.runner.RunGHCommandWithContext(ctx, "api", "user", "-q", ".login")
	if err != nil {
		return "", fmt.Errorf("failed to get current GitHub user: %w", err)
	}
	return strings.TrimSpace(output), nil
}
