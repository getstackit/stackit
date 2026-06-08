package github

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// PRContent is the current title and body of a pull request, fetched in bulk.
type PRContent struct {
	Title string
	Body  string
}

// BatchGetPRContentGraphQL fetches the current title and body for multiple PR
// numbers in a single GraphQL query, replacing one REST GetPullRequest call per
// PR. PRs absent from the response (e.g. not found) are omitted from the map.
func BatchGetPRContentGraphQL(ctx context.Context, runner GitCommandRunner, owner, repo string, prNumbers []int) (map[int]PRContent, error) {
	if len(prNumbers) == 0 {
		return make(map[int]PRContent), nil
	}

	seen := make(map[int]struct{}, len(prNumbers))
	unique := make([]int, 0, len(prNumbers))
	for _, n := range prNumbers {
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		unique = append(unique, n)
	}

	query := buildPRContentQuery(unique)
	variables := map[string]any{
		"owner": owner,
		"repo":  repo,
	}

	body, err := executeGraphQLQuery(ctx, runner, query, variables)
	if err != nil {
		return nil, err
	}

	return parsePRContentResponse(body, unique)
}

// buildPRContentQuery builds a GraphQL query to fetch title and body for
// multiple PRs by number.
func buildPRContentQuery(prNumbers []int) string {
	var b strings.Builder
	b.WriteString("query($owner: String!, $repo: String!) {\n")
	b.WriteString("  repository(owner: $owner, name: $repo) {\n")
	for _, n := range prNumbers {
		fmt.Fprintf(&b, "    pr_%d: pullRequest(number: %d) { title body }\n", n, n)
	}
	b.WriteString("  }\n")
	b.WriteString("}\n")
	return b.String()
}

// parsePRContentResponse parses the GraphQL response for PR content queries.
func parsePRContentResponse(body []byte, prNumbers []int) (map[int]PRContent, error) {
	var resp struct {
		Data   map[string]any `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse GraphQL response: %w", err)
	}

	repository, ok := resp.Data["repository"].(map[string]any)
	if !ok {
		if len(resp.Errors) > 0 {
			return nil, fmt.Errorf("graphql error: %s", resp.Errors[0].Message)
		}
		return nil, fmt.Errorf("invalid GraphQL response format: missing repository")
	}

	results := make(map[int]PRContent, len(prNumbers))
	for _, n := range prNumbers {
		alias := fmt.Sprintf("pr_%d", n)
		data, ok := repository[alias].(map[string]any)
		if !ok {
			continue
		}
		content := PRContent{}
		if title, ok := data["title"].(string); ok {
			content.Title = title
		}
		if prBody, ok := data["body"].(string); ok {
			content.Body = prBody
		}
		results[n] = content
	}

	return results, nil
}
