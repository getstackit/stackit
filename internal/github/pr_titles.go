package github

import (
	"context"
)

// BatchGetPRTitlesGraphQL fetches PR titles for multiple PR numbers using a single GraphQL query.
func BatchGetPRTitlesGraphQL(ctx context.Context, runner GitCommandRunner, repo Repo, prNumbers []int) (map[int]string, error) {
	if len(prNumbers) == 0 {
		return make(map[int]string), nil
	}

	unique := uniquePRNumbers(prNumbers)

	query := buildPRTitlesQuery(unique)
	variables := map[string]any{
		graphqlVarOwner: repo.Owner,
		graphqlVarRepo:  repo.Name,
	}

	body, err := executeGraphQLQuery(ctx, runner, query, variables)
	if err != nil {
		return nil, err
	}

	return parsePRTitlesResponse(body, unique)
}

// buildPRTitlesQuery builds a GraphQL query to fetch titles for multiple PRs by number.
func buildPRTitlesQuery(prNumbers []int) string {
	return buildPRNumberQuery(prNumbers, "title")
}

// parsePRTitlesResponse parses the GraphQL response for PR title queries.
func parsePRTitlesResponse(body []byte, prNumbers []int) (map[int]string, error) {
	return parsePRNumberQueryResponse(body, prNumbers, func(prData map[string]any) (string, bool) {
		if title, ok := prData["title"].(string); ok {
			return title, true
		}
		return "", false
	})
}
