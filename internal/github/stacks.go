package github

import (
	"context"
	"fmt"
	"net/http"
	"slices"
)

const (
	// MinStackPullRequests is GitHub's minimum number of pull requests in one
	// native Stack.
	MinStackPullRequests = 2
	// MaxStackPullRequests is GitHub's maximum number of pull requests in one
	// native Stack.
	MaxStackPullRequests = 100
)

// StackClient is the narrow GitHub Stacks API surface used by the experimental
// Stackit command. It is deliberately separate from Client while the feature
// is being evaluated, so normal PR operations do not depend on the API.
type StackClient interface {
	CreateStack(ctx context.Context, pullRequests []int) (*StackInfo, error)
	FindStackByPullRequest(ctx context.Context, pullRequest int) (*StackInfo, error)
	AddPullRequestsToStack(ctx context.Context, stackNumber int, pullRequests []int) (*StackInfo, error)
}

// StackInfo is GitHub's server-side representation of a linear stack of pull
// requests. PullRequests are ordered from the bottom of the stack to the top.
type StackInfo struct {
	ID           int           `json:"id"`
	Number       int           `json:"number"`
	URL          string        `json:"url"`
	Base         StackBase     `json:"base"`
	PullRequests []StackPRInfo `json:"pull_requests"`
}

// StackBase is the ultimate base branch shared by the GitHub Stack.
type StackBase struct {
	Ref string `json:"ref"`
}

// StackPRInfo is the minimal pull request representation returned by GitHub's
// Stacks API.
type StackPRInfo struct {
	Number int `json:"number"`
	Head   struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"head"`
}

type createStackRequest struct {
	PullRequests []int `json:"pull_requests"`
}

// StackSyncAction describes how EnsureStack reconciled native Stack metadata.
type StackSyncAction string

const (
	StackSyncCreated   StackSyncAction = "created"
	StackSyncExtended  StackSyncAction = "extended"
	StackSyncUnchanged StackSyncAction = "unchanged"
)

// EnsureStack creates, extends, or leaves unchanged the native GitHub Stack
// containing the supplied bottom-to-top PR chain. Existing stacks may only be
// extended at the top; a divergent chain needs explicit user resolution.
func EnsureStack(ctx context.Context, client StackClient, pullRequests []int) (*StackInfo, StackSyncAction, error) {
	if err := ValidateStackPullRequestCount(len(pullRequests)); err != nil {
		return nil, "", err
	}

	existing, err := client.FindStackByPullRequest(ctx, pullRequests[0])
	if err != nil {
		return nil, "", err
	}
	if existing == nil {
		stack, err := client.CreateStack(ctx, pullRequests)
		return stack, StackSyncCreated, err
	}

	existingPRs := stackPullRequestNumbers(existing)
	if slices.Equal(existingPRs, pullRequests) || hasPrefix(existingPRs, pullRequests) {
		return existing, StackSyncUnchanged, nil
	}
	if hasPrefix(pullRequests, existingPRs) {
		stack, err := client.AddPullRequestsToStack(ctx, existing.Number, pullRequests[len(existingPRs):])
		return stack, StackSyncExtended, err
	}

	return nil, "", fmt.Errorf("GitHub Stack #%d has PRs %v, which do not match the submitted chain %v", existing.Number, existingPRs, pullRequests)
}

// ValidateStackPullRequestCount validates the GitHub Stacks API's request
// bounds before a caller performs any irreversible PR writes.
func ValidateStackPullRequestCount(count int) error {
	if count < MinStackPullRequests {
		return fmt.Errorf("a GitHub Stack requires at least two pull requests")
	}
	if count > MaxStackPullRequests {
		return fmt.Errorf("a GitHub Stack supports at most %d pull requests (got %d)", MaxStackPullRequests, count)
	}
	return nil
}

// CreateStack creates a native GitHub Stack from the supplied pull request
// numbers, ordered from bottom to top. GitHub validates that their base refs
// form a contiguous linear chain.
func (c *StackitGitHubClient) CreateStack(ctx context.Context, pullRequests []int) (*StackInfo, error) {
	if err := ValidateStackPullRequestCount(len(pullRequests)); err != nil {
		return nil, err
	}

	path := fmt.Sprintf("repos/%s/%s/stacks", c.repo.Owner, c.repo.Name)
	req, err := c.client.NewRequest(ctx, http.MethodPost, path, createStackRequest{PullRequests: pullRequests})
	if err != nil {
		return nil, fmt.Errorf("build GitHub Stack request: %w", err)
	}

	var stack StackInfo
	if _, err := c.client.Do(req, &stack); err != nil {
		return nil, fmt.Errorf("create GitHub Stack: %w", err)
	}
	return &stack, nil
}

// FindStackByPullRequest returns the native Stack containing pullRequest, if
// it belongs to one.
func (c *StackitGitHubClient) FindStackByPullRequest(ctx context.Context, pullRequest int) (*StackInfo, error) {
	path := fmt.Sprintf("repos/%s/%s/stacks?pull_request=%d", c.repo.Owner, c.repo.Name, pullRequest)
	req, err := c.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("build GitHub Stack lookup request: %w", err)
	}

	var stacks []StackInfo
	if _, err := c.client.Do(req, &stacks); err != nil {
		return nil, fmt.Errorf("find GitHub Stack for pull request %d: %w", pullRequest, err)
	}
	if len(stacks) == 0 {
		return nil, nil
	}
	return &stacks[0], nil
}

// AddPullRequestsToStack appends pull requests to the top of an existing
// native Stack. The PRs must be ordered from the current top upward.
func (c *StackitGitHubClient) AddPullRequestsToStack(ctx context.Context, stackNumber int, pullRequests []int) (*StackInfo, error) {
	if len(pullRequests) == 0 {
		return nil, fmt.Errorf("at least one pull request is required to extend a GitHub Stack")
	}

	path := fmt.Sprintf("repos/%s/%s/stacks/%d/add", c.repo.Owner, c.repo.Name, stackNumber)
	req, err := c.client.NewRequest(ctx, http.MethodPost, path, createStackRequest{PullRequests: pullRequests})
	if err != nil {
		return nil, fmt.Errorf("build GitHub Stack extension request: %w", err)
	}

	var stack StackInfo
	if _, err := c.client.Do(req, &stack); err != nil {
		return nil, fmt.Errorf("extend GitHub Stack #%d: %w", stackNumber, err)
	}
	return &stack, nil
}

func stackPullRequestNumbers(stack *StackInfo) []int {
	numbers := make([]int, len(stack.PullRequests))
	for i, pullRequest := range stack.PullRequests {
		numbers[i] = pullRequest.Number
	}
	return numbers
}

func hasPrefix(values, prefix []int) bool {
	return len(values) >= len(prefix) && slices.Equal(values[:len(prefix)], prefix)
}

var _ StackClient = (*StackitGitHubClient)(nil)
