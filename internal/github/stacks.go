package github

import (
	"context"
	"fmt"
	"net/http"
)

// StackClient is the narrow GitHub Stacks API surface used by the experimental
// Stackit command. It is deliberately separate from Client while the feature
// is being evaluated, so normal PR operations do not depend on the API.
type StackClient interface {
	CreateStack(ctx context.Context, pullRequests []int) (*StackInfo, error)
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

// CreateStack creates a native GitHub Stack from the supplied pull request
// numbers, ordered from bottom to top. GitHub validates that their base refs
// form a contiguous linear chain.
func (c *StackitGitHubClient) CreateStack(ctx context.Context, pullRequests []int) (*StackInfo, error) {
	if len(pullRequests) < 2 {
		return nil, fmt.Errorf("a GitHub Stack requires at least two pull requests")
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

var _ StackClient = (*StackitGitHubClient)(nil)
