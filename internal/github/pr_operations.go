// Package github provides a client for interacting with the GitHub API.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/go-github/v88/github"
	"golang.org/x/oauth2"
)

var (
	// ErrAutoMergeNotEnabled is returned when auto-merge is not enabled for the repository.
	ErrAutoMergeNotEnabled = errors.New("auto-merge is not enabled for this repository")
	// ErrPRCleanStatus is returned when a PR is already in a clean/mergeable status.
	ErrPRCleanStatus = errors.New("PR is already in clean status")
	// ErrPRAlreadyMerged is returned when a PR was merged while waiting.
	ErrPRAlreadyMerged = errors.New("PR was merged while waiting")
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

// applyPRMetadata requests reviewers, adds labels, and adds assignees on an existing PR.
// Failures are collected as warnings rather than hard errors, matching the non-fatal
// contract used by both CreatePullRequest and UpdatePullRequest.
func applyPRMetadata(ctx context.Context, client *github.Client, owner, repo string, prNumber int, reviewers, teamReviewers, labels, assignees []string) []string {
	var warnings []string

	if len(reviewers) > 0 || len(teamReviewers) > 0 {
		_, _, err := client.PullRequests.RequestReviewers(ctx, owner, repo, prNumber, github.ReviewersRequest{
			Reviewers:     reviewers,
			TeamReviewers: teamReviewers,
		})
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("failed to add reviewers: %v", err))
		}
	}

	if len(labels) > 0 {
		_, _, err := client.Issues.AddLabelsToIssue(ctx, owner, repo, prNumber, labels)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("failed to add labels: %v", err))
		}
	}

	if len(assignees) > 0 {
		_, _, err := client.Issues.AddAssignees(ctx, owner, repo, prNumber, assignees)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("failed to add assignees: %v", err))
		}
	}

	return warnings
}

// CreatePullRequest creates a new pull request.
// Returns warnings (non-fatal issues like failed label/assignee additions) and error,
// matching the same contract as UpdatePullRequest.
func CreatePullRequest(ctx context.Context, client *github.Client, owner, repo string, opts CreatePROptions) ([]string, *github.PullRequest, error) {
	pr := &github.NewPullRequest{
		Title: new(opts.Title),
		Head:  new(opts.Head),
		Base:  new(opts.Base),
		Draft: new(opts.Draft),
	}

	if opts.Body != "" {
		pr.Body = new(opts.Body)
	}

	createdPR, _, err := client.PullRequests.Create(ctx, owner, repo, pr)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create pull request: %w", err)
	}

	warnings := applyPRMetadata(ctx, client, owner, repo, *createdPR.Number, opts.Reviewers, opts.TeamReviewers, opts.Labels, opts.Assignees)
	return warnings, createdPR, nil
}

// UpdatePullRequest updates an existing pull request
// Returns warnings (non-fatal issues like failed label/assignee additions) and error
func UpdatePullRequest(ctx context.Context, client *github.Client, runner GitCommandRunner, owner, repo string, prNumber int, opts UpdatePROptions) ([]string, error) {
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

	warnings := applyPRMetadata(ctx, client, owner, repo, prNumber, opts.Reviewers, opts.TeamReviewers, opts.Labels, opts.Assignees)

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

	return prs[0], nil
}

// GetGitHubClient creates a GitHub client with authentication
func GetGitHubClient(ctx context.Context, runner GitCommandRunner) (*github.Client, string, string, error) {
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

	parts := strings.SplitSeq(reviewersStr, ",")
	for part := range parts {
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

// MergePullRequest merges a pull request using the GitHub API.
// If opts.CommitBody is set, it is used as an additional commit message body
// for merge/squash strategies.
func MergePullRequest(ctx context.Context, client *github.Client, owner, repo, branchName string, opts MergePROptions) error {
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
		MergeMethod: string(opts.Method),
	}
	_, _, err = client.PullRequests.Merge(ctx, owner, repo, *pr.Number, opts.CommitBody, mergeRequest)
	if err != nil {
		return fmt.Errorf("failed to merge PR #%d for branch %s using %s: %w", *pr.Number, branchName, opts.Method, err)
	}
	return nil
}

// executeGraphQLQuery executes a GraphQL query and returns the response body
func executeGraphQLQuery(ctx context.Context, runner GitCommandRunner, query string, variables map[string]any) ([]byte, error) {
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
	requestBody := map[string]any{
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

// EnableAutoMergeOptions contains options for enabling auto-merge on a PR.
type EnableAutoMergeOptions struct {
	MergeMethod MergeMethod
	CommitBody  string // optional — omit for default GitHub behavior
}

// EnableAutoMerge enables GitHub's auto-merge feature on a PR.
// This requires the repository to have auto-merge enabled in settings.
func EnableAutoMerge(ctx context.Context, runner GitCommandRunner, prNodeID string, opts EnableAutoMergeOptions) error {
	// We use two separate mutations because GitHub's GraphQL API treats
	// `commitBody: null` differently from omitting commitBody entirely.
	// When omitted, GitHub uses its default commit message (e.g., PR body).
	// When explicitly set to null, it may override that default to empty.
	var mutation string
	if opts.CommitBody != "" {
		mutation = `mutation EnableAutoMerge($pullRequestId: ID!, $mergeMethod: PullRequestMergeMethod!, $commitBody: String) {
			enablePullRequestAutoMerge(input: {
				pullRequestId: $pullRequestId
				mergeMethod: $mergeMethod
				commitBody: $commitBody
			}) {
				pullRequest {
					autoMergeRequest {
						enabledAt
					}
				}
			}
		}`
	} else {
		mutation = `mutation EnableAutoMerge($pullRequestId: ID!, $mergeMethod: PullRequestMergeMethod!) {
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
	}

	// Convert our MergeMethod to GitHub's GraphQL enum format
	var graphqlMethod string
	switch opts.MergeMethod {
	case MergeMethodMerge:
		graphqlMethod = "MERGE"
	case MergeMethodSquash:
		graphqlMethod = "SQUASH"
	case MergeMethodRebase:
		graphqlMethod = "REBASE"
	default:
		graphqlMethod = "SQUASH"
	}

	variables := map[string]any{
		graphqlVarPullRequestID: prNodeID,
		"mergeMethod":           graphqlMethod,
	}
	if opts.CommitBody != "" {
		variables["commitBody"] = opts.CommitBody
	}

	_, err := executeGraphQLQuery(ctx, runner, mutation, variables)
	if err != nil {
		errMsg := err.Error()
		// Check for common error cases and wrap with sentinel errors
		if strings.Contains(errMsg, "auto-merge is not allowed") || strings.Contains(errMsg, "Pull request auto-merge is not enabled") {
			return fmt.Errorf("enable it in repository settings under 'Pull Requests' → 'Allow auto-merge': %w", ErrAutoMergeNotEnabled)
		}
		if strings.Contains(errMsg, "clean status") {
			return fmt.Errorf("PR is ready for direct merge: %w", ErrPRCleanStatus)
		}
		if strings.Contains(errMsg, "Pull request is not in a mergeable state") {
			return fmt.Errorf("PR has merge conflicts. Please resolve conflicts before enabling auto-merge")
		}
		return fmt.Errorf("failed to enable auto-merge: %w", err)
	}

	return nil
}

// DisableAutoMerge disables GitHub's auto-merge feature on a PR.
func DisableAutoMerge(ctx context.Context, runner GitCommandRunner, prNodeID string) error {
	mutation := `mutation DisableAutoMerge($pullRequestId: ID!) {
		disablePullRequestAutoMerge(input: {
			pullRequestId: $pullRequestId
		}) {
			pullRequest {
				id
			}
		}
	}`

	variables := map[string]any{
		graphqlVarPullRequestID: prNodeID,
	}

	_, err := executeGraphQLQuery(ctx, runner, mutation, variables)
	if err != nil {
		return fmt.Errorf("failed to disable auto-merge: %w", err)
	}

	return nil
}

// GetAutoMergeStatus checks if auto-merge is enabled on a PR and returns its status.
func GetAutoMergeStatus(ctx context.Context, runner GitCommandRunner, prNodeID string) (*AutoMergeStatus, error) {
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

	variables := map[string]any{
		graphqlVarNodeID: prNodeID,
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

// PR state constants as returned by GitHub's GraphQL API.
const (
	PRStateMerged = "MERGED"
	PRStateClosed = "CLOSED"
)

// GraphQL variable name constants shared across GitHub API calls.
const (
	graphqlVarOwner         = "owner"
	graphqlVarRepo          = "repo"
	graphqlVarPullRequestID = "pullRequestId"
	graphqlVarNodeID        = "nodeId"
)

// PR list state constant for fetching PRs regardless of their open/closed status.
const prStateAll = "all"

// PR merge state text constants as returned by GitHub's mergeStateStatus field.
const (
	prMergeStateClean   = "CLEAN"
	prMergeStateDirty   = "DIRTY"
	prMergeStateUnknown = "UNKNOWN"
)

// mergeableMergeable is the GraphQL `mergeable` field value indicating a PR can
// be merged without conflicts. The field's possible values are MERGEABLE,
// CONFLICTING, and UNKNOWN.
const mergeableMergeable = "MERGEABLE"

// PRMergeableState represents the mergeable state of a PR
type PRMergeableState struct {
	Mergeable      bool   // True if PR can be merged without conflicts
	MergeStateText string // mergeStateStatus value: CLEAN, DIRTY, BLOCKED, UNKNOWN, etc.
	State          string // OPEN, CLOSED, MERGED
}

// isMergeStateStatusUnsupported returns true when the error indicates the mergeStateStatus
// field is not available on the GitHub instance (e.g. older GitHub Enterprise versions).
func isMergeStateStatusUnsupported(err error) bool {
	return strings.Contains(err.Error(), "mergeStateStatus")
}

// mergeableToMergeStateText maps GitHub's mergeable field to an equivalent MergeStateStatus
// value. Used on GitHub Enterprise instances that don't support mergeStateStatus.
func mergeableToMergeStateText(mergeable string) string {
	switch mergeable {
	case mergeableMergeable:
		return prMergeStateClean
	case "CONFLICTING":
		return prMergeStateDirty
	default:
		return prMergeStateUnknown
	}
}

// GetPRMergeableState checks if a PR has merge conflicts.
func GetPRMergeableState(ctx context.Context, runner GitCommandRunner, prNodeID string) (*PRMergeableState, error) {
	query := `query GetPRMergeableState($nodeId: ID!) {
		node(id: $nodeId) {
			... on PullRequest {
				mergeable
				mergeStateStatus
				state
			}
		}
	}`

	variables := map[string]any{
		graphqlVarNodeID: prNodeID,
	}

	body, err := executeGraphQLQuery(ctx, runner, query, variables)
	if err != nil {
		if isMergeStateStatusUnsupported(err) {
			return getPRMergeableStateBasic(ctx, runner, prNodeID)
		}
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
		Mergeable:      response.Data.Node.Mergeable == mergeableMergeable,
		MergeStateText: response.Data.Node.MergeStateStatus,
		State:          response.Data.Node.State,
	}, nil
}

// getPRMergeableStateBasic fetches PR mergeable state without the mergeStateStatus field,
// used as a fallback for GitHub Enterprise instances that don't support that field.
func getPRMergeableStateBasic(ctx context.Context, runner GitCommandRunner, prNodeID string) (*PRMergeableState, error) {
	query := `query GetPRMergeableState($nodeId: ID!) {
		node(id: $nodeId) {
			... on PullRequest {
				mergeable
				state
			}
		}
	}`

	variables := map[string]any{
		graphqlVarNodeID: prNodeID,
	}

	body, err := executeGraphQLQuery(ctx, runner, query, variables)
	if err != nil {
		return nil, fmt.Errorf("failed to get PR mergeable state: %w", err)
	}

	var response struct {
		Data struct {
			Node struct {
				Mergeable string `json:"mergeable"`
				State     string `json:"state"`
			} `json:"node"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse PR mergeable state response: %w", err)
	}

	return &PRMergeableState{
		Mergeable:      response.Data.Node.Mergeable == mergeableMergeable,
		MergeStateText: mergeableToMergeStateText(response.Data.Node.Mergeable),
		State:          response.Data.Node.State,
	}, nil
}

// buildBatchPRNodeQuery builds a batch GraphQL query for fetching multiple PR nodes by ID.
// fields is the space-separated list of GraphQL field names to select within the PullRequest fragment.
func buildBatchPRNodeQuery(queryName string, prNodeIDs []string, fields string) (string, map[string]any) {
	queryParts := make([]string, 0, len(prNodeIDs))
	variables := make(map[string]any, len(prNodeIDs))
	for i, nodeID := range prNodeIDs {
		alias := fmt.Sprintf("pr%d", i)
		varName := fmt.Sprintf("nodeId%d", i)
		queryParts = append(queryParts, fmt.Sprintf(`%s: node(id: $%s) { ... on PullRequest { %s } }`, alias, varName, fields))
		variables[varName] = nodeID
	}

	varDecls := make([]string, 0, len(prNodeIDs))
	for i := range prNodeIDs {
		varDecls = append(varDecls, fmt.Sprintf("$nodeId%d: ID!", i))
	}

	return fmt.Sprintf("query %s(%s) { %s }",
		queryName,
		strings.Join(varDecls, ", "),
		strings.Join(queryParts, " ")), variables
}

// BatchGetPRMergeableStates checks mergeable state for multiple PRs in a single GraphQL query.
// Returns a map from node ID to PRMergeableState. If a PR fails to fetch, it won't be in the map.
func BatchGetPRMergeableStates(ctx context.Context, runner GitCommandRunner, prNodeIDs []string) (map[string]*PRMergeableState, error) {
	if len(prNodeIDs) == 0 {
		return make(map[string]*PRMergeableState), nil
	}

	query, variables := buildBatchPRNodeQuery("BatchGetPRMergeableStates", prNodeIDs, "id mergeable mergeStateStatus state")

	body, err := executeGraphQLQuery(ctx, runner, query, variables)
	if err != nil {
		if isMergeStateStatusUnsupported(err) {
			return batchGetPRMergeableStatesBasic(ctx, runner, prNodeIDs)
		}
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
			Mergeable:      prData.Mergeable == mergeableMergeable,
			MergeStateText: prData.MergeStateStatus,
			State:          prData.State,
		}
	}

	return result, nil
}

// batchGetPRMergeableStatesBasic fetches PR mergeable states without the mergeStateStatus field,
// used as a fallback for GitHub Enterprise instances that don't support that field.
func batchGetPRMergeableStatesBasic(ctx context.Context, runner GitCommandRunner, prNodeIDs []string) (map[string]*PRMergeableState, error) {
	query, variables := buildBatchPRNodeQuery("BatchGetPRMergeableStates", prNodeIDs, "id mergeable state")

	body, err := executeGraphQLQuery(ctx, runner, query, variables)
	if err != nil {
		return nil, fmt.Errorf("failed to batch get PR mergeable states: %w", err)
	}

	var response struct {
		Data map[string]struct {
			ID        string `json:"id"`
			Mergeable string `json:"mergeable"`
			State     string `json:"state"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse batch PR mergeable state response: %w", err)
	}

	result := make(map[string]*PRMergeableState, len(prNodeIDs))
	for _, prData := range response.Data {
		if prData.ID == "" {
			continue
		}
		result[prData.ID] = &PRMergeableState{
			Mergeable:      prData.Mergeable == mergeableMergeable,
			MergeStateText: mergeableToMergeStateText(prData.Mergeable),
			State:          prData.State,
		}
	}

	return result, nil
}

// WaitForPRMerge polls until a PR is merged or times out.
// Returns nil if the PR is merged, error otherwise.
func WaitForPRMerge(ctx context.Context, runner GitCommandRunner, prNodeID string, timeout time.Duration, pollInterval time.Duration) error {
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

		if state.State == PRStateMerged {
			return nil
		}

		if state.State == PRStateClosed {
			return fmt.Errorf("PR was closed without merging")
		}

		// Check if auto-merge was disabled (might indicate conflicts or other issues)
		autoMerge, err := GetAutoMergeStatus(ctx, runner, prNodeID)
		if err == nil && !autoMerge.Enabled {
			// Re-check PR state to avoid race condition where PR merged between checks
			freshState, freshErr := GetPRMergeableState(ctx, runner, prNodeID)
			if freshErr == nil && freshState.State == PRStateMerged {
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

// WaitForMergeable polls until a PR's mergeStateStatus becomes CLEAN or HAS_HOOKS (ready to merge).
// Returns the final PRMergeableState when ready.
// Returns ErrPRAlreadyMerged if the PR is merged during polling.
// Returns an error if the PR is CLOSED, DIRTY (conflicts), times out, or the context is canceled.
func WaitForMergeable(ctx context.Context, runner GitCommandRunner, prNodeID string, timeout time.Duration, pollInterval time.Duration) (*PRMergeableState, error) {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		state, err := GetPRMergeableState(ctx, runner, prNodeID)
		if err != nil {
			return nil, fmt.Errorf("failed to check PR state: %w", err)
		}

		switch state.State {
		case PRStateMerged:
			return state, fmt.Errorf("PR was merged while waiting: %w", ErrPRAlreadyMerged)
		case PRStateClosed:
			return state, fmt.Errorf("PR was closed without merging")
		}

		switch state.MergeStateText {
		case prMergeStateClean, "HAS_HOOKS":
			return state, nil
		case prMergeStateDirty:
			return state, fmt.Errorf("PR has merge conflicts (DIRTY). Please resolve conflicts and try again")
		}

		// BLOCKED, BEHIND, UNKNOWN, or empty — keep polling
		time.Sleep(pollInterval)
	}

	return nil, fmt.Errorf("timed out waiting for PR to become mergeable after %v", timeout)
}

// updatePRDraftStatus updates the draft status of a PR using GitHub's GraphQL API
func updatePRDraftStatus(ctx context.Context, runner GitCommandRunner, pullRequestID string, isDraft bool) error {
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

	variables := map[string]any{
		graphqlVarPullRequestID: pullRequestID,
	}

	_, err := executeGraphQLQuery(ctx, runner, mutation, variables)
	if err != nil {
		return fmt.Errorf("GraphQL %s mutation failed: %w", mutationName, err)
	}

	return nil
}
