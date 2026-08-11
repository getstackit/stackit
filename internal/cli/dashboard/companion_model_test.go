package dashboard

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/shippable"
)

func TestCompanionReloadMessageUpdatesLocalState(t *testing.T) {
	t.Parallel()

	m := &companionModel{loading: true}
	workingTree := workingTreeSummary{staged: 2, unstaged: 1, untracked: 3}
	analysis := &shippable.AnalysisResult{}

	updated, cmd := m.Update(companionReloadMsg{
		analysis:    analysis,
		workingTree: workingTree,
	})

	require.Same(t, m, updated)
	require.Nil(t, cmd)
	require.False(t, m.loading)
	require.Same(t, analysis, m.analysis)
	require.Equal(t, workingTree, m.workingTree)
	require.False(t, m.lastRefresh.IsZero())
}
