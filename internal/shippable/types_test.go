package shippable

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/getstackit/stackit/internal/actions/merge"
)

func TestStatus_Constants(t *testing.T) {
	t.Parallel()

	// Verify status string values are stable (important for any serialization)
	assert.Equal(t, Status("shippable"), StatusShippable)
	assert.Equal(t, Status("pending"), StatusPending)
	assert.Equal(t, Status("blocked"), StatusBlocked)
	assert.Equal(t, Status("incomplete"), StatusIncomplete)
}

func TestBlockingReason_Constants(t *testing.T) {
	t.Parallel()

	// Verify reason string values are stable
	assert.Equal(t, BlockingReason("changes_requested"), ReasonChangesRequested)
	assert.Equal(t, BlockingReason("ci_failing"), ReasonCIFailing)
	assert.Equal(t, BlockingReason("ci_pending"), ReasonCIPending)
	assert.Equal(t, BlockingReason("draft"), ReasonDraft)
	assert.Equal(t, BlockingReason("no_pr"), ReasonNoPR)
	assert.Equal(t, BlockingReason("review_required"), ReasonReviewRequired)
}

func TestStack_IsShippable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status Status
		want   bool
	}{
		{"shippable", StatusShippable, true},
		{"pending", StatusPending, false},
		{"blocked", StatusBlocked, false},
		{"incomplete", StatusIncomplete, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := &Stack{Status: tt.status}
			assert.Equal(t, tt.want, s.IsShippable())
		})
	}
}

func TestStack_IsPending(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status Status
		want   bool
	}{
		{"shippable", StatusShippable, false},
		{"pending", StatusPending, true},
		{"blocked", StatusBlocked, false},
		{"incomplete", StatusIncomplete, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := &Stack{Status: tt.status}
			assert.Equal(t, tt.want, s.IsPending())
		})
	}
}

func TestStack_IsBlocked(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status Status
		want   bool
	}{
		{"shippable", StatusShippable, false},
		{"pending", StatusPending, false},
		{"blocked", StatusBlocked, true},
		{"incomplete", StatusIncomplete, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := &Stack{Status: tt.status}
			assert.Equal(t, tt.want, s.IsBlocked())
		})
	}
}

func TestStack_IsIncomplete(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status Status
		want   bool
	}{
		{"shippable", StatusShippable, false},
		{"pending", StatusPending, false},
		{"blocked", StatusBlocked, false},
		{"incomplete", StatusIncomplete, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := &Stack{Status: tt.status}
			assert.Equal(t, tt.want, s.IsIncomplete())
		})
	}
}

func TestStack_BranchCount(t *testing.T) {
	t.Parallel()

	s := &Stack{
		Stack: merge.MultiStackInfo{
			AllBranches: []string{"branch1", "branch2", "branch3"},
		},
	}
	assert.Equal(t, 3, s.BranchCount())
}

func TestStack_RootBranch(t *testing.T) {
	t.Parallel()

	s := &Stack{
		Stack: merge.MultiStackInfo{
			RootBranch: "feature/my-stack",
		},
	}
	assert.Equal(t, "feature/my-stack", s.RootBranch())
}

func TestStack_DisplayTitle(t *testing.T) {
	t.Parallel()

	t.Run("returns PR title when available", func(t *testing.T) {
		t.Parallel()
		s := &Stack{
			Stack: merge.MultiStackInfo{
				RootBranch: "jonnii/20260131/feature",
			},
			PRTitle: "feat: add user authentication",
		}
		assert.Equal(t, "feat: add user authentication", s.DisplayTitle())
	})

	t.Run("falls back to branch name when no PR title", func(t *testing.T) {
		t.Parallel()
		s := &Stack{
			Stack: merge.MultiStackInfo{
				RootBranch: "jonnii/20260131/feature",
			},
			PRTitle: "",
		}
		assert.Equal(t, "jonnii/20260131/feature", s.DisplayTitle())
	})
}

func TestAnalysisResult_GetShippable(t *testing.T) {
	t.Parallel()

	result := &AnalysisResult{
		Stacks: []Stack{
			{Stack: merge.MultiStackInfo{RootBranch: "a"}, Status: StatusShippable},
			{Stack: merge.MultiStackInfo{RootBranch: "b"}, Status: StatusPending},
			{Stack: merge.MultiStackInfo{RootBranch: "c"}, Status: StatusShippable},
			{Stack: merge.MultiStackInfo{RootBranch: "d"}, Status: StatusBlocked},
		},
	}

	shippable := result.GetShippable()
	assert.Len(t, shippable, 2)
	assert.Equal(t, "a", shippable[0].RootBranch())
	assert.Equal(t, "c", shippable[1].RootBranch())
}

func TestAnalysisResult_GetPending(t *testing.T) {
	t.Parallel()

	result := &AnalysisResult{
		Stacks: []Stack{
			{Stack: merge.MultiStackInfo{RootBranch: "a"}, Status: StatusShippable},
			{Stack: merge.MultiStackInfo{RootBranch: "b"}, Status: StatusPending},
			{Stack: merge.MultiStackInfo{RootBranch: "c"}, Status: StatusPending},
		},
	}

	pending := result.GetPending()
	assert.Len(t, pending, 2)
	assert.Equal(t, "b", pending[0].RootBranch())
	assert.Equal(t, "c", pending[1].RootBranch())
}

func TestAnalysisResult_GetBlocked(t *testing.T) {
	t.Parallel()

	result := &AnalysisResult{
		Stacks: []Stack{
			{Stack: merge.MultiStackInfo{RootBranch: "a"}, Status: StatusBlocked},
			{Stack: merge.MultiStackInfo{RootBranch: "b"}, Status: StatusShippable},
		},
	}

	blocked := result.GetBlocked()
	assert.Len(t, blocked, 1)
	assert.Equal(t, "a", blocked[0].RootBranch())
}

func TestAnalysisResult_GetIncomplete(t *testing.T) {
	t.Parallel()

	result := &AnalysisResult{
		Stacks: []Stack{
			{Stack: merge.MultiStackInfo{RootBranch: "a"}, Status: StatusIncomplete},
			{Stack: merge.MultiStackInfo{RootBranch: "b"}, Status: StatusShippable},
		},
	}

	incomplete := result.GetIncomplete()
	assert.Len(t, incomplete, 1)
	assert.Equal(t, "a", incomplete[0].RootBranch())
}

func TestAnalysisResult_HasShippable(t *testing.T) {
	t.Parallel()

	t.Run("has shippable", func(t *testing.T) {
		t.Parallel()
		result := &AnalysisResult{ShippableCount: 2}
		assert.True(t, result.HasShippable())
	})

	t.Run("no shippable", func(t *testing.T) {
		t.Parallel()
		result := &AnalysisResult{ShippableCount: 0}
		assert.False(t, result.HasShippable())
	})
}

func TestAnalysisResult_TotalStacks(t *testing.T) {
	t.Parallel()

	result := &AnalysisResult{
		Stacks: []Stack{
			{Stack: merge.MultiStackInfo{RootBranch: "a"}},
			{Stack: merge.MultiStackInfo{RootBranch: "b"}},
			{Stack: merge.MultiStackInfo{RootBranch: "c"}},
		},
	}
	assert.Equal(t, 3, result.TotalStacks())
}
