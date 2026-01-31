package demo

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/github"
	"stackit.dev/stackit/internal/utils"
)

// prCounter is used to generate unique PR numbers
var prCounter int32 = 100

func init() {
	// Register the demo GitHub client factory with runtime package
	app.DemoGitHubClientFactory = func() github.Client {
		return NewDemoGitHubClient()
	}
}

// GitHubClient implements github.Client for demo mode
type GitHubClient struct {
	owner string
	repo  string
	// prs stores PR info by branch name
	prs map[string]*github.PullRequestInfo
}

// NewDemoGitHubClient creates a new demo GitHub client
func NewDemoGitHubClient() *GitHubClient {
	return &GitHubClient{
		owner: "example",
		repo:  "repo",
		prs:   make(map[string]*github.PullRequestInfo),
	}
}

// GetOwnerRepo returns the repository owner and name
func (c *GitHubClient) GetOwnerRepo() (string, string) {
	return c.owner, c.repo
}

// CreatePullRequest creates a simulated pull request
func (c *GitHubClient) CreatePullRequest(_ context.Context, owner, repo string, opts github.CreatePROptions) (*github.PullRequestInfo, error) {
	simulateDelay(delayMedium)

	prNum := int(atomic.AddInt32(&prCounter, 1))
	pr := &github.PullRequestInfo{
		Number:  prNum,
		NodeID:  fmt.Sprintf("PR_%d", prNum),
		HTMLURL: fmt.Sprintf("https://github.com/%s/%s/pull/%d", owner, repo, prNum),
		Title:   opts.Title,
		Body:    opts.Body,
		State:   "open",
		Draft:   opts.Draft,
		Base:    opts.Base,
		Head:    opts.Head,
	}

	c.prs[opts.Head] = pr
	return pr, nil
}

// UpdatePullRequest simulates updating a pull request
func (c *GitHubClient) UpdatePullRequest(_ context.Context, _, _ string, prNumber int, opts github.UpdatePROptions) ([]string, error) {
	simulateDelay(delayShort)

	// Find the PR by number
	for _, pr := range c.prs {
		if pr.Number == prNumber {
			if opts.Title != nil {
				pr.Title = *opts.Title
			}
			if opts.Body != nil {
				pr.Body = *opts.Body
			}
			if opts.Base != nil {
				pr.Base = *opts.Base
			}
			if opts.Draft != nil {
				pr.Draft = *opts.Draft
			}
			return nil, nil
		}
	}

	return nil, nil
}

// GetPullRequestByBranch returns a simulated PR for a branch
func (c *GitHubClient) GetPullRequestByBranch(_ context.Context, _, _, branchName string) (*github.PullRequestInfo, error) {
	simulateDelay(delayShort)

	if pr, ok := c.prs[branchName]; ok {
		return pr, nil
	}
	return nil, nil
}

// GetPullRequest returns a simulated PR by number
func (c *GitHubClient) GetPullRequest(_ context.Context, _, _ string, prNumber int) (*github.PullRequestInfo, error) {
	simulateDelay(delayShort)

	for _, pr := range c.prs {
		if pr.Number == prNumber {
			return pr, nil
		}
	}
	return nil, fmt.Errorf("PR #%d not found", prNumber)
}

// MergePullRequest simulates merging a pull request using the specified merge method
func (c *GitHubClient) MergePullRequest(_ context.Context, branchName string, _ github.MergeMethod) error {
	simulateDelay(delayMedium)

	if pr, ok := c.prs[branchName]; ok {
		pr.State = "closed"
	}
	return nil
}

// GetAllowedMergeMethods returns simulated allowed merge methods
func (c *GitHubClient) GetAllowedMergeMethods(_ context.Context) (*github.MergeMethodSettings, error) {
	simulateDelay(delayShort)

	// In demo mode, allow all merge methods
	return &github.MergeMethodSettings{
		AllowMergeCommit: true,
		AllowSquashMerge: true,
		AllowRebaseMerge: true,
	}, nil
}

// getPRChecksStatus returns simulated check status
func (c *GitHubClient) getPRChecksStatus(_ context.Context, _ string) *github.CheckStatus {
	// Simulate a small delay
	time.Sleep(50 * time.Millisecond)

	// In demo mode, always return checks passing with approved review
	return &github.CheckStatus{
		Passing:        true,
		Pending:        false,
		ReviewDecision: "APPROVED",
		Checks: []github.CheckDetail{
			{Name: "Build", Status: "COMPLETED", Conclusion: "SUCCESS"},
			{Name: "Test", Status: "COMPLETED", Conclusion: "SUCCESS"},
			{Name: "Lint", Status: "COMPLETED", Conclusion: "SUCCESS"},
		},
	}
}

// GetPRChecksStatus returns simulated check status for a single branch
func (c *GitHubClient) GetPRChecksStatus(ctx context.Context, branchName string) (*github.CheckStatus, error) {
	statuses, err := c.BatchGetPRChecksStatus(ctx, []string{branchName})
	if err != nil {
		return nil, err
	}
	return statuses[branchName], nil
}

// BatchGetPRChecksStatus returns simulated check status for multiple branches
func (c *GitHubClient) BatchGetPRChecksStatus(ctx context.Context, branchNames []string) (map[string]*github.CheckStatus, error) {
	results := make(map[string]*github.CheckStatus)
	var mu sync.Mutex

	utils.RunWithWorkers(branchNames, github.MaxGitHubConcurrency, func(name string) {
		status := c.getPRChecksStatus(ctx, name)
		mu.Lock()
		results[name] = status
		mu.Unlock()
	})

	return results, nil
}

// ClosePullRequest simulates closing a pull request
func (c *GitHubClient) ClosePullRequest(_ context.Context, _, _ string, prNumber int) error {
	simulateDelay(delayShort)

	// Find the PR by number and close it
	for _, pr := range c.prs {
		if pr.Number == prNumber {
			pr.State = "closed"
			return nil
		}
	}

	return nil
}

// CreatePRComment simulates creating a PR comment
func (c *GitHubClient) CreatePRComment(_ context.Context, _, _ string, _ int, _ string) (int64, error) {
	simulateDelay(delayShort)
	// In demo mode, return a simulated comment ID
	return 12345, nil
}

// UpdatePRComment simulates updating a PR comment
func (c *GitHubClient) UpdatePRComment(_ context.Context, _, _ string, _ int64, _ string) error {
	simulateDelay(delayShort)
	return nil
}

// DeletePRComment simulates deleting a PR comment
func (c *GitHubClient) DeletePRComment(_ context.Context, _, _ string, _ int64) error {
	simulateDelay(delayShort)
	return nil
}

// ListPRComments simulates listing PR comments
func (c *GitHubClient) ListPRComments(_ context.Context, _, _ string, _ int) ([]github.PRComment, error) {
	simulateDelay(delayShort)
	// In demo mode, return empty list
	return []github.PRComment{}, nil
}

// GetCurrentUser returns a simulated GitHub username
func (c *GitHubClient) GetCurrentUser(_ context.Context) (string, error) {
	return "demouser", nil
}
