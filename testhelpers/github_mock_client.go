package testhelpers

import (
	"context"
	"sync"

	"github.com/google/go-github/v62/github"

	githubpkg "stackit.dev/stackit/internal/github"
	"stackit.dev/stackit/internal/utils"
)

// MockGitHubClient implements githubpkg.Client using the mock server
type MockGitHubClient struct {
	client *github.Client
	owner  string
	repo   string
	config *MockGitHubServerConfig
}

// NewMockGitHubClientInterface creates a GitHubClient interface implementation
// using the mock server
func NewMockGitHubClientInterface(client *github.Client, owner, repo string, config *MockGitHubServerConfig) githubpkg.Client {
	return &MockGitHubClient{
		client: client,
		owner:  owner,
		repo:   repo,
		config: config,
	}
}

// GetOwnerRepo returns the repository owner and name
func (c *MockGitHubClient) GetOwnerRepo() (string, string) {
	return c.owner, c.repo
}

// CreatePullRequest creates a new pull request
func (c *MockGitHubClient) CreatePullRequest(ctx context.Context, owner, repo string, opts githubpkg.CreatePROptions) (*githubpkg.PullRequestInfo, error) {
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
		return nil, err
	}

	return githubpkg.ToPullRequestInfo(createdPR), nil
}

// UpdatePullRequest updates an existing pull request
func (c *MockGitHubClient) UpdatePullRequest(ctx context.Context, owner, repo string, prNumber int, opts githubpkg.UpdatePROptions) error {
	update := &github.PullRequest{}

	if opts.Title != nil {
		update.Title = opts.Title
	}
	if opts.Body != nil {
		update.Body = opts.Body
	}
	if opts.Base != nil {
		update.Base = &github.PullRequestBranch{
			Ref: opts.Base,
		}
	}

	_, _, err := c.client.PullRequests.Edit(ctx, owner, repo, prNumber, update)
	return err
}

// GetPullRequestByBranch gets a pull request for a branch
func (c *MockGitHubClient) GetPullRequestByBranch(ctx context.Context, owner, repo, branchName string) (*githubpkg.PullRequestInfo, error) {
	prs, _, err := c.client.PullRequests.List(ctx, owner, repo, &github.PullRequestListOptions{
		Head:  owner + ":" + branchName,
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

	return githubpkg.ToPullRequestInfo(prs[0]), nil
}

// GetPullRequest gets a pull request by number
func (c *MockGitHubClient) GetPullRequest(ctx context.Context, owner, repo string, prNumber int) (*githubpkg.PullRequestInfo, error) {
	pr, _, err := c.client.PullRequests.Get(ctx, owner, repo, prNumber)
	if err != nil {
		return nil, err
	}

	return githubpkg.ToPullRequestInfo(pr), nil
}

// MergePullRequest merges a pull request using the specified merge method
func (c *MockGitHubClient) MergePullRequest(_ context.Context, _ string, _ githubpkg.MergeMethod) error {
	// In tests, just return nil
	return nil
}

// GetAllowedMergeMethods returns the allowed merge methods for the repository
func (c *MockGitHubClient) GetAllowedMergeMethods(_ context.Context) (*githubpkg.MergeMethodSettings, error) {
	// In tests, allow all merge methods by default
	return &githubpkg.MergeMethodSettings{
		AllowMergeCommit: true,
		AllowSquashMerge: true,
		AllowRebaseMerge: true,
	}, nil
}

// getPRChecksStatus returns the check status for a PR
func (c *MockGitHubClient) getPRChecksStatus(_ context.Context, _ string) *githubpkg.CheckStatus {
	// In tests, always return passing
	return &githubpkg.CheckStatus{
		Passing: true,
		Pending: false,
		Checks: []githubpkg.CheckDetail{
			{Name: "Mock Check", Status: "COMPLETED", Conclusion: "SUCCESS"},
		},
	}
}

// GetPRChecksStatus returns the check status for a PR
func (c *MockGitHubClient) GetPRChecksStatus(ctx context.Context, branchName string) (*githubpkg.CheckStatus, error) {
	statuses, err := c.BatchGetPRChecksStatus(ctx, []string{branchName})
	if err != nil {
		return nil, err
	}
	return statuses[branchName], nil
}

// BatchGetPRChecksStatus returns the check status for multiple branches
func (c *MockGitHubClient) BatchGetPRChecksStatus(ctx context.Context, branchNames []string) (map[string]*githubpkg.CheckStatus, error) {
	results := make(map[string]*githubpkg.CheckStatus)
	var mu sync.Mutex

	utils.RunWithWorkers(branchNames, githubpkg.MaxGitHubConcurrency, func(name string) {
		status := c.getPRChecksStatus(ctx, name)
		mu.Lock()
		results[name] = status
		mu.Unlock()
	})

	return results, nil
}
