package github

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// PRStateBody holds the subset of pull-request fields needed to append a
// consolidation footer and decide whether a PR still needs closing.
type PRStateBody struct {
	State string
	Body  string
}

// BatchGetPRStateBodyGraphQL fetches the state and body for multiple PR numbers
// in a single GraphQL query, replacing one REST GetPullRequest call per number.
// Numbers with no matching PR are absent from the returned map.
func BatchGetPRStateBodyGraphQL(ctx context.Context, runner GitCommandRunner, owner, repo string, prNumbers []int) (map[int]PRStateBody, error) {
	if len(prNumbers) == 0 {
		return make(map[int]PRStateBody), nil
	}

	// Deduplicate PR numbers
	seen := make(map[int]struct{}, len(prNumbers))
	unique := make([]int, 0, len(prNumbers))
	for _, n := range prNumbers {
		if _, ok := seen[n]; !ok {
			seen[n] = struct{}{}
			unique = append(unique, n)
		}
	}

	query := buildPRStateBodyQuery(unique)
	variables := map[string]any{
		"owner": owner,
		"repo":  repo,
	}

	body, err := executeGraphQLQuery(ctx, runner, query, variables)
	if err != nil {
		return nil, err
	}

	return parsePRStateBodyResponse(body, unique)
}

// buildPRStateBodyQuery builds a GraphQL query to fetch state and body for multiple PRs by number.
func buildPRStateBodyQuery(prNumbers []int) string {
	var b strings.Builder
	b.WriteString("query($owner: String!, $repo: String!) {\n")
	b.WriteString("  repository(owner: $owner, name: $repo) {\n")
	for _, n := range prNumbers {
		fmt.Fprintf(&b, "    pr_%d: pullRequest(number: %d) { state body }\n", n, n)
	}
	b.WriteString("  }\n")
	b.WriteString("}\n")
	return b.String()
}

// parsePRStateBodyResponse parses the GraphQL response for PR state/body queries.
func parsePRStateBodyResponse(body []byte, prNumbers []int) (map[int]PRStateBody, error) {
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

	results := make(map[int]PRStateBody, len(prNumbers))
	for _, n := range prNumbers {
		alias := fmt.Sprintf("pr_%d", n)
		data, ok := repository[alias]
		if !ok || data == nil {
			continue
		}
		prData, ok := data.(map[string]any)
		if !ok {
			continue
		}
		var entry PRStateBody
		if state, ok := prData["state"].(string); ok {
			entry.State = state
		}
		if prBody, ok := prData["body"].(string); ok {
			entry.Body = prBody
		}
		results[n] = entry
	}

	return results, nil
}
