package dashboard

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions/merge"
	"github.com/getstackit/stackit/internal/shippable"
	"github.com/getstackit/stackit/internal/tui/core"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestCompanionViewStaysNarrow(t *testing.T) {
	t.Parallel()

	m := &companionModel{
		BaseModel: core.BaseModel{Width: 40},
		analysis: &shippable.AnalysisResult{Stacks: []shippable.Stack{{
			Stack:  merge.MultiStackInfo{RootBranch: "very-long-branch-name-for-a-narrow-panel", AllBranches: []string{"very-long-branch-name-for-a-narrow-panel"}},
			Status: shippable.StatusShippable,
		}}},
		workingTree: workingTreeSummary{staged: 2, unstaged: 1, untracked: 3},
	}

	view := m.View()
	for _, line := range strings.Split(view.Content, "\n") {
		require.LessOrEqual(t, lipgloss.Width(line), 40)
	}
	require.Contains(t, view.Content, "staged 2")
	require.Contains(t, view.Content, "…")
}

func TestCompanionTreeRendersTrunkOnce(t *testing.T) {
	t.Parallel()

	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{"P1": "main", "P2": "main"})
	analysis, err := shippable.NewAnalyzer(s.Engine, nil).AnalyzeAllLocal()
	require.NoError(t, err)

	m := &companionModel{
		engine:   s.Engine,
		analysis: analysis,
		renderer: buildCompanionRenderer(s.Engine),
	}
	content := strings.Join(m.renderStackTree(80), "\n")

	require.Equal(t, 1, strings.Count(content, "main"))
	require.Contains(t, content, "P1")
	require.Contains(t, content, "P2")
}
