package merge

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/github"
)

type mockGitHubClient struct {
	github.Client
	checkStatus *github.CheckStatus
}

func (m *mockGitHubClient) GetPRChecksStatus(_ context.Context, _ string) (*github.CheckStatus, error) {
	return m.checkStatus, nil
}

func TestWaitForChecks(t *testing.T) {
	t.Parallel()

	t.Run("returns immediately when no checks expected", func(t *testing.T) {
		t.Parallel()

		waiter := NewCIWaiter(CIWaiterOptions{
			Client:       &mockGitHubClient{},
			Timeout:      5 * time.Second,
			PollInterval: 100 * time.Millisecond,
		})

		start := time.Now()
		result, err := waiter.WaitForChecks(context.Background(), "branch", 1, false)
		elapsed := time.Since(start)

		require.NoError(t, err)
		require.NotNil(t, result)
		require.True(t, result.Passed)
		// Should return almost immediately, not wait for any polling
		require.Less(t, elapsed, 1*time.Second)
	})

	t.Run("returns when checks pass", func(t *testing.T) {
		t.Parallel()

		client := &mockGitHubClient{
			checkStatus: &github.CheckStatus{
				Passing: true,
				Pending: false,
				Checks: []github.CheckDetail{
					{Name: "ci", Status: "COMPLETED", Conclusion: "SUCCESS"},
				},
			},
		}

		waiter := NewCIWaiter(CIWaiterOptions{
			Client:       client,
			Timeout:      30 * time.Second, // Enough to cover CIRegistrationDelay + poll
			PollInterval: 100 * time.Millisecond,
		})

		result, err := waiter.WaitForChecks(context.Background(), "branch", 1, true)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.True(t, result.Passed)
	})

	t.Run("returns error when checks fail", func(t *testing.T) {
		t.Parallel()

		client := &mockGitHubClient{
			checkStatus: &github.CheckStatus{
				Passing: false,
				Pending: false,
				Checks: []github.CheckDetail{
					{Name: "ci", Status: "COMPLETED", Conclusion: "FAILURE"},
				},
			},
		}

		waiter := NewCIWaiter(CIWaiterOptions{
			Client:       client,
			Timeout:      30 * time.Second,
			PollInterval: 100 * time.Millisecond,
		})

		result, err := waiter.WaitForChecks(context.Background(), "branch", 1, true)
		require.Error(t, err)
		require.Contains(t, err.Error(), "CI checks failed")
		require.NotNil(t, result)
		require.False(t, result.Passed)
	})
}
