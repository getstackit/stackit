// Package github provides a client for interacting with the GitHub API.
package github

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"stackit.dev/stackit/internal/git"
)

// Status check constants
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

	// Stackit check names - these checks are excluded from CI status evaluation
	// because they are part of stackit's own workflow and expected to fail
	// during merge operations.
	stackitLockCheckName       = "Check Lock Status"
	stackitStackOrderCheckName = "Check Stack Order"
)

// Review decision constants
const (
	ReviewDecisionApproved         = "APPROVED"
	ReviewDecisionChangesRequested = "CHANGES_REQUESTED"
	ReviewDecisionReviewRequired   = "REVIEW_REQUIRED"
)

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

	// Build GraphQL query - query pullRequests to get both CI status and review decision
	query := buildPRStatusQuery(aliasMap)

	variables := map[string]interface{}{
		"owner": owner,
		"repo":  repo,
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
		fmt.Fprintf(&queryBuilder, "    %s: pullRequests(headRefName: \"%s\", first: 1, states: [OPEN]) {\n", alias, branch)
		queryBuilder.WriteString("      nodes {\n")
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
func parsePRStatusResponse(body []byte, aliasToBranch map[string]string) (map[string]*CheckStatus, error) {
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

		status := parseBranchStatus(data)
		if status != nil {
			results[branchName] = status
		}
	}

	return results, nil
}

// parseBranchStatus parses the status data for a single branch from the GraphQL response
func parseBranchStatus(data interface{}) *CheckStatus {
	prData, ok := data.(map[string]interface{})
	if !ok {
		return nil
	}
	prNodes, ok := prData["nodes"].([]interface{})
	if !ok || len(prNodes) == 0 {
		// No open PR for this branch
		return nil
	}

	prNode, ok := prNodes[0].(map[string]interface{})
	if !ok {
		return nil
	}

	// Extract author
	var author string
	if authorData, ok := prNode["author"].(map[string]interface{}); ok {
		if login, ok := authorData["login"].(string); ok {
			author = login
		}
	}

	// Extract review decision
	var reviewDecision string
	if rd, ok := prNode["reviewDecision"].(string); ok {
		reviewDecision = rd
	}

	// Navigate to commit's statusCheckRollup
	commits, ok := prNode["commits"].(map[string]interface{})
	if !ok {
		return &CheckStatus{Passing: true, Pending: false, ReviewDecision: reviewDecision, Author: author}
	}

	commitNodes, ok := commits["nodes"].([]interface{})
	if !ok || len(commitNodes) == 0 {
		return &CheckStatus{Passing: true, Pending: false, ReviewDecision: reviewDecision, Author: author}
	}

	commitNode, ok := commitNodes[0].(map[string]interface{})
	if !ok {
		return &CheckStatus{Passing: true, Pending: false, ReviewDecision: reviewDecision, Author: author}
	}
	commit, ok := commitNode["commit"].(map[string]interface{})
	if !ok {
		return &CheckStatus{Passing: true, Pending: false, ReviewDecision: reviewDecision, Author: author}
	}

	rollup, ok := commit["statusCheckRollup"].(map[string]interface{})
	if !ok || rollup == nil {
		// No status checks
		return &CheckStatus{Passing: true, Pending: false, ReviewDecision: reviewDecision, Author: author}
	}

	return parseCheckRollup(rollup, reviewDecision, author)
}

// parseCheckRollup parses the statusCheckRollup data from the GraphQL response
func parseCheckRollup(rollup map[string]interface{}, reviewDecision, author string) *CheckStatus {
	hasPending := false
	hasFailing := false
	var checks []CheckDetail

	contexts, ok := rollup["contexts"].(map[string]interface{})
	if ok && contexts != nil {
		nodes, ok := contexts["nodes"].([]interface{})
		if ok {
			for _, node := range nodes {
				n, ok := node.(map[string]interface{})
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

				if detail.Status == "QUEUED" || detail.Status == checkStatusInProgress {
					hasPending = true
				}
				if detail.Conclusion == checkConclusionFailure || detail.Conclusion == checkConclusionCanceled || detail.Conclusion == checkConclusionTimedOut || detail.Conclusion == checkConclusionActionRequired {
					hasFailing = true
				}
				checks = append(checks, *detail)
			}
		}
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
func parseCheckNode(n map[string]interface{}) *CheckDetail {
	detail := &CheckDetail{}
	typeName, ok := n["__typename"].(string)
	if !ok {
		return nil
	}

	if typeName == "CheckRun" {
		if name, ok := n["name"].(string); ok {
			detail.Name = name
		}
		if status, ok := n["status"].(string); ok {
			detail.Status = strings.ToUpper(status)
		}
		if conclusion, ok := n["conclusion"].(string); ok {
			detail.Conclusion = strings.ToUpper(conclusion)
		}
		if startedAt, ok := n["startedAt"].(string); ok {
			t, _ := time.Parse(time.RFC3339, startedAt)
			detail.StartedAt = t
		}
		if completedAt, ok := n["completedAt"].(string); ok {
			t, _ := time.Parse(time.RFC3339, completedAt)
			detail.FinishedAt = t
		}
	} else if typeName == "StatusContext" {
		if context, ok := n["context"].(string); ok {
			detail.Name = context
		}
		detail.Status = "COMPLETED"
		if state, ok := n["state"].(string); ok {
			state = strings.ToUpper(state)
			switch state {
			case checkStatePending:
				detail.Status = checkStatusInProgress
			case checkStateFailure, checkStateError:
				detail.Conclusion = checkConclusionFailure
			case "SUCCESS":
				detail.Conclusion = "SUCCESS"
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
