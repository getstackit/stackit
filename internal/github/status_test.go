package github_test

import (
	"testing"

	"github.com/getstackit/stackit/internal/github"
	"github.com/stretchr/testify/require"
)

func TestCheckStatus_IsApproved(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		reviewDecision github.ReviewDecision
		want           bool
	}{
		{"approved", github.ReviewDecisionApproved, true},
		{"changes requested", github.ReviewDecisionChangesRequested, false},
		{"review required", github.ReviewDecisionReviewRequired, false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			status := &github.CheckStatus{ReviewDecision: tt.reviewDecision}
			require.Equal(t, tt.want, status.IsApproved())
		})
	}
}

func TestCheckStatus_HasFailingChecks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		passing bool
		want    bool
	}{
		{"passing", true, false},
		{"failing", false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			status := &github.CheckStatus{Passing: tt.passing}
			require.Equal(t, tt.want, status.HasFailingChecks())
		})
	}
}

func TestCheckStatus_HasPendingChecks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pending bool
		want    bool
	}{
		{"pending", true, true},
		{"not pending", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			status := &github.CheckStatus{Pending: tt.pending}
			require.Equal(t, tt.want, status.HasPendingChecks())
		})
	}
}

func TestCheckStatus_IsReady(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		passing        bool
		pending        bool
		reviewDecision github.ReviewDecision
		want           bool
	}{
		{"fully ready", true, false, github.ReviewDecisionApproved, true},
		{"failing CI", false, false, github.ReviewDecisionApproved, false},
		{"pending CI", true, true, github.ReviewDecisionApproved, false},
		{"not approved", true, false, github.ReviewDecisionChangesRequested, false},
		{"no review", true, false, "", false},
		{"failing and pending", false, true, github.ReviewDecisionApproved, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			status := &github.CheckStatus{
				Passing:        tt.passing,
				Pending:        tt.pending,
				ReviewDecision: tt.reviewDecision,
			}
			require.Equal(t, tt.want, status.IsReady())
		})
	}
}

func TestReviewDecisionConstants(t *testing.T) {
	t.Parallel()

	// Verify constants match GitHub's actual values
	require.Equal(t, github.ReviewDecisionApproved, github.ReviewDecision("APPROVED"))
	require.Equal(t, github.ReviewDecisionChangesRequested, github.ReviewDecision("CHANGES_REQUESTED"))
	require.Equal(t, github.ReviewDecisionReviewRequired, github.ReviewDecision("REVIEW_REQUIRED"))
}
