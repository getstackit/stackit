package github

import (
	"github.com/getstackit/stackit/internal/git"

	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseCheckRollup_DeduplicatesByName(t *testing.T) {
	t.Parallel()

	// Simulate GitHub returning multiple runs of the same check
	// (happens when a check is re-run after failure)
	rollup := map[string]any{
		"contexts": map[string]any{
			"nodes": []any{
				// Older run that failed
				map[string]any{
					"__typename":  "CheckRun",
					"name":        "Test",
					"status":      "COMPLETED",
					"conclusion":  "FAILURE",
					"startedAt":   "2024-01-01T10:00:00Z",
					"completedAt": "2024-01-01T10:05:00Z",
				},
				// Newer run that succeeded (re-run)
				map[string]any{
					"__typename":  "CheckRun",
					"name":        "Test",
					"status":      "COMPLETED",
					"conclusion":  "SUCCESS",
					"startedAt":   "2024-01-01T10:10:00Z",
					"completedAt": "2024-01-01T10:15:00Z",
				},
			},
		},
	}

	status := parseCheckRollup(rollup, "APPROVED", "author")

	require.NotNil(t, status)
	require.True(t, status.Passing, "should use the newer successful run, not the older failed one")
	require.False(t, status.Pending)
	require.Len(t, status.Checks, 1, "should deduplicate to single check")
	require.Equal(t, "Test", status.Checks[0].Name)
	require.Equal(t, CheckConclusionSuccess, status.Checks[0].Conclusion)
}

func TestParseCheckRollup_KeepsLatestByFinishedAt(t *testing.T) {
	t.Parallel()

	// Test that we keep the most recently finished check, not just any check
	rollup := map[string]any{
		"contexts": map[string]any{
			"nodes": []any{
				// This one finished later but started earlier
				map[string]any{
					"__typename":  "CheckRun",
					"name":        "Build",
					"status":      "COMPLETED",
					"conclusion":  "SUCCESS",
					"startedAt":   "2024-01-01T10:00:00Z",
					"completedAt": "2024-01-01T10:30:00Z", // finished last
				},
				// This one started later but finished earlier
				map[string]any{
					"__typename":  "CheckRun",
					"name":        "Build",
					"status":      "COMPLETED",
					"conclusion":  "FAILURE",
					"startedAt":   "2024-01-01T10:10:00Z",
					"completedAt": "2024-01-01T10:20:00Z", // finished first
				},
			},
		},
	}

	status := parseCheckRollup(rollup, "APPROVED", "author")

	require.NotNil(t, status)
	require.True(t, status.Passing, "should use the check that finished last")
	require.Len(t, status.Checks, 1)
	require.Equal(t, CheckConclusionSuccess, status.Checks[0].Conclusion)
	expectedTime, _ := time.Parse(time.RFC3339, "2024-01-01T10:30:00Z")
	require.Equal(t, expectedTime, status.Checks[0].FinishedAt)
}

func TestParseCheckRollup_FiltersStackitChecks(t *testing.T) {
	t.Parallel()

	rollup := map[string]any{
		"contexts": map[string]any{
			"nodes": []any{
				// Stackit's own checks that should be ignored
				map[string]any{
					"__typename":  "CheckRun",
					"name":        "Check Lock Status",
					"status":      "COMPLETED",
					"conclusion":  "FAILURE",
					"completedAt": "2024-01-01T10:00:00Z",
				},
				map[string]any{
					"__typename":  "CheckRun",
					"name":        "Check Stack Order",
					"status":      "COMPLETED",
					"conclusion":  "FAILURE",
					"completedAt": "2024-01-01T10:00:00Z",
				},
				// Real CI check that passes
				map[string]any{
					"__typename":  "CheckRun",
					"name":        "CI",
					"status":      "COMPLETED",
					"conclusion":  "SUCCESS",
					"completedAt": "2024-01-01T10:00:00Z",
				},
			},
		},
	}

	status := parseCheckRollup(rollup, "APPROVED", "author")

	require.NotNil(t, status)
	require.True(t, status.Passing, "should ignore stackit checks and report passing")
	require.Len(t, status.Checks, 1, "should only include the CI check")
	require.Equal(t, "CI", status.Checks[0].Name)
}

func TestParseBranchStatus_ExtractsPRState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		prState       string
		expectedState git.PRState
	}{
		{"open PR", "OPEN", "OPEN"},
		{"merged PR", "MERGED", "MERGED"},
		{"closed PR", "CLOSED", "CLOSED"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			data := map[string]any{
				"nodes": []any{
					map[string]any{
						"state":          tt.prState,
						"author":         map[string]any{"login": "testuser"},
						"reviewDecision": "APPROVED",
					},
				},
			}

			status := parseBranchStatus(data)
			require.NotNil(t, status)
			require.Equal(t, tt.expectedState, status.State)
		})
	}
}

func TestParseBranchStatus_NoPRReturnsNil(t *testing.T) {
	t.Parallel()

	data := map[string]any{
		"nodes": []any{},
	}

	status := parseBranchStatus(data)
	require.Nil(t, status)
}

func TestParseCheckRollup_MultipleDistinctChecks(t *testing.T) {
	t.Parallel()

	rollup := map[string]any{
		"contexts": map[string]any{
			"nodes": []any{
				map[string]any{
					"__typename":  "CheckRun",
					"name":        "Build",
					"status":      "COMPLETED",
					"conclusion":  "SUCCESS",
					"completedAt": "2024-01-01T10:00:00Z",
				},
				map[string]any{
					"__typename":  "CheckRun",
					"name":        "Test",
					"status":      "COMPLETED",
					"conclusion":  "SUCCESS",
					"completedAt": "2024-01-01T10:00:00Z",
				},
				map[string]any{
					"__typename":  "CheckRun",
					"name":        "Lint",
					"status":      "COMPLETED",
					"conclusion":  "SUCCESS",
					"completedAt": "2024-01-01T10:00:00Z",
				},
			},
		},
	}

	status := parseCheckRollup(rollup, "APPROVED", "author")

	require.NotNil(t, status)
	require.True(t, status.Passing)
	require.Len(t, status.Checks, 3, "should include all distinct checks")
}

func TestParseCheckRollup_ChecksSortedByName(t *testing.T) {
	t.Parallel()

	names := []string{"lint", "build", "test", "deploy", "analyze", "docs"}
	nodes := make([]any, 0, len(names))
	for _, name := range names {
		nodes = append(nodes, map[string]any{
			"__typename":  "CheckRun",
			"name":        name,
			"status":      "COMPLETED",
			"conclusion":  "SUCCESS",
			"startedAt":   "2024-01-01T10:00:00Z",
			"completedAt": "2024-01-01T10:05:00Z",
		})
	}
	rollup := map[string]any{"contexts": map[string]any{"nodes": nodes}}

	// Map iteration order is random, so repeat to make an unsorted result fail.
	for range 20 {
		status := parseCheckRollup(rollup, "APPROVED", "author")
		got := make([]string, len(status.Checks))
		for i, check := range status.Checks {
			got[i] = check.Name
		}
		require.Equal(t, []string{"analyze", "build", "deploy", "docs", "lint", "test"}, got)
	}
}

func TestBuildPRStatusQuery_PassesBranchNamesAsVariables(t *testing.T) {
	t.Parallel()

	// A double quote is legal in a Git ref name; spliced into the query text it
	// would terminate the string literal and break the whole batch.
	branches := []string{"feature", `odd"name`, "feat/a", "feat-a"}
	query, variables := buildPRStatusQuery(Repo{Owner: "octo", Name: "repo"}, branches)

	require.Contains(t, query, "query($owner: String!, $repo: String!, $b0: String!, $b1: String!, $b2: String!, $b3: String!)")
	require.Contains(t, query, "b0: pullRequests(headRefName: $b0,")
	require.Contains(t, query, "b3: pullRequests(headRefName: $b3,")
	require.NotContains(t, query, `odd"name`)

	require.Equal(t, "octo", variables[graphqlVarOwner])
	require.Equal(t, "repo", variables[graphqlVarRepo])
	for i, name := range branches {
		require.Equal(t, name, variables[fmt.Sprintf("b%d", i)])
	}
}

func TestParsePRStatusResponse_MapsAliasesByIndex(t *testing.T) {
	t.Parallel()

	body := []byte(`{
		"data": {
			"repository": {
				"b0": {"nodes": [{"state": "OPEN", "reviewDecision": "APPROVED"}]},
				"b1": {"nodes": []},
				"b2": null
			}
		}
	}`)

	results, err := parsePRStatusResponse(body, []string{"feat/a", "feat-a", `odd"name`})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.NotNil(t, results.Get("feat/a"))
	require.Equal(t, ReviewDecisionApproved, results.Get("feat/a").ReviewDecision)
	require.Equal(t, git.PRStateOpen, results.Get("feat/a").State)
}
