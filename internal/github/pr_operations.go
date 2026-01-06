// Package github provides a client for interacting with the GitHub API.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/go-github/v62/github"
	"golang.org/x/oauth2"

	"stackit.dev/stackit/internal/git"
)

const (
	// GitHub check conclusion and status constants
	checkConclusionFailure        = "FAILURE"
	checkConclusionCanceled       = "CANCELED"
	checkConclusionTimedOut       = "TIMED_OUT"
	checkConclusionActionRequired = "ACTION_REQUIRED"
	checkStateFailure             = "FAILURE"
	checkStateError               = "ERROR"
	checkStatePending             = "PENDING"
	checkStatusInProgress         = "IN_PROGRESS"

	// Stackit lock check name - this check is excluded from CI status evaluation
	// because locking PRs is part of the consolidation workflow and expected to fail
	// during merge operations.
	stackitLockCheckName = "Check Lock Status"
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
}

// UpdatePROptions contains options for updating a pull request
type UpdatePROptions struct {
	Title           *string
	Body            *string
	Base            *string
	Draft           *bool
	Reviewers       []string
	TeamReviewers   []string
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

	return createdPR, nil
}

// UpdatePullRequest updates an existing pull request
func UpdatePullRequest(ctx context.Context, client *github.Client, runner git.Runner, owner, repo string, prNumber int, opts UpdatePROptions) error {
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
					return fmt.Errorf("PR %d does not have a Node ID", prNumber)
				}

				if err := updatePRDraftStatus(ctx, runner, *pr.NodeID, desiredDraft); err != nil {
					return fmt.Errorf("failed to update draft status for PR %d: %w", prNumber, err)
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
		return fmt.Errorf("failed to update pull request: %w", err)
	}

	// Update reviewers if specified
	if len(opts.Reviewers) > 0 || len(opts.TeamReviewers) > 0 {
		_, _, _ = client.PullRequests.RequestReviewers(ctx, owner, repo, prNumber, github.ReviewersRequest{
			Reviewers:     opts.Reviewers,
			TeamReviewers: opts.TeamReviewers,
		})
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

	return nil
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

// BatchGetPRChecksStatusGraphQL returns the check status for multiple branches using a single GraphQL query
func BatchGetPRChecksStatusGraphQL(ctx context.Context, runner git.Runner, owner, repo string, branchNames []string) (map[string]*CheckStatus, error) {
	if len(branchNames) == 0 {
		return make(map[string]*CheckStatus), nil
	}

	// Sanitize branch names for GraphQL aliases
	aliasMap := make(map[string]string)
	aliasToBranch := make(map[string]string)
	re := regexp.MustCompile(`[^a-zA-Z0-9]`)
	for _, name := range branchNames {
		alias := "b_" + re.ReplaceAllString(name, "_")
		// Ensure unique aliases if multiple branches map to same sanitized name
		baseAlias := alias
		counter := 1
		for {
			if _, exists := aliasToBranch[alias]; !exists {
				break
			}
			alias = fmt.Sprintf("%s_%d", baseAlias, counter)
			counter++
		}
		aliasMap[name] = alias
		aliasToBranch[alias] = name
	}

	// Build GraphQL query
	var queryBuilder strings.Builder
	queryBuilder.WriteString("query($owner: String!, $repo: String!) {\n")
	queryBuilder.WriteString("  repository(owner: $owner, name: $repo) {\n")
	for branch, alias := range aliasMap {
		fmt.Fprintf(&queryBuilder, "    %s: ref(qualifiedName: \"refs/heads/%s\") {\n", alias, branch)
		queryBuilder.WriteString("      name\n")
		queryBuilder.WriteString("      target {\n")
		queryBuilder.WriteString("        ... on Commit {\n")
		queryBuilder.WriteString("          oid\n")
		queryBuilder.WriteString("          statusCheckRollup {\n")
		queryBuilder.WriteString("            state\n")
		queryBuilder.WriteString("            contexts(first: 100) {\n")
		queryBuilder.WriteString("              nodes {\n")
		queryBuilder.WriteString("                __typename\n")
		queryBuilder.WriteString("                ... on CheckRun {\n")
		queryBuilder.WriteString("                  name\n")
		queryBuilder.WriteString("                  status\n")
		queryBuilder.WriteString("                  conclusion\n")
		queryBuilder.WriteString("                  startedAt\n")
		queryBuilder.WriteString("                  completedAt\n")
		queryBuilder.WriteString("                }\n")
		queryBuilder.WriteString("                ... on StatusContext {\n")
		queryBuilder.WriteString("                  context\n")
		queryBuilder.WriteString("                  state\n")
		queryBuilder.WriteString("                  createdAt\n")
		queryBuilder.WriteString("                }\n")
		queryBuilder.WriteString("              }\n")
		queryBuilder.WriteString("            }\n")
		queryBuilder.WriteString("          }\n")
		queryBuilder.WriteString("        }\n")
		queryBuilder.WriteString("      }\n")
		queryBuilder.WriteString("    }\n")
	}
	queryBuilder.WriteString("  }\n")
	queryBuilder.WriteString("}\n")

	variables := map[string]interface{}{
		"owner": owner,
		"repo":  repo,
	}

	body, err := executeGraphQLQuery(ctx, runner, queryBuilder.String(), variables)
	if err != nil {
		return nil, err
	}

	// Parse dynamic response
	var graphqlResponse struct {
		Data map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(body, &graphqlResponse); err != nil {
		return nil, fmt.Errorf("failed to parse GraphQL response: %w", err)
	}

	repository, ok := graphqlResponse.Data["repository"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid GraphQL response format: missing repository")
	}

	results := make(map[string]*CheckStatus)
	for alias, data := range repository {
		if data == nil {
			continue
		}
		branchName, ok := aliasToBranch[alias]
		if !ok {
			continue
		}

		refData := data.(map[string]interface{})
		target, ok := refData["target"].(map[string]interface{})
		if !ok || target == nil {
			continue
		}

		rollup, ok := target["statusCheckRollup"].(map[string]interface{})
		if !ok || rollup == nil {
			// No status checks
			results[branchName] = &CheckStatus{Passing: true, Pending: false}
			continue
		}

		hasPending := false
		hasFailing := false
		var checks []CheckDetail

		contexts, ok := rollup["contexts"].(map[string]interface{})
		if ok && contexts != nil {
			nodes, ok := contexts["nodes"].([]interface{})
			if ok {
				for _, node := range nodes {
					n := node.(map[string]interface{})
					detail := CheckDetail{}
					typeName := n["__typename"].(string)

					if typeName == "CheckRun" {
						detail.Name = n["name"].(string)
						detail.Status = strings.ToUpper(n["status"].(string))
						if n["conclusion"] != nil {
							detail.Conclusion = strings.ToUpper(n["conclusion"].(string))
						}
						if n["startedAt"] != nil {
							t, _ := time.Parse(time.RFC3339, n["startedAt"].(string))
							detail.StartedAt = t
						}
						if n["completedAt"] != nil {
							t, _ := time.Parse(time.RFC3339, n["completedAt"].(string))
							detail.FinishedAt = t
						}
					} else if typeName == "StatusContext" {
						detail.Name = n["context"].(string)
						detail.Status = "COMPLETED"
						state := strings.ToUpper(n["state"].(string))
						switch state {
						case checkStatePending:
							detail.Status = checkStatusInProgress
						case checkStateFailure, checkStateError:
							detail.Conclusion = checkConclusionFailure
						case "SUCCESS":
							detail.Conclusion = "SUCCESS"
						}
						if n["createdAt"] != nil {
							t, _ := time.Parse(time.RFC3339, n["createdAt"].(string))
							detail.StartedAt = t
							detail.FinishedAt = t
						}
					}

					// Skip stackit lock check as in REST implementation
					if detail.Name == stackitLockCheckName {
						continue
					}

					if detail.Status == "QUEUED" || detail.Status == checkStatusInProgress {
						hasPending = true
					}
					if detail.Conclusion == checkConclusionFailure || detail.Conclusion == checkConclusionCanceled || detail.Conclusion == checkConclusionTimedOut || detail.Conclusion == checkConclusionActionRequired {
						hasFailing = true
					}
					checks = append(checks, detail)
				}
			}
		}

		results[branchName] = &CheckStatus{
			Passing: !hasFailing,
			Pending: hasPending,
			Checks:  checks,
		}
	}

	return results, nil
}
