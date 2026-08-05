package actions

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
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
