package github

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/getstackit/stackit/internal/git"
)

func uniquePRNumbers(prNumbers []git.PRNumber) []git.PRNumber {
	seen := make(map[git.PRNumber]struct{}, len(prNumbers))
	unique := make([]git.PRNumber, 0, len(prNumbers))
	for _, n := range prNumbers {
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		unique = append(unique, n)
	}
	return unique
}

func buildPRNumberQuery(prNumbers []git.PRNumber, fields string) string {
	var b strings.Builder
	b.WriteString("query($owner: String!, $repo: String!) {\n")
	b.WriteString("  repository(owner: $owner, name: $repo) {\n")
	for _, n := range prNumbers {
		fmt.Fprintf(&b, "    pr_%d: pullRequest(number: %d) { %s }\n", n, n, fields)
	}
	b.WriteString("  }\n")
	b.WriteString("}\n")
	return b.String()
}

func parsePRNumberQueryResponse[T any](body []byte, prNumbers []git.PRNumber, decode func(map[string]any) (T, bool)) (map[git.PRNumber]T, error) {
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

	results := make(map[git.PRNumber]T, len(prNumbers))
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
		if value, ok := decode(prData); ok {
			results[n] = value
		}
	}
	return results, nil
}
