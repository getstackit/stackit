package github

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// BatchGetPRNodeIDsGraphQL fetches PR node IDs for multiple PR numbers using a
// single GraphQL query, replacing one REST GetPullRequest call per number.
// Numbers with no matching PR are simply absent from the returned map.
func BatchGetPRNodeIDsGraphQL(ctx context.Context, runner GitCommandRunner, owner, repo string, prNumbers []int) (map[int]string, error) {
	if len(prNumbers) == 0 {
		return make(map[int]string), nil
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

	query := buildPRNodeIDsQuery(unique)
	variables := map[string]any{
		"owner": owner,
		"repo":  repo,
	}

	body, err := executeGraphQLQuery(ctx, runner, query, variables)
	if err != nil {
		return nil, err
	}

	return parsePRNodeIDsResponse(body, unique)
}

// buildPRNodeIDsQuery builds a GraphQL query to fetch node IDs for multiple PRs by number.
func buildPRNodeIDsQuery(prNumbers []int) string {
	var b strings.Builder
	b.WriteString("query($owner: String!, $repo: String!) {\n")
	b.WriteString("  repository(owner: $owner, name: $repo) {\n")
	for _, n := range prNumbers {
		fmt.Fprintf(&b, "    pr_%d: pullRequest(number: %d) { id }\n", n, n)
	}
	b.WriteString("  }\n")
	b.WriteString("}\n")
	return b.String()
}

// parsePRNodeIDsResponse parses the GraphQL response for PR node ID queries.
func parsePRNodeIDsResponse(body []byte, prNumbers []int) (map[int]string, error) {
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

	results := make(map[int]string, len(prNumbers))
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
		if id, ok := prData["id"].(string); ok {
			results[n] = id
		}
	}

	return results, nil
}
