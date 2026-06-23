package github_test

import (
	"testing"
	"time"

	gogithub "github.com/google/go-github/v73/github"
	"github.com/stretchr/testify/require"

	githubpkg "github.com/getstackit/stackit/internal/github"
)

func TestToPullRequestInfoState(t *testing.T) {
	t.Parallel()

	mergedAt := gogithub.Timestamp{Time: time.Date(2026, 6, 12, 0, 0, 0, 0, time.UTC)}

	tests := []struct {
		name     string
		pr       *gogithub.PullRequest
		expected string
	}{
		{
			name:     "open PR",
			pr:       &gogithub.PullRequest{State: new("open")},
			expected: "OPEN",
		},
		{
			name: "closed PR that was not merged",
			pr: &gogithub.PullRequest{
				State:  new("closed"),
				Merged: new(false),
			},
			expected: "CLOSED",
		},
		{
			name: "closed PR with merged flag",
			pr: &gogithub.PullRequest{
				State:  new("closed"),
				Merged: new(true),
			},
			expected: "MERGED",
		},
		{
			name: "closed PR with only merged_at populated (list response)",
			pr: &gogithub.PullRequest{
				State:    new("closed"),
				MergedAt: &mergedAt,
			},
			expected: "MERGED",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			info := githubpkg.ToPullRequestInfo(tt.pr)
			require.NotNil(t, info)
			require.Equal(t, tt.expected, info.State)
		})
	}
}
