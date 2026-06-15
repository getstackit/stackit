package github

import (
	"context"
)

// BatchGetPRNodeIDsGraphQL fetches PR node IDs for multiple PR numbers using a
// single GraphQL query, replacing one REST GetPullRequest call per number.
// Numbers with no matching PR are simply absent from the returned map.
func BatchGetPRNodeIDsGraphQL(ctx context.Context, runner GitCommandRunner, owner, repo string, prNumbers []int) (map[int]string, error) {
	if len(prNumbers) == 0 {
		return make(map[int]string), nil
	}

	unique := uniquePRNumbers(prNumbers)

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
	return buildPRNumberQuery(prNumbers, "id")
}

// parsePRNodeIDsResponse parses the GraphQL response for PR node ID queries.
func parsePRNodeIDsResponse(body []byte, prNumbers []int) (map[int]string, error) {
	return parsePRNumberQueryResponse(body, prNumbers, func(prData map[string]any) (string, bool) {
		if id, ok := prData["id"].(string); ok {
			return id, true
		}
		return "", false
	})
}
