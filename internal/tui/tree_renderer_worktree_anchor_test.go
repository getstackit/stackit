package tui_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/tui"
	"github.com/getstackit/stackit/internal/tui/components/tree"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestNewStackTreeRendererHidesWorktreeAnchorKeepsDescendants(t *testing.T) {
	t.Parallel()

	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{
			"wt-anchor": "main",
			"feature":   "wt-anchor",
		})

	err := s.Engine.SetBranchType(s.Engine.GetBranch("wt-anchor"), git.BranchTypeWorktreeAnchor)
	require.NoError(t, err)

	renderer := tui.NewStackTreeRenderer(s.Engine)
	lines := renderer.RenderStack("main", tree.RenderOptions{
		Mode:                tree.RenderModeCompact,
		NoStyleBranchName:   true,
		SkipSelectionPrefix: true,
	})

	output := strings.Join(lines, "\n")
	require.NotContains(t, output, "wt-anchor", "worktree anchors should stay hidden in log output")
	require.Contains(t, output, "feature", "children of hidden worktree anchors must remain visible")
}

func TestNewStackTreeRendererHidesNestedWorktreeAnchorsKeepsDescendants(t *testing.T) {
	t.Parallel()

	// Stack: main -> anchor1 -> anchor2 -> feature
	// Both anchors should be hidden, feature should appear as direct child of main
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

	renderer := tui.NewStackTreeRenderer(s.Engine)
	lines := renderer.RenderStack("main", tree.RenderOptions{
		Mode:                tree.RenderModeCompact,
		NoStyleBranchName:   true,
		SkipSelectionPrefix: true,
	})

	output := strings.Join(lines, "\n")
	require.NotContains(t, output, "anchor1", "nested worktree anchors should be hidden")
	require.NotContains(t, output, "anchor2", "nested worktree anchors should be hidden")
	require.Contains(t, output, "feature", "descendants of nested anchors must remain visible")
}
