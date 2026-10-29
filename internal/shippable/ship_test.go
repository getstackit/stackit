package shippable

import (
	"testing"

	"github.com/getstackit/stackit/internal/actions/stackview"
	"github.com/getstackit/stackit/internal/git"
	"github.com/stretchr/testify/require"
)

func TestShipResult_Fields(t *testing.T) {
	t.Parallel()
	result := &ShipResult{
		IncludedStacks: []Stack{
			{Stack: stackview.StackInfo{RootBranch: "a"}},
			{Stack: stackview.StackInfo{RootBranch: "b"}},
		},
		ExcludedStacks: []ExcludedStack{
			{Stack: Stack{Stack: stackview.StackInfo{RootBranch: "c"}}, Reason: ReasonMergeConflict},
		},
		PRNumber:   123,
		PRURL:      "https://github.com/org/repo/pull/123",
		BranchName: "consolidation-123",
	}

	require.Len(t, result.IncludedStacks, 2)
	require.Len(t, result.ExcludedStacks, 1)
	require.Equal(t, git.PRNumber(123), result.PRNumber)
	require.Equal(t, "https://github.com/org/repo/pull/123", result.PRURL)
	require.Equal(t, "consolidation-123", result.BranchName)
}

func TestShipOptions_Defaults(t *testing.T) {
	t.Parallel()
	opts := ShipOptions{}
	require.False(t, opts.SkipLocalCI)
	require.False(t, opts.Wait)
}

func TestShipOptions_WithValues(t *testing.T) {
	t.Parallel()
	opts := ShipOptions{
		SkipLocalCI: true,
		Wait:        true,
	}
	require.True(t, opts.SkipLocalCI)
	require.True(t, opts.Wait)
}
