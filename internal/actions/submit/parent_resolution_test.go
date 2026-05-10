package submit

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestResolveSubmitParentNameSkipsWorktreeAnchors(t *testing.T) {
	t.Parallel()

	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{
			"wt-anchor": "main",
			"feature":   "wt-anchor",
		})

	err := s.Engine.SetBranchType(s.Engine.GetBranch("wt-anchor"), git.BranchTypeWorktreeAnchor)
	require.NoError(t, err)

	parent := resolveSubmitParentName(s.Engine, s.Engine.GetBranch("feature"))
	require.Equal(t, "main", parent)
}

func TestResolveSubmitParentNamePreservesNormalParents(t *testing.T) {
	t.Parallel()

	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{
			"parent": "main",
			"child":  "parent",
		})

	parent := resolveSubmitParentName(s.Engine, s.Engine.GetBranch("child"))
	require.Equal(t, "parent", parent)
}

func TestResolveSubmitParentNameSkipsNestedWorktreeAnchors(t *testing.T) {
	t.Parallel()

	// Stack: main -> anchor1 -> anchor2 -> feature
	// feature's submit parent should be main, skipping both anchors
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{
			"anchor1": "main",
			"anchor2": "anchor1",
			"feature": "anchor2",
		})

	err := s.Engine.SetBranchType(s.Engine.GetBranch("anchor1"), git.BranchTypeWorktreeAnchor)
	require.NoError(t, err)
	err = s.Engine.SetBranchType(s.Engine.GetBranch("anchor2"), git.BranchTypeWorktreeAnchor)
	require.NoError(t, err)

	parent := resolveSubmitParentName(s.Engine, s.Engine.GetBranch("feature"))
	require.Equal(t, "main", parent)
}
