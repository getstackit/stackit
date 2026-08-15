package testhelpers

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/go-github/v90/github"

	githubpkg "github.com/getstackit/stackit/internal/github"
	"github.com/getstackit/stackit/internal/utils"
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

// Repo returns the repository the client is bound to
func (c *MockGitHubClient) Repo() githubpkg.Repo {
	return githubpkg.Repo{Owner: c.owner, Name: c.repo}
}

// CreateStack records native GitHub Stack creation for submit integration tests.
func (c *MockGitHubClient) CreateStack(_ context.Context, pullRequests []int) (*githubpkg.StackInfo, error) {
	c.config.mu.Lock()
	defer c.config.mu.Unlock()

	if c.config.StackError != nil {
		return nil, c.config.StackError
	}
	created := append([]int(nil), pullRequests...)
	c.config.CreatedStacks = append(c.config.CreatedStacks, created)
	return mockStackInfo(len(c.config.CreatedStacks), created), nil
}

// FindStackByPullRequest returns the mock native Stack containing pullRequest.
func (c *MockGitHubClient) FindStackByPullRequest(_ context.Context, pullRequest int) (*githubpkg.StackInfo, error) {
	c.config.mu.Lock()
	defer c.config.mu.Unlock()

	if c.config.StackError != nil {
		return nil, c.config.StackError
	}
	for i, stack := range c.config.CreatedStacks {
		if containsInt(stack, pullRequest) {
			return mockStackInfo(i+1, stack), nil
		}
	}
	return nil, nil
}

// AddPullRequestsToStack extends a mock native Stack in place.
func (c *MockGitHubClient) AddPullRequestsToStack(_ context.Context, stackNumber int, pullRequests []int) (*githubpkg.StackInfo, error) {
	c.config.mu.Lock()
	defer c.config.mu.Unlock()

	if stackNumber < 1 || stackNumber > len(c.config.CreatedStacks) {
		return nil, fmt.Errorf("GitHub Stack #%d does not exist", stackNumber)
	}
	c.config.CreatedStacks[stackNumber-1] = append(c.config.CreatedStacks[stackNumber-1], pullRequests...)
	return mockStackInfo(stackNumber, c.config.CreatedStacks[stackNumber-1]), nil
}

func mockStackInfo(number int, pullRequests []int) *githubpkg.StackInfo {
	stack := &githubpkg.StackInfo{Number: number, PullRequests: make([]githubpkg.StackPRInfo, len(pullRequests))}
	for i, pullRequest := range pullRequests {
		stack.PullRequests[i].Number = pullRequest
	}
	return stack
}

func containsInt(values []int, target int) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// CreatePullRequest creates a new pull request
func (c *MockGitHubClient) CreatePullRequest(ctx context.Context, opts githubpkg.CreatePROptions) (*githubpkg.PullRequestInfo, error) {
	pr := github.CreatePullRequest{
		Title: new(opts.Title),
		Head:  opts.Head,
		Base:  opts.Base,
		Draft: new(opts.Draft),
	}

	if opts.Body != "" {
		pr.Body = new(opts.Body)
	}

	createdPR, _, err := c.client.PullRequests.Create(ctx, c.owner, c.repo, pr)
	if err != nil {
		return nil, err
	}

	return githubpkg.ToPullRequestInfo(createdPR), nil
}

// UpdatePullRequest updates an existing pull request
func (c *MockGitHubClient) UpdatePullRequest(ctx context.Context, prNumber int, opts githubpkg.UpdatePROptions) ([]string, error) {
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

	_, _, err := c.client.PullRequests.Edit(ctx, c.owner, c.repo, prNumber, update)
	return nil, err
}

// GetPullRequestByBranch gets a pull request for a branch
func (c *MockGitHubClient) GetPullRequestByBranch(ctx context.Context, branchName string) (*githubpkg.PullRequestInfo, error) {
	prs, _, err := c.client.PullRequests.List(ctx, c.owner, c.repo, &github.PullRequestListOptions{
		Head:  c.owner + ":" + branchName,
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
func (c *MockGitHubClient) GetPullRequest(ctx context.Context, prNumber int) (*githubpkg.PullRequestInfo, error) {
	pr, _, err := c.client.PullRequests.Get(ctx, c.owner, c.repo, prNumber)
	if err != nil {
		return nil, err
	}

	return githubpkg.ToPullRequestInfo(pr), nil
}

// MergePullRequest merges a pull request using the specified merge method
func (c *MockGitHubClient) MergePullRequest(_ context.Context, _ string, _ githubpkg.MergePROptions) error {
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
func (c *MockGitHubClient) BatchGetPRChecksStatus(ctx context.Context, branchNames []string) (githubpkg.ChecksByBranch, error) {
	results := make(githubpkg.ChecksByBranch)
	var mu sync.Mutex

	utils.RunWithWorkers(branchNames, githubpkg.MaxGitHubConcurrency, func(name string) {
		status := c.getPRChecksStatus(ctx, name)
		mu.Lock()
		results[name] = status
		mu.Unlock()
	})

	return results, nil
}

// BatchGetPRTitles returns synthetic titles for testing
func (c *MockGitHubClient) BatchGetPRTitles(_ context.Context, prNumbers []int) (map[int]string, error) {
	results := make(map[int]string, len(prNumbers))
	for _, num := range prNumbers {
		results[num] = fmt.Sprintf("PR #%d title", num)
	}
	return results, nil
}

// ClosePullRequest closes a pull request
func (c *MockGitHubClient) ClosePullRequest(ctx context.Context, prNumber int) error {
	state := "closed"
	_, _, err := c.client.PullRequests.Edit(ctx, c.owner, c.repo, prNumber, &github.PullRequest{State: &state})
	return err
}

// CreatePRComment creates a new comment on a pull request
func (c *MockGitHubClient) CreatePRComment(ctx context.Context, prNumber int, body string) (int64, error) {
	comment, _, err := c.client.Issues.CreateComment(ctx, c.owner, c.repo, prNumber, &github.IssueComment{
		Body: new(body),
	})
	if err != nil {
		return 0, err
	}
	return comment.GetID(), nil
}

// UpdatePRComment updates an existing pull request comment
func (c *MockGitHubClient) UpdatePRComment(ctx context.Context, commentID int64, body string) error {
	_, _, err := c.client.Issues.EditComment(ctx, c.owner, c.repo, commentID, &github.IssueComment{
		Body: new(body),
	})
	return err
}

// DeletePRComment deletes a pull request comment
func (c *MockGitHubClient) DeletePRComment(ctx context.Context, commentID int64) error {
	_, err := c.client.Issues.DeleteComment(ctx, c.owner, c.repo, commentID)
	return err
}

// ListPRComments lists all comments on a pull request
func (c *MockGitHubClient) ListPRComments(ctx context.Context, prNumber int) ([]githubpkg.PRComment, error) {
	comments, _, err := c.client.Issues.ListComments(ctx, c.owner, c.repo, prNumber, &github.IssueListCommentsOptions{
		ListOptions: github.ListOptions{
			PerPage: 100,
		},
	})
	if err != nil {
		return nil, err
	}

	result := make([]githubpkg.PRComment, len(comments))
	for i, comment := range comments {
		result[i] = githubpkg.PRComment{
			ID:   comment.GetID(),
			Body: comment.GetBody(),
		}
	}
	return result, nil
}

// GetCurrentUser returns a mock GitHub username
func (c *MockGitHubClient) GetCurrentUser(_ context.Context) (string, error) {
	return "testuser", nil
}
