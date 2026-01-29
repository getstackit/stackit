// Package github provides a client for interacting with the GitHub API.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/go-github/v62/github"
	"golang.org/x/oauth2"

	"stackit.dev/stackit/internal/git"
)

// CreatePROptions contains options for creating a pull request
type CreatePROptions struct {
	Title         string
	Body          string
	Head          string
	Base          string
	Draft         bool
	Reviewers     []string
	TeamReviewers []string
	Labels        []string
	Assignees     []string
}

// UpdatePROptions contains options for updating a pull request
type UpdatePROptions struct {
	Title           *string
	Body            *string
	Base            *string
	Draft           *bool
	Reviewers       []string
	TeamReviewers   []string
	Labels          []string
	Assignees       []string
	MergeWhenReady  *bool
	RerequestReview bool
}

// CreatePullRequest creates a new pull request
func CreatePullRequest(ctx context.Context, client *github.Client, owner, repo string, opts CreatePROptions) (*github.PullRequest, error) {
	pr := &github.NewPullRequest{
		Title: github.String(opts.Title),
		Head:  github.String(opts.Head),
		Base:  github.String(opts.Base),
		Draft: github.Bool(opts.Draft),
	}

	if opts.Body != "" {
		pr.Body = github.String(opts.Body)
	}

	createdPR, _, err := client.PullRequests.Create(ctx, owner, repo, pr)
	if err != nil {
		return nil, fmt.Errorf("failed to create pull request: %w", err)
	}

	// Add reviewers if specified
	if len(opts.Reviewers) > 0 || len(opts.TeamReviewers) > 0 {
		_, _, _ = client.PullRequests.RequestReviewers(ctx, owner, repo, *createdPR.Number, github.ReviewersRequest{
			Reviewers:     opts.Reviewers,
			TeamReviewers: opts.TeamReviewers,
		})
	}

	// Add labels if specified
	if len(opts.Labels) > 0 {
		_, _, _ = client.Issues.AddLabelsToIssue(ctx, owner, repo, *createdPR.Number, opts.Labels)
	}

	// Add assignees if specified
	if len(opts.Assignees) > 0 {
		_, _, _ = client.Issues.AddAssignees(ctx, owner, repo, *createdPR.Number, opts.Assignees)
	}

	return createdPR, nil
}

// UpdatePullRequest updates an existing pull request
// Returns warnings (non-fatal issues like failed label/assignee additions) and error
func UpdatePullRequest(ctx context.Context, client *github.Client, runner git.Runner, owner, repo string, prNumber int, opts UpdatePROptions) ([]string, error) {
	var warnings []string

	// Handle draft status changes separately using GraphQL API, as the REST API
	// doesn't support updating draft status. We need to use GraphQL mutation
	// markPullRequestReadyForReview or convertPullRequestToDraft.
	if opts.Draft != nil {
		// Get current PR to check if draft status actually needs to change
		pr, _, err := client.PullRequests.Get(ctx, owner, repo, prNumber)
		if err == nil && pr.Draft != nil {
			currentDraft := *pr.Draft
			desiredDraft := *opts.Draft

			// Only change draft status if it's different
			if currentDraft != desiredDraft {
				// Get the PR's Node ID (required for GraphQL)
				if pr.NodeID == nil {
					return nil, fmt.Errorf("PR %d does not have a Node ID", prNumber)
				}

				if err := updatePRDraftStatus(ctx, runner, *pr.NodeID, desiredDraft); err != nil {
					return nil, fmt.Errorf("failed to update draft status for PR %d: %w", prNumber, err)
				}
			}
		}
	}

	// Update other fields via REST API
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
	// Note: We don't set update.Draft here because the REST API doesn't support it

	_, _, err := client.PullRequests.Edit(ctx, owner, repo, prNumber, update)

	if err != nil {
		return nil, fmt.Errorf("failed to update pull request: %w", err)
	}

	// Update reviewers if specified
	if len(opts.Reviewers) > 0 || len(opts.TeamReviewers) > 0 {
		_, _, err := client.PullRequests.RequestReviewers(ctx, owner, repo, prNumber, github.ReviewersRequest{
			Reviewers:     opts.Reviewers,
			TeamReviewers: opts.TeamReviewers,
		})
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("failed to add reviewers: %v", err))
		}
	}

	// Add labels if specified
	if len(opts.Labels) > 0 {
		_, _, err := client.Issues.AddLabelsToIssue(ctx, owner, repo, prNumber, opts.Labels)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("failed to add labels: %v", err))
		}
	}

	// Add assignees if specified
	if len(opts.Assignees) > 0 {
		_, _, err := client.Issues.AddAssignees(ctx, owner, repo, prNumber, opts.Assignees)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("failed to add assignees: %v", err))
		}
	}

	// Rerequest review if specified
	if opts.RerequestReview {
		// Get current reviewers first
		pr, _, err := client.PullRequests.Get(ctx, owner, repo, prNumber)
		if err == nil && pr.RequestedReviewers != nil {
			var reviewers []string
			var teamReviewers []string
			for _, reviewer := range pr.RequestedReviewers {
				reviewers = append(reviewers, *reviewer.Login)
			}
			for _, team := range pr.RequestedTeams {
				teamReviewers = append(teamReviewers, *team.Slug)
			}
			if len(reviewers) > 0 || len(teamReviewers) > 0 {
				// Remove and re-add reviewers
				_, _ = client.PullRequests.RemoveReviewers(ctx, owner, repo, prNumber, github.ReviewersRequest{
					Reviewers:     reviewers,
					TeamReviewers: teamReviewers,
				})
				_, _, _ = client.PullRequests.RequestReviewers(ctx, owner, repo, prNumber, github.ReviewersRequest{
					Reviewers:     reviewers,
					TeamReviewers: teamReviewers,
				})
			}
		}
	}

	// Merge when ready (this is typically handled via GitHub's auto-merge feature)
	// For now, we'll skip this as it requires additional API calls and permissions

	return warnings, nil
}

// GetPullRequestByBranch gets a pull request for a branch
func GetPullRequestByBranch(ctx context.Context, client *github.Client, owner, repo, branchName string) (*github.PullRequest, error) {
	// List PRs for this branch
	prs, _, err := client.PullRequests.List(ctx, owner, repo, &github.PullRequestListOptions{
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

	return prs[0], nil
}

// GetGitHubClient creates a GitHub client with authentication
func GetGitHubClient(ctx context.Context, runner git.Runner) (*github.Client, string, string, error) {
	token, err := getGitHubToken(runner)
	if err != nil {
		return nil, "", "", fmt.Errorf("failed to get GitHub token: %w", err)
	}

	repoInfo, err := getRepoInfoWithHostname(ctx, runner)
	if err != nil {
		return nil, "", "", fmt.Errorf("failed to get repository info: %w", err)
	}

	client, err := createGitHubClient(ctx, repoInfo.Hostname, token)
	if err != nil {
		return nil, "", "", fmt.Errorf("failed to create GitHub client: %w", err)
	}

	return client, repoInfo.Owner, repoInfo.Repo, nil
}

// ParseReviewers parses a comma-separated string of reviewers
// Returns individual reviewers and team reviewers
// Team reviewers can be specified as "org/team" or just "team"
func ParseReviewers(reviewersStr string) ([]string, []string) {
	if reviewersStr == "" {
		return nil, nil
	}

	var reviewers []string
	var teamReviewers []string

	parts := strings.Split(reviewersStr, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Check if it's a team (contains /)
		if strings.Contains(part, "/") {
			// Could be org/team or just a team slug
			teamReviewers = append(teamReviewers, part)
		} else {
			reviewers = append(reviewers, part)
		}
	}

	return reviewers, teamReviewers
}

// MergePullRequest merges a pull request using the GitHub API
func MergePullRequest(ctx context.Context, client *github.Client, owner, repo, branchName string, method MergeMethod) error {
	// First, get the PR for this branch
	pr, err := GetPullRequestByBranch(ctx, client, owner, repo, branchName)
	if err != nil {
		return fmt.Errorf("failed to get PR for branch %s: %w", branchName, err)
	}
	if pr == nil {
		return fmt.Errorf("no PR found for branch %s", branchName)
	}

	// Merge the PR using the specified method
	mergeRequest := &github.PullRequestOptions{
		MergeMethod: string(method),
	}
	_, _, err = client.PullRequests.Merge(ctx, owner, repo, *pr.Number, "", mergeRequest)
	if err != nil {
		return fmt.Errorf("failed to merge PR #%d for branch %s using %s: %w", *pr.Number, branchName, method, err)
	}
	return nil
}

// executeGraphQLQuery executes a GraphQL query and returns the response body
func executeGraphQLQuery(ctx context.Context, runner git.Runner, query string, variables map[string]interface{}) ([]byte, error) {
	// Get GitHub token
	token, err := getGitHubToken(runner)
	if err != nil {
		return nil, fmt.Errorf("failed to get GitHub token: %w", err)
	}

	// Get repository info to determine hostname
	repoInfo, err := getRepoInfoWithHostname(ctx, runner)
	if err != nil {
		return nil, fmt.Errorf("failed to get repository info: %w", err)
	}

	// Construct GraphQL endpoint URL
	var graphqlURL string
	if repoInfo.Hostname == "github.com" {
		graphqlURL = "https://api.github.com/graphql"
	} else {
		// GitHub Enterprise: https://hostname/api/graphql
		graphqlURL = fmt.Sprintf("https://%s/api/graphql", repoInfo.Hostname)
	}

	// Create authenticated HTTP client
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	httpClient := oauth2.NewClient(ctx, ts)

	// Prepare GraphQL request
	requestBody := map[string]interface{}{
		"query":     query,
		"variables": variables,
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal GraphQL request: %w", err)
	}

	// Make GraphQL request
	req, err := http.NewRequestWithContext(ctx, "POST", graphqlURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create GraphQL request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute GraphQL request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read GraphQL response: %w", err)
	}

	// Check for errors
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GraphQL request failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Check for GraphQL errors
	var graphqlErrors struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &graphqlErrors); err == nil && len(graphqlErrors.Errors) > 0 {
		errorMessages := make([]string, len(graphqlErrors.Errors))
		for i, ge := range graphqlErrors.Errors {
			errorMessages[i] = ge.Message
		}
		return nil, fmt.Errorf("GraphQL error: %s", strings.Join(errorMessages, "; "))
	}

	return body, nil
}

// AutoMergeStatus represents the state of GitHub's auto-merge feature on a PR
type AutoMergeStatus struct {
	Enabled     bool
	EnabledAt   string
	EnabledBy   string
	MergeMethod string
}

// EnableAutoMerge enables GitHub's auto-merge feature on a PR.
// This requires the repository to have auto-merge enabled in settings.
func EnableAutoMerge(ctx context.Context, runner git.Runner, prNodeID string, mergeMethod MergeMethod) error {
	mutation := `mutation EnableAutoMerge($pullRequestId: ID!, $mergeMethod: PullRequestMergeMethod!) {
		enablePullRequestAutoMerge(input: {
			pullRequestId: $pullRequestId
			mergeMethod: $mergeMethod
		}) {
			pullRequest {
				autoMergeRequest {
					enabledAt
				}
			}
		}
	}`

	// Convert our MergeMethod to GitHub's GraphQL enum format
	var graphqlMethod string
	switch mergeMethod {
	case MergeMethodMerge:
		graphqlMethod = "MERGE"
	case MergeMethodSquash:
		graphqlMethod = "SQUASH"
	case MergeMethodRebase:
		graphqlMethod = "REBASE"
	default:
		graphqlMethod = "SQUASH"
	}

	variables := map[string]interface{}{
		"pullRequestId": prNodeID,
		"mergeMethod":   graphqlMethod,
	}

	_, err := executeGraphQLQuery(ctx, runner, mutation, variables)
	if err != nil {
		// Check for common error cases
		if strings.Contains(err.Error(), "auto-merge is not allowed") || strings.Contains(err.Error(), "Pull request auto-merge is not enabled") {
			return fmt.Errorf("auto-merge is not enabled for this repository. Enable it in repository settings under 'Pull Requests' → 'Allow auto-merge'")
		}
		if strings.Contains(err.Error(), "Pull request is not in a mergeable state") {
			return fmt.Errorf("PR has merge conflicts. Please resolve conflicts before enabling auto-merge")
		}
		return fmt.Errorf("failed to enable auto-merge: %w", err)
	}

	return nil
}

// DisableAutoMerge disables GitHub's auto-merge feature on a PR.
func DisableAutoMerge(ctx context.Context, runner git.Runner, prNodeID string) error {
	mutation := `mutation DisableAutoMerge($pullRequestId: ID!) {
		disablePullRequestAutoMerge(input: {
			pullRequestId: $pullRequestId
		}) {
			pullRequest {
				id
			}
		}
	}`

	variables := map[string]interface{}{
		"pullRequestId": prNodeID,
	}

	_, err := executeGraphQLQuery(ctx, runner, mutation, variables)
	if err != nil {
		return fmt.Errorf("failed to disable auto-merge: %w", err)
	}

	return nil
}

// GetAutoMergeStatus checks if auto-merge is enabled on a PR and returns its status.
func GetAutoMergeStatus(ctx context.Context, runner git.Runner, prNodeID string) (*AutoMergeStatus, error) {
	query := `query GetAutoMergeStatus($nodeId: ID!) {
		node(id: $nodeId) {
			... on PullRequest {
				autoMergeRequest {
					enabledAt
					enabledBy {
						login
					}
					mergeMethod
				}
			}
		}
	}`

	variables := map[string]interface{}{
		"nodeId": prNodeID,
	}

	body, err := executeGraphQLQuery(ctx, runner, query, variables)
	if err != nil {
		return nil, fmt.Errorf("failed to get auto-merge status: %w", err)
	}

	var response struct {
		Data struct {
			Node struct {
				AutoMergeRequest *struct {
					EnabledAt string `json:"enabledAt"`
					EnabledBy *struct {
						Login string `json:"login"`
					} `json:"enabledBy"`
					MergeMethod string `json:"mergeMethod"`
				} `json:"autoMergeRequest"`
			} `json:"node"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse auto-merge status response: %w", err)
	}

	status := &AutoMergeStatus{
		Enabled: response.Data.Node.AutoMergeRequest != nil,
	}

	if response.Data.Node.AutoMergeRequest != nil {
		status.EnabledAt = response.Data.Node.AutoMergeRequest.EnabledAt
		status.MergeMethod = response.Data.Node.AutoMergeRequest.MergeMethod
		if response.Data.Node.AutoMergeRequest.EnabledBy != nil {
			status.EnabledBy = response.Data.Node.AutoMergeRequest.EnabledBy.Login
		}
	}

	return status, nil
}

// PRMergeableState represents the mergeable state of a PR
type PRMergeableState struct {
	Mergeable      bool   // True if PR can be merged without conflicts
	MergeStateText string // MERGEABLE, CONFLICTING, UNKNOWN
	State          string // OPEN, CLOSED, MERGED
}

// GetPRMergeableState checks if a PR has merge conflicts.
func GetPRMergeableState(ctx context.Context, runner git.Runner, prNodeID string) (*PRMergeableState, error) {
	query := `query GetPRMergeableState($nodeId: ID!) {
		node(id: $nodeId) {
			... on PullRequest {
				mergeable
				mergeStateStatus
				state
			}
		}
	}`

	variables := map[string]interface{}{
		"nodeId": prNodeID,
	}

	body, err := executeGraphQLQuery(ctx, runner, query, variables)
	if err != nil {
		return nil, fmt.Errorf("failed to get PR mergeable state: %w", err)
	}

	var response struct {
		Data struct {
			Node struct {
				Mergeable        string `json:"mergeable"`
				MergeStateStatus string `json:"mergeStateStatus"`
				State            string `json:"state"`
			} `json:"node"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse PR mergeable state response: %w", err)
	}

	return &PRMergeableState{
		Mergeable:      response.Data.Node.Mergeable == "MERGEABLE",
		MergeStateText: response.Data.Node.MergeStateStatus,
		State:          response.Data.Node.State,
	}, nil
}

// BatchGetPRMergeableStates checks mergeable state for multiple PRs in a single GraphQL query.
// Returns a map from node ID to PRMergeableState. If a PR fails to fetch, it won't be in the map.
func BatchGetPRMergeableStates(ctx context.Context, runner git.Runner, prNodeIDs []string) (map[string]*PRMergeableState, error) {
	if len(prNodeIDs) == 0 {
		return make(map[string]*PRMergeableState), nil
	}

	// Build query with aliases for each PR
	queryParts := make([]string, 0, len(prNodeIDs))
	variables := make(map[string]interface{}, len(prNodeIDs))
	for i, nodeID := range prNodeIDs {
		alias := fmt.Sprintf("pr%d", i)
		varName := fmt.Sprintf("nodeId%d", i)
		queryParts = append(queryParts, fmt.Sprintf(`%s: node(id: $%s) {
			... on PullRequest {
				id
				mergeable
				mergeStateStatus
				state
			}
		}`, alias, varName))
		variables[varName] = nodeID
	}

	// Build variable declarations
	varDecls := make([]string, 0, len(prNodeIDs))
	for i := range prNodeIDs {
		varDecls = append(varDecls, fmt.Sprintf("$nodeId%d: ID!", i))
	}

	query := fmt.Sprintf("query BatchGetPRMergeableStates(%s) { %s }",
		strings.Join(varDecls, ", "),
		strings.Join(queryParts, " "))

	body, err := executeGraphQLQuery(ctx, runner, query, variables)
	if err != nil {
		return nil, fmt.Errorf("failed to batch get PR mergeable states: %w", err)
	}

	// Parse response - the data object has dynamic keys (pr0, pr1, etc.)
	var response struct {
		Data map[string]struct {
			ID               string `json:"id"`
			Mergeable        string `json:"mergeable"`
			MergeStateStatus string `json:"mergeStateStatus"`
			State            string `json:"state"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse batch PR mergeable state response: %w", err)
	}

	// Build result map keyed by node ID
	result := make(map[string]*PRMergeableState, len(prNodeIDs))
	for _, prData := range response.Data {
		if prData.ID == "" {
			continue // Skip null nodes
		}
		result[prData.ID] = &PRMergeableState{
			Mergeable:      prData.Mergeable == "MERGEABLE",
			MergeStateText: prData.MergeStateStatus,
			State:          prData.State,
		}
	}

	return result, nil
}

// WaitForPRMerge polls until a PR is merged or times out.
// Returns nil if the PR is merged, error otherwise.
func WaitForPRMerge(ctx context.Context, runner git.Runner, prNodeID string, timeout time.Duration, pollInterval time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		state, err := GetPRMergeableState(ctx, runner, prNodeID)
		if err != nil {
			return fmt.Errorf("failed to check PR state: %w", err)
		}

		if state.State == "MERGED" {
			return nil
		}

		if state.State == "CLOSED" {
			return fmt.Errorf("PR was closed without merging")
		}

		// Check if auto-merge was disabled (might indicate conflicts or other issues)
		autoMerge, err := GetAutoMergeStatus(ctx, runner, prNodeID)
		if err == nil && !autoMerge.Enabled {
			// Re-check PR state to avoid race condition where PR merged between checks
			freshState, freshErr := GetPRMergeableState(ctx, runner, prNodeID)
			if freshErr == nil && freshState.State == "MERGED" {
				return nil // PR merged successfully
			}

			// Auto-merge was disabled and PR is not merged
			if freshErr == nil && !freshState.Mergeable {
				return fmt.Errorf("auto-merge was disabled due to merge conflicts. Please resolve conflicts and try again")
			}
			return fmt.Errorf("auto-merge was disabled. This may indicate a problem with the PR")
		}

		time.Sleep(pollInterval)
	}

	return fmt.Errorf("timed out waiting for PR to be merged after %v", timeout)
}

// updatePRDraftStatus updates the draft status of a PR using GitHub's GraphQL API
func updatePRDraftStatus(ctx context.Context, runner git.Runner, pullRequestID string, isDraft bool) error {
	// Determine which mutation to use
	var mutation string
	var mutationName string
	if isDraft {
		mutationName = "convertPullRequestToDraft"
		mutation = `mutation ConvertPullRequestToDraft($pullRequestId: ID!) {
			convertPullRequestToDraft(input: {pullRequestId: $pullRequestId}) {
				pullRequest {
					id
					isDraft
				}
			}
		}`
	} else {
		mutationName = "markPullRequestReadyForReview"
		mutation = `mutation MarkPullRequestReadyForReview($pullRequestId: ID!) {
			markPullRequestReadyForReview(input: {pullRequestId: $pullRequestId}) {
				pullRequest {
					id
					isDraft
				}
			}
		}`
	}

	variables := map[string]interface{}{
		"pullRequestId": pullRequestID,
	}

	_, err := executeGraphQLQuery(ctx, runner, mutation, variables)
	if err != nil {
		return fmt.Errorf("GraphQL %s mutation failed: %w", mutationName, err)
	}

	return nil
}
