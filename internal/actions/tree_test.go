package actions

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/github"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestBuildTreeJSONHidesAnchorParents(t *testing.T) {
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
	s.CreateBranch("anchor").Commit("anchor")
	s.TrackBranch("anchor", "main")
	require.NoError(t, s.Engine.SetBranchType(s.Engine.GetBranch("anchor"), git.BranchTypeWorktreeAnchor))
	s.CreateBranch("feature").Commit("feature")
	s.TrackBranch("feature", "anchor")

	result := BuildTreeJSON(s.Context, TreeOptions{})
	info := make(map[string]TreeBranchInfo, len(result.Branches))
	for _, branch := range result.Branches {
		info[branch.Name] = branch
	}

	_, anchorPresent := info["anchor"]
	require.False(t, anchorPresent)
	require.Equal(t, "main", info["feature"].Parent)
	require.Contains(t, info["main"].Children, "feature")
}

type treeStatsEngine struct {
	engine.Engine
	reads int
	names []string
}

func (e *treeStatsEngine) BatchBranchStats(branches engine.Branches) map[string]engine.BranchStat {
	e.reads++
	e.names = branches.Names()
	return e.Engine.BatchBranchStats(branches)
}

type treeChecksClient struct {
	github.Client
	reads int
	names []string
}

func (c *treeChecksClient) BatchGetPRChecksStatus(_ context.Context, names []string) (github.ChecksByBranch, error) {
	c.reads++
	c.names = names
	return nil, nil
}

func TestTreeShortJSONSkipsEnrichment(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).WithLinearStack3()
	require.NoError(t, s.Engine.UpsertPrInfo(context.Background(), s.Engine.GetBranch("b"),
		testhelpers.NewTestPrInfoEmpty().WithNumber(new(42))))
	eng := &treeStatsEngine{Engine: s.Engine}
	client := &treeChecksClient{}
	s.Context.Engine = eng
	s.Context.GitHubClient = client

	for _, style := range []TreeStyle{TreeStyleShort, TreeStyleNormal, TreeStyleFull} {
		result := BuildTreeJSON(s.Context, TreeOptions{Style: style})
		data, err := json.Marshal(result)
		require.NoError(t, err)
		var decoded map[string]any
		require.NoError(t, json.Unmarshal(data, &decoded))
		if style == TreeStyleShort {
			require.Zero(t, eng.reads)
			require.Zero(t, client.reads)
			require.NotContains(t, decoded, "github_available")
			require.NotContains(t, decoded["summary"], "approved_count")
			require.NotContains(t, decoded["summary"], "in_review_count")
			for _, branch := range decoded["branches"].([]any) {
				for _, field := range []string{"commits", "additions", "deletions", "pr"} {
					require.NotContains(t, branch, field)
				}
				require.Contains(t, branch, "needs_restack")
				require.Contains(t, branch, "is_locked")
			}
		} else {
			require.True(t, *result.GitHubAvailable)
			require.NotNil(t, result.Summary.ApprovedCount)
			for _, branch := range result.Branches {
				require.NotNil(t, branch.Commits, "normal/full retain zero counts, including trunk")
			}
		}
	}
	require.Equal(t, 2, eng.reads)
	require.Equal(t, 2, client.reads)
}

func TestTreeJSONBoundsEnrichment(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).WithLinearStack3()
	s.Checkout("a").CreateBranch("sibling").Commit("sibling").TrackBranch("sibling", "a")
	s.Checkout("b")
	for i, name := range []string{"a", "b", "c", "sibling"} {
		require.NoError(t, s.Engine.UpsertPrInfo(context.Background(), s.Engine.GetBranch(name),
			testhelpers.NewTestPrInfoEmpty().WithNumber(new(i+1))))
	}
	eng := &treeStatsEngine{Engine: s.Engine}
	client := &treeChecksClient{}
	s.Context.Engine = eng
	s.Context.GitHubClient = client
	result := BuildTreeJSON(s.Context, TreeOptions{BranchName: "b", Steps: new(1)})
	names := make([]string, 0, len(result.Branches))
	for _, branch := range result.Branches {
		names = append(names, branch.Name)
	}
	require.ElementsMatch(t, []string{"a", "b", "c"}, names)
	require.ElementsMatch(t, names, eng.names)
	require.ElementsMatch(t, names, client.names)
	require.Equal(t, 3, result.Summary.TotalBranches)

	// A scoped unbounded view retains ancestors but excludes sibling stacks.
	result = BuildTreeJSON(s.Context, TreeOptions{BranchName: "b", Style: TreeStyleShort})
	names = names[:0]
	for _, branch := range result.Branches {
		names = append(names, branch.Name)
	}
	require.ElementsMatch(t, []string{"a", "b", "c"}, names)
}
