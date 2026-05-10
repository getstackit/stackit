package shippable

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/getstackit/stackit/internal/actions/merge"
)

func TestShipResult_Fields(t *testing.T) {
	result := &ShipResult{
		IncludedStacks: []Stack{
			{Stack: merge.MultiStackInfo{RootBranch: "a"}},
			{Stack: merge.MultiStackInfo{RootBranch: "b"}},
		},
		ExcludedStacks: []ExcludedStack{
			{Stack: Stack{Stack: merge.MultiStackInfo{RootBranch: "c"}}, Reason: ReasonMergeConflict},
		},
		PRNumber:   123,
		PRURL:      "https://github.com/org/repo/pull/123",
		BranchName: "consolidation-123",
	}

	assert.Equal(t, 2, len(result.IncludedStacks))
	assert.Equal(t, 1, len(result.ExcludedStacks))
	assert.Equal(t, 123, result.PRNumber)
	assert.Equal(t, "https://github.com/org/repo/pull/123", result.PRURL)
	assert.Equal(t, "consolidation-123", result.BranchName)
}

func TestShipOptions_Defaults(t *testing.T) {
	opts := ShipOptions{}
	assert.False(t, opts.SkipLocalCI)
	assert.False(t, opts.Wait)
}

func TestShipOptions_WithValues(t *testing.T) {
	opts := ShipOptions{
		SkipLocalCI: true,
		Wait:        true,
	}
	assert.True(t, opts.SkipLocalCI)
	assert.True(t, opts.Wait)
}
