// Package github provides a client for interacting with the GitHub API.
package github

import (
	"github.com/getstackit/stackit/internal/git"

	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Status check constants
const (
	// Commit-status (StatusContext) states, distinct from check-run conclusions.
	checkStateFailure = "FAILURE"
	checkStateError   = "ERROR"
	checkStatePending = "PENDING"
	checkTypeCheckRun = "CheckRun"

	// Stackit check names - these checks are excluded from CI status evaluation
	// because they are part of stackit's own workflow and expected to fail
	// during merge operations.
	stackitLockCheckName       = "Check Lock Status"
	stackitStackOrderCheckName = "Check Stack Order"
)

// ReviewDecision is a GitHub PR review decision.
type ReviewDecision string

const (
	ReviewDecisionApproved         ReviewDecision = "APPROVED"
	ReviewDecisionChangesRequested ReviewDecision = "CHANGES_REQUESTED"
	ReviewDecisionReviewRequired   ReviewDecision = "REVIEW_REQUIRED"
)

// graphQLAliasRegex matches characters not valid in GraphQL alias identifiers
var graphQLAliasRegex = regexp.MustCompile(`[^a-zA-Z0-9]`)

// ChecksByBranch maps branch names to their PR check status. Branches without
// a PR (or whose status could not be fetched) are absent.
type ChecksByBranch map[string]*CheckStatus

// Get returns the check status for a branch, or nil if unknown.
// Safe to call on a nil map.
func (c ChecksByBranch) Get(branch string) *CheckStatus {
	return c[branch]
}

// IsApproved returns true if the review decision indicates approval
func (s *CheckStatus) IsApproved() bool {
	return s.ReviewDecision == ReviewDecisionApproved
}

// HasFailingChecks returns true if any CI checks are failing
func (s *CheckStatus) HasFailingChecks() bool {
	return !s.Passing
}

// HasPendingChecks returns true if any CI checks are pending
func (s *CheckStatus) HasPendingChecks() bool {
	return s.Pending
}

// IsReady returns true if all checks pass and the PR is approved
func (s *CheckStatus) IsReady() bool {
	return s.Passing && !s.Pending && s.IsApproved()
}

// BatchGetPRChecksStatusGraphQL returns the check status for multiple branches using a single GraphQL query.
// This function fetches both CI check status and PR review decisions in a single request for efficiency.
func BatchGetPRChecksStatusGraphQL(ctx context.Context, runner GitCommandRunner, repo Repo, branchNames []string) (ChecksByBranch, error) {
	if len(branchNames) == 0 {
		return make(ChecksByBranch), nil
	}

	// Sanitize branch names for GraphQL aliases
	aliasMap := make(map[string]string)
	aliasToBranch := make(map[string]string)
	for _, name := range branchNames {
		alias := "b_" + graphQLAliasRegex.ReplaceAllString(name, "_")
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

	// Build GraphQL query - query pullRequests to get both CI status and review decision
	query := buildPRStatusQuery(aliasMap)

	variables := map[string]any{
		graphqlVarOwner: repo.Owner,
		graphqlVarRepo:  repo.Name,
	}

	body, err := executeGraphQLQuery(ctx, runner, query, variables)
	if err != nil {
		return nil, err
	}

	return parsePRStatusResponse(body, aliasToBranch)
}

// buildPRStatusQuery builds a GraphQL query to fetch PR status for multiple branches
func buildPRStatusQuery(aliasMap map[string]string) string {
	var queryBuilder strings.Builder
	queryBuilder.WriteString("query($owner: String!, $repo: String!) {\n")
	queryBuilder.WriteString("  repository(owner: $owner, name: $repo) {\n")
	for branch, alias := range aliasMap {
		fmt.Fprintf(&queryBuilder, "    %s: pullRequests(headRefName: \"%s\", first: 1, orderBy: {field: CREATED_AT, direction: DESC}) {\n", alias, branch)
		queryBuilder.WriteString("      nodes {\n")
		queryBuilder.WriteString("        state\n")
		queryBuilder.WriteString("        author { login }\n")
		queryBuilder.WriteString("        reviewDecision\n")
		queryBuilder.WriteString("        commits(last: 1) {\n")
		queryBuilder.WriteString("          nodes {\n")
		queryBuilder.WriteString("            commit {\n")
		queryBuilder.WriteString("              oid\n")
		queryBuilder.WriteString("              statusCheckRollup {\n")
		queryBuilder.WriteString("                state\n")
		queryBuilder.WriteString("                contexts(first: 100) {\n")
		queryBuilder.WriteString("                  nodes {\n")
		queryBuilder.WriteString("                    __typename\n")
		queryBuilder.WriteString("                    ... on CheckRun {\n")
		queryBuilder.WriteString("                      name\n")
		queryBuilder.WriteString("                      status\n")
		queryBuilder.WriteString("                      conclusion\n")
		queryBuilder.WriteString("                      startedAt\n")
		queryBuilder.WriteString("                      completedAt\n")
		queryBuilder.WriteString("                    }\n")
		queryBuilder.WriteString("                    ... on StatusContext {\n")
		queryBuilder.WriteString("                      context\n")
		queryBuilder.WriteString("                      state\n")
		queryBuilder.WriteString("                      createdAt\n")
		queryBuilder.WriteString("                    }\n")
		queryBuilder.WriteString("                  }\n")
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
	return queryBuilder.String()
}

// parsePRStatusResponse parses the GraphQL response for PR status queries
func parsePRStatusResponse(body []byte, aliasToBranch map[string]string) (ChecksByBranch, error) {
	var graphqlResponse struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(body, &graphqlResponse); err != nil {
		return nil, fmt.Errorf("failed to parse GraphQL response: %w", err)
	}

	repository, ok := graphqlResponse.Data["repository"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid GraphQL response format: missing repository")
	}

	results := make(ChecksByBranch)
	for alias, data := range repository {
		if data == nil {
			continue
		}
		branchName, ok := aliasToBranch[alias]
		if !ok {
			continue
		}

		status := parseBranchStatus(data)
		if status != nil {
			results[branchName] = status
		}
	}

	return results, nil
}

// parseBranchStatus parses the status data for a single branch from the GraphQL response
func parseBranchStatus(data any) *CheckStatus {
	prData, ok := data.(map[string]any)
	if !ok {
		return nil
	}
	prNodes, ok := prData["nodes"].([]any)
	if !ok || len(prNodes) == 0 {
		// No open PR for this branch
		return nil
	}

	prNode, ok := prNodes[0].(map[string]any)
	if !ok {
		return nil
	}

	// Extract PR state
	var prState git.PRState
	if s, ok := prNode["state"].(string); ok {
		prState = git.PRState(s)
	}

	// Extract author
	var author string
	if authorData, ok := prNode["author"].(map[string]any); ok {
		if login, ok := authorData["login"].(string); ok {
			author = login
		}
	}

	// Extract review decision
	var reviewDecision ReviewDecision
	if rd, ok := prNode["reviewDecision"].(string); ok {
		reviewDecision = ReviewDecision(rd)
	}

	// Navigate to commit's statusCheckRollup
	commits, ok := prNode["commits"].(map[string]any)
	if !ok {
		return &CheckStatus{Passing: true, Pending: false, ReviewDecision: reviewDecision, Author: author, State: prState}
	}

	commitNodes, ok := commits["nodes"].([]any)
	if !ok || len(commitNodes) == 0 {
		return &CheckStatus{Passing: true, Pending: false, ReviewDecision: reviewDecision, Author: author, State: prState}
	}

	commitNode, ok := commitNodes[0].(map[string]any)
	if !ok {
		return &CheckStatus{Passing: true, Pending: false, ReviewDecision: reviewDecision, Author: author, State: prState}
	}
	commit, ok := commitNode["commit"].(map[string]any)
	if !ok {
		return &CheckStatus{Passing: true, Pending: false, ReviewDecision: reviewDecision, Author: author, State: prState}
	}

	rollup, ok := commit["statusCheckRollup"].(map[string]any)
	if !ok || rollup == nil {
		// No status checks
		return &CheckStatus{Passing: true, Pending: false, ReviewDecision: reviewDecision, Author: author, State: prState}
	}

	result := parseCheckRollup(rollup, reviewDecision, author)
	result.State = prState
	return result
}

// parseCheckRollup parses the statusCheckRollup data from the GraphQL response
func parseCheckRollup(rollup map[string]any, reviewDecision ReviewDecision, author string) *CheckStatus {
	// First pass: collect all checks and deduplicate by name, keeping the most recent
	checksByName := make(map[string]*CheckDetail)

	contexts, ok := rollup["contexts"].(map[string]any)
	if ok && contexts != nil {
		nodes, ok := contexts["nodes"].([]any)
		if ok {
			for _, node := range nodes {
				n, ok := node.(map[string]any)
				if !ok {
					continue
				}
				detail := parseCheckNode(n)
				if detail == nil {
					continue
				}

				// Skip stackit's own checks as they are expected to fail during merge operations
				if detail.Name == stackitLockCheckName || detail.Name == stackitStackOrderCheckName {
					continue
				}

				// Deduplicate by name: keep the most recently finished check
				// This handles re-runs where an older run failed but a newer one succeeded
				existing, exists := checksByName[detail.Name]
				if !exists || detail.FinishedAt.After(existing.FinishedAt) {
					checksByName[detail.Name] = detail
				}
			}
		}
	}

	// Second pass: evaluate status based on deduplicated checks
	hasPending := false
	hasFailing := false
	checks := make([]CheckDetail, 0, len(checksByName))

	for _, detail := range checksByName {
		if detail.Status == CheckRunStatusQueued || detail.Status == CheckRunStatusInProgress {
			hasPending = true
		}
		if detail.Conclusion == CheckConclusionFailure || detail.Conclusion == CheckConclusionCanceled || detail.Conclusion == CheckConclusionTimedOut || detail.Conclusion == CheckConclusionActionRequired {
			hasFailing = true
		}
		checks = append(checks, *detail)
	}

	return &CheckStatus{
		Passing:        !hasFailing,
		Pending:        hasPending,
		Checks:         checks,
		ReviewDecision: reviewDecision,
		Author:         author,
	}
}

// parseCheckNode parses a single check node from the GraphQL response
func parseCheckNode(n map[string]any) *CheckDetail {
	detail := &CheckDetail{}
	typeName, ok := n["__typename"].(string)
	if !ok {
		return nil
	}

	switch typeName {
	case checkTypeCheckRun:
		if name, ok := n["name"].(string); ok {
			detail.Name = name
		}
		if status, ok := n["status"].(string); ok {
			detail.Status = CheckRunStatus(strings.ToUpper(status))
		}
		if conclusion, ok := n["conclusion"].(string); ok {
			detail.Conclusion = CheckConclusion(strings.ToUpper(conclusion))
		}
		if startedAt, ok := n["startedAt"].(string); ok {
			t, _ := time.Parse(time.RFC3339, startedAt)
			detail.StartedAt = t
		}
		if completedAt, ok := n["completedAt"].(string); ok {
			t, _ := time.Parse(time.RFC3339, completedAt)
			detail.FinishedAt = t
		}
	case "StatusContext":
		if context, ok := n["context"].(string); ok {
			detail.Name = context
		}
		detail.Status = CheckRunStatusCompleted
		if state, ok := n["state"].(string); ok {
			state = strings.ToUpper(state)
			switch state {
			case checkStatePending:
				detail.Status = CheckRunStatusInProgress
			case checkStateFailure, checkStateError:
				detail.Conclusion = CheckConclusionFailure
			case string(CheckConclusionSuccess):
				detail.Conclusion = CheckConclusionSuccess
			}
		}
		if createdAt, ok := n["createdAt"].(string); ok {
			t, _ := time.Parse(time.RFC3339, createdAt)
			detail.StartedAt = t
			detail.FinishedAt = t
		}
	}

	return detail
}
