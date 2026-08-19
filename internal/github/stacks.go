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
	UnstackStack(ctx context.Context, stackNumber int) (bool, error)
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
	StackSyncRebuilt   StackSyncAction = "rebuilt"
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

	return rebuildStack(ctx, client, existing, existingPRs, pullRequests)
}

// rebuildStack dissolves a stack whose contents no longer match the submitted
// chain and recreates it from that chain.
//
// GitHub's Stacks API can only append to the top of a stack — there is no
// endpoint that removes an individual pull request. So a stack that diverges
// (typically because a PR in it was closed and resubmitted as a new one, or a
// branch was folded away) has exactly one repair: unstack it and create it
// again. Without this, every later submit reported a mismatch and skipped
// native Stack sync forever, with nothing the user could do from stackit.
//
// Unstack removes only the pull requests GitHub allows to leave a stack;
// merged, merging, and queued ones stay. Rather than guess which those are —
// the API reports a merged PR's state as "closed", indistinguishable from a
// closed one — attempt the unstack and let GitHub answer: the stack dissolves
// only when nothing was pinned in place.
func rebuildStack(ctx context.Context, client StackClient, existing *StackInfo, existingPRs, pullRequests []int) (*StackInfo, StackSyncAction, error) {
	dissolved, err := client.UnstackStack(ctx, existing.Number)
	if err != nil {
		return nil, "", fmt.Errorf("GitHub Stack #%d has PRs %v, which do not match the submitted chain %v, and it could not be dissolved to rebuild: %w", existing.Number, existingPRs, pullRequests, err)
	}
	if !dissolved {
		return nil, "", fmt.Errorf("GitHub Stack #%d has PRs %v, which do not match the submitted chain %v; the pull requests still in it are merged or queued to merge and cannot be unstacked", existing.Number, existingPRs, pullRequests)
	}

	stack, err := client.CreateStack(ctx, pullRequests)
	if err != nil {
		return nil, "", fmt.Errorf("dissolved GitHub Stack #%d but could not recreate it from %v: %w", existing.Number, pullRequests, err)
	}
	return stack, StackSyncRebuilt, nil
}

// UnstackStack removes every pull request GitHub permits from a native Stack.
// Reports whether the stack was dissolved outright; false means merged or
// queued pull requests remain in it.
func (c *StackitGitHubClient) UnstackStack(ctx context.Context, stackNumber int) (bool, error) {
	path := fmt.Sprintf("repos/%s/%s/stacks/%d/unstack", c.repo.Owner, c.repo.Name, stackNumber)
	req, err := c.client.NewRequest(ctx, http.MethodPost, path, nil)
	if err != nil {
		return false, fmt.Errorf("build GitHub Stack unstack request: %w", err)
	}

	var remaining StackInfo
	resp, err := c.client.Do(req, &remaining)
	if err != nil {
		return false, fmt.Errorf("unstack GitHub Stack #%d: %w", stackNumber, err)
	}
	// 204 means the stack is gone; 200 returns whatever is still stacked.
	if resp != nil && resp.StatusCode == http.StatusNoContent {
		return true, nil
	}
	return len(remaining.PullRequests) == 0, nil
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
