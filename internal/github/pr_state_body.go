package github

import (
	"github.com/getstackit/stackit/internal/git"

	"context"
)

// PRStateBody holds the subset of pull-request fields needed to append a
// consolidation footer and decide whether a PR still needs closing.
type PRStateBody struct {
	State git.PRState
	Body  string
}

// BatchGetPRStateBodyGraphQL fetches the state and body for multiple PR numbers
// in a single GraphQL query, replacing one REST GetPullRequest call per number.
// Numbers with no matching PR are absent from the returned map.
func BatchGetPRStateBodyGraphQL(ctx context.Context, runner GitCommandRunner, owner, repo string, prNumbers []int) (map[int]PRStateBody, error) {
	if len(prNumbers) == 0 {
		return make(map[int]PRStateBody), nil
	}

	unique := uniquePRNumbers(prNumbers)

	query := buildPRStateBodyQuery(unique)
	variables := map[string]any{
		graphqlVarOwner: owner,
		graphqlVarRepo:  repo,
	}

	body, err := executeGraphQLQuery(ctx, runner, query, variables)
	if err != nil {
		return nil, err
	}

	return parsePRStateBodyResponse(body, unique)
}

// buildPRStateBodyQuery builds a GraphQL query to fetch state and body for multiple PRs by number.
func buildPRStateBodyQuery(prNumbers []int) string {
	return buildPRNumberQuery(prNumbers, "state body")
}

// parsePRStateBodyResponse parses the GraphQL response for PR state/body queries.
func parsePRStateBodyResponse(body []byte, prNumbers []int) (map[int]PRStateBody, error) {
	return parsePRNumberQueryResponse(body, prNumbers, func(prData map[string]any) (PRStateBody, bool) {
		var entry PRStateBody
		if state, ok := prData["state"].(string); ok {
			entry.State = git.PRState(state)
		}
		if prBody, ok := prData["body"].(string); ok {
			entry.Body = prBody
		}
		return entry, true
	})
}
