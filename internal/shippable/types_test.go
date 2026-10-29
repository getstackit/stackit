package shippable

import (
	"testing"

	"github.com/getstackit/stackit/internal/actions/stackview"
	"github.com/stretchr/testify/require"
)

func TestStatus_Constants(t *testing.T) {
	t.Parallel()

	// Verify status string values are stable (important for any serialization)
	require.Equal(t, StatusShippable, Status("shippable"))
	require.Equal(t, StatusPending, Status("pending"))
	require.Equal(t, StatusBlocked, Status("blocked"))
	require.Equal(t, StatusIncomplete, Status("incomplete"))
}

func TestBlockingReason_Constants(t *testing.T) {
	t.Parallel()

	// Verify reason string values are stable
	require.Equal(t, ReasonChangesRequested, BlockingReason("changes_requested"))
	require.Equal(t, ReasonCIFailing, BlockingReason("ci_failing"))
	require.Equal(t, ReasonCIPending, BlockingReason("ci_pending"))
	require.Equal(t, ReasonDraft, BlockingReason("draft"))
	require.Equal(t, ReasonNoPR, BlockingReason("no_pr"))
	require.Equal(t, ReasonReviewRequired, BlockingReason("review_required"))
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
			require.Equal(t, tt.want, s.IsShippable())
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
			require.Equal(t, tt.want, s.IsPending())
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
			require.Equal(t, tt.want, s.IsBlocked())
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
			require.Equal(t, tt.want, s.IsIncomplete())
		})
	}
}

func TestStack_BranchCount(t *testing.T) {
	t.Parallel()

	s := &Stack{
		Stack: stackview.StackInfo{
			AllBranches: []string{"branch1", "branch2", "branch3"},
		},
	}
	require.Equal(t, 3, s.BranchCount())
}

func TestStack_RootBranch(t *testing.T) {
	t.Parallel()

	s := &Stack{
		Stack: stackview.StackInfo{
			RootBranch: "feature/my-stack",
		},
	}
	require.Equal(t, "feature/my-stack", s.RootBranch())
}

func TestStack_DisplayTitle(t *testing.T) {
	t.Parallel()

	t.Run("returns PR title when available", func(t *testing.T) {
		t.Parallel()
		s := &Stack{
			Stack: stackview.StackInfo{
				RootBranch: "jonnii/20260131/feature",
			},
			PRTitle: "feat: add user authentication",
		}
		require.Equal(t, "feat: add user authentication", s.DisplayTitle())
	})

	t.Run("falls back to branch name when no PR title", func(t *testing.T) {
		t.Parallel()
		s := &Stack{
			Stack: stackview.StackInfo{
				RootBranch: "jonnii/20260131/feature",
			},
			PRTitle: "",
		}
		require.Equal(t, "jonnii/20260131/feature", s.DisplayTitle())
	})
}

func TestAnalysisResult_GetShippable(t *testing.T) {
	t.Parallel()

	result := &AnalysisResult{
		Stacks: []Stack{
			{Stack: stackview.StackInfo{RootBranch: "a"}, Status: StatusShippable},
			{Stack: stackview.StackInfo{RootBranch: "b"}, Status: StatusPending},
			{Stack: stackview.StackInfo{RootBranch: "c"}, Status: StatusShippable},
			{Stack: stackview.StackInfo{RootBranch: "d"}, Status: StatusBlocked},
		},
	}

	shippable := result.Shippable()
	require.Len(t, shippable, 2)
	require.Equal(t, "a", shippable[0].RootBranch())
	require.Equal(t, "c", shippable[1].RootBranch())
}

func TestAnalysisResult_GetPending(t *testing.T) {
	t.Parallel()

	result := &AnalysisResult{
		Stacks: []Stack{
			{Stack: stackview.StackInfo{RootBranch: "a"}, Status: StatusShippable},
			{Stack: stackview.StackInfo{RootBranch: "b"}, Status: StatusPending},
			{Stack: stackview.StackInfo{RootBranch: "c"}, Status: StatusPending},
		},
	}

	pending := result.Pending()
	require.Len(t, pending, 2)
	require.Equal(t, "b", pending[0].RootBranch())
	require.Equal(t, "c", pending[1].RootBranch())
}

func TestAnalysisResult_GetBlocked(t *testing.T) {
	t.Parallel()

	result := &AnalysisResult{
		Stacks: []Stack{
			{Stack: stackview.StackInfo{RootBranch: "a"}, Status: StatusBlocked},
			{Stack: stackview.StackInfo{RootBranch: "b"}, Status: StatusShippable},
		},
	}

	blocked := result.Blocked()
	require.Len(t, blocked, 1)
	require.Equal(t, "a", blocked[0].RootBranch())
}

func TestAnalysisResult_GetIncomplete(t *testing.T) {
	t.Parallel()

	result := &AnalysisResult{
		Stacks: []Stack{
			{Stack: stackview.StackInfo{RootBranch: "a"}, Status: StatusIncomplete},
			{Stack: stackview.StackInfo{RootBranch: "b"}, Status: StatusShippable},
		},
	}

	incomplete := result.Incomplete()
	require.Len(t, incomplete, 1)
	require.Equal(t, "a", incomplete[0].RootBranch())
}

func TestAnalysisResult_HasShippable(t *testing.T) {
	t.Parallel()

	t.Run("has shippable", func(t *testing.T) {
		t.Parallel()
		result := &AnalysisResult{ShippableCount: 2}
		require.True(t, result.HasShippable())
	})

	t.Run("no shippable", func(t *testing.T) {
		t.Parallel()
		result := &AnalysisResult{ShippableCount: 0}
		require.False(t, result.HasShippable())
	})
}

func TestAnalysisResult_TotalStacks(t *testing.T) {
	t.Parallel()

	result := &AnalysisResult{
		Stacks: []Stack{
			{Stack: stackview.StackInfo{RootBranch: "a"}},
			{Stack: stackview.StackInfo{RootBranch: "b"}},
			{Stack: stackview.StackInfo{RootBranch: "c"}},
		},
	}
	require.Equal(t, 3, result.TotalStacks())
}
