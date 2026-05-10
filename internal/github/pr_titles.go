package github

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/getstackit/stackit/internal/git"
)

// BatchGetPRTitlesGraphQL fetches PR titles for multiple PR numbers using a single GraphQL query.
func BatchGetPRTitlesGraphQL(ctx context.Context, runner git.Runner, owner, repo string, prNumbers []int) (map[int]string, error) {
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

	query := buildPRTitlesQuery(unique)
	variables := map[string]any{
		"owner": owner,
		"repo":  repo,
	}

	body, err := executeGraphQLQuery(ctx, runner, query, variables)
	if err != nil {
		return nil, err
	}

	return parsePRTitlesResponse(body, unique)
}

// buildPRTitlesQuery builds a GraphQL query to fetch titles for multiple PRs by number.
func buildPRTitlesQuery(prNumbers []int) string {
	var b strings.Builder
	b.WriteString("query($owner: String!, $repo: String!) {\n")
	b.WriteString("  repository(owner: $owner, name: $repo) {\n")
	for _, n := range prNumbers {
		fmt.Fprintf(&b, "    pr_%d: pullRequest(number: %d) { title }\n", n, n)
	}
	b.WriteString("  }\n")
	b.WriteString("}\n")
	return b.String()
}

// parsePRTitlesResponse parses the GraphQL response for PR title queries.
func parsePRTitlesResponse(body []byte, prNumbers []int) (map[int]string, error) {
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
		if title, ok := prData["title"].(string); ok {
			results[n] = title
		}
	}

	return results, nil
}
