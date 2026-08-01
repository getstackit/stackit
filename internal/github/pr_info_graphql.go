package github

import (
	"github.com/getstackit/stackit/internal/git"

	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const pullRequestInfoFields = "number title body state url isDraft baseRefName headRefName"

// batchGetPRInfoByBranchGraphQL fetches the most recent pull request associated
// (as head ref) with each branch in a single GraphQL query, replacing one REST
// list call per branch. Branches with no associated PR are absent from the
// returned map.
func batchGetPRInfoByBranchGraphQL(ctx context.Context, runner GitCommandRunner, repo Repo, branchNames []string) (map[string]*PullRequestInfo, error) {
	results := make(map[string]*PullRequestInfo, len(branchNames))
	if len(branchNames) == 0 {
		return results, nil
	}

	// Deduplicate while keeping a stable order for alias assignment.
	seen := make(map[string]struct{}, len(branchNames))
	unique := make([]string, 0, len(branchNames))
	for _, n := range branchNames {
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		unique = append(unique, n)
	}

	query, variables := buildPRInfoByBranchQuery(repo, unique)
	body, err := executeGraphQLQuery(ctx, runner, query, variables)
	if err != nil {
		return nil, err
	}
	return parsePRInfoByBranchResponse(body, unique)
}

// buildPRInfoByBranchQuery builds a GraphQL query that resolves each branch's
// head PR under a b<index> alias. Branch names are passed as variables so names
// containing slashes or other characters need no escaping.
func buildPRInfoByBranchQuery(repo Repo, branches []string) (string, map[string]any) {
	var b strings.Builder
	b.WriteString("query($owner: String!, $repo: String!")
	for i := range branches {
		fmt.Fprintf(&b, ", $b%d: String!", i)
	}
	b.WriteString(") {\n")
	b.WriteString("  repository(owner: $owner, name: $repo) {\n")
	for i := range branches {
		fmt.Fprintf(&b, "    b%d: ref(qualifiedName: $b%d) {\n", i, i)
		b.WriteString("      associatedPullRequests(first: 1, orderBy: {field: CREATED_AT, direction: DESC}) {\n")
		b.WriteString("        nodes { " + pullRequestInfoFields + " }\n")
		b.WriteString("      }\n")
		b.WriteString("    }\n")
	}
	b.WriteString("  }\n")
	b.WriteString("}\n")

	variables := map[string]any{
		graphqlVarOwner: repo.Owner,
		graphqlVarRepo:  repo.Name,
	}
	for i, name := range branches {
		variables[fmt.Sprintf("b%d", i)] = "refs/heads/" + name
	}
	return b.String(), variables
}

// supplementPRInfoByNumberGraphQL resolves PRs for branches whose head refs
// are no longer present. A missing ref is not itself a deletion signal: only a
// previously recorded PR number is eligible for this supplemental lookup.
func supplementPRInfoByNumberGraphQL(ctx context.Context, runner GitCommandRunner, repo Repo, infos map[string]*PullRequestInfo, knownPRNumbers map[string]int) error {
	if len(knownPRNumbers) == 0 {
		return nil
	}

	prNumbers := make([]int, 0, len(knownPRNumbers))
	for branchName, number := range knownPRNumbers {
		if number > 0 && infos[branchName] == nil {
			prNumbers = append(prNumbers, number)
		}
	}
	if len(prNumbers) == 0 {
		return nil
	}

	query := buildPRNumberQuery(uniquePRNumbers(prNumbers), pullRequestInfoFields)
	variables := map[string]any{
		graphqlVarOwner: repo.Owner,
		graphqlVarRepo:  repo.Name,
	}
	body, err := executeGraphQLQuery(ctx, runner, query, variables)
	if err != nil {
		return err
	}

	byNumber, err := parsePRInfoByNumberResponse(body, prNumbers)
	if err != nil {
		return err
	}
	addPRInfoForKnownBranches(infos, knownPRNumbers, byNumber)
	return nil
}

func parsePRInfoByNumberResponse(body []byte, prNumbers []int) (map[int]*PullRequestInfo, error) {
	return parsePRNumberQueryResponse(body, prNumbers, func(prData map[string]any) (*PullRequestInfo, bool) {
		return pullRequestInfoFromGraphQLNode(prData), true
	})
}

func addPRInfoForKnownBranches(infos map[string]*PullRequestInfo, knownPRNumbers map[string]int, byNumber map[int]*PullRequestInfo) {
	for branchName, number := range knownPRNumbers {
		if infos[branchName] != nil {
			continue
		}
		if info := byNumber[number]; info != nil {
			infos[branchName] = info
		}
	}
}

// parsePRInfoByBranchResponse maps each b<index> alias back to its branch name.
// A missing ref or a ref with no associated PR is skipped (left out of the map),
// matching the prior per-branch behavior where such branches kept their existing
// local PR info.
func parsePRInfoByBranchResponse(body []byte, branches []string) (map[string]*PullRequestInfo, error) {
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
		// No repository payload at all is a hard failure (bad repo, auth, etc.).
		// Per-ref nulls do not land here — ref() simply returns null for a
		// missing branch without producing a top-level error.
		if len(resp.Errors) > 0 {
			return nil, fmt.Errorf("graphql error: %s", resp.Errors[0].Message)
		}
		return nil, fmt.Errorf("invalid GraphQL response format: missing repository")
	}

	results := make(map[string]*PullRequestInfo, len(branches))
	for i, name := range branches {
		alias := fmt.Sprintf("b%d", i)
		refData, ok := repository[alias].(map[string]any)
		if !ok {
			continue
		}
		assoc, ok := refData["associatedPullRequests"].(map[string]any)
		if !ok {
			continue
		}
		nodes, ok := assoc["nodes"].([]any)
		if !ok || len(nodes) == 0 {
			continue
		}
		node, ok := nodes[0].(map[string]any)
		if !ok {
			continue
		}
		results[name] = pullRequestInfoFromGraphQLNode(node)
	}
	return results, nil
}

// pullRequestInfoFromGraphQLNode converts a single associatedPullRequests node
// into a PullRequestInfo. State is normalized to upper case to match the REST
// path (ToPullRequestInfo); the GraphQL PullRequestState enum is already upper
// case (OPEN/CLOSED/MERGED).
func pullRequestInfoFromGraphQLNode(node map[string]any) *PullRequestInfo {
	info := &PullRequestInfo{}
	if v, ok := node["number"].(float64); ok {
		info.Number = int(v)
	}
	if v, ok := node["title"].(string); ok {
		info.Title = v
	}
	if v, ok := node["body"].(string); ok {
		info.Body = v
	}
	if v, ok := node["state"].(string); ok {
		info.State = git.PRState(strings.ToUpper(v))
	}
	if v, ok := node["url"].(string); ok {
		info.HTMLURL = v
	}
	if v, ok := node["isDraft"].(bool); ok {
		info.Draft = v
	}
	if v, ok := node["baseRefName"].(string); ok {
		info.Base = v
	}
	if v, ok := node["headRefName"].(string); ok {
		info.Head = v
	}
	return info
}
