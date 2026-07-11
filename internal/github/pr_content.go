package github

import (
	"context"
)

// PRContent is the current title and body of a pull request, fetched in bulk.
type PRContent struct {
	Title string
	Body  string
}

// BatchGetPRContentGraphQL fetches the current title and body for multiple PR
// numbers in a single GraphQL query, replacing one REST GetPullRequest call per
// PR. PRs absent from the response (e.g. not found) are omitted from the map.
func BatchGetPRContentGraphQL(ctx context.Context, runner GitCommandRunner, repo Repo, prNumbers []int) (map[int]PRContent, error) {
	if len(prNumbers) == 0 {
		return make(map[int]PRContent), nil
	}

	unique := uniquePRNumbers(prNumbers)

	query := buildPRContentQuery(unique)
	variables := map[string]any{
		graphqlVarOwner: repo.Owner,
		graphqlVarRepo:  repo.Name,
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
	return buildPRNumberQuery(prNumbers, "title body")
}

// parsePRContentResponse parses the GraphQL response for PR content queries.
func parsePRContentResponse(body []byte, prNumbers []int) (map[int]PRContent, error) {
	return parsePRNumberQueryResponse(body, prNumbers, func(data map[string]any) (PRContent, bool) {
		content := PRContent{}
		if title, ok := data["title"].(string); ok {
			content.Title = title
		}
		if prBody, ok := data["body"].(string); ok {
			content.Body = prBody
		}
		return content, true
	})
}
