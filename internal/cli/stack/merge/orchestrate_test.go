package merge

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/github"
	"github.com/getstackit/stackit/internal/output"
)

// mockPRMergeAPI is a test double for prMergeAPI that records calls and returns configured responses.
type mockPRMergeAPI struct {
	mergeableState    *github.PRMergeableState
	mergeableStateErr error

	enableAutoMergeErr error

	waitForPRMergeErr error

	waitForMergeableState *github.PRMergeableState
	waitForMergeableErr   error

	mergePRErr error

	// Track calls
	mergePRCalled          bool
	enableAutoMergeCalled  bool
	waitForPRMergeCalled   bool
	waitForMergeableCalled bool
}

func (m *mockPRMergeAPI) getMergeableState(_ context.Context, _ git.Runner, _ string) (*github.PRMergeableState, error) {
	return m.mergeableState, m.mergeableStateErr
}

func (m *mockPRMergeAPI) enableAutoMerge(_ context.Context, _ git.Runner, _ string, _ github.MergeMethod) error {
	m.enableAutoMergeCalled = true
	return m.enableAutoMergeErr
}

func (m *mockPRMergeAPI) waitForPRMerge(_ context.Context, _ git.Runner, _ string, _, _ time.Duration) error {
	m.waitForPRMergeCalled = true
	return m.waitForPRMergeErr
}

func (m *mockPRMergeAPI) waitForMergeable(_ context.Context, _ git.Runner, _ string, _, _ time.Duration) (*github.PRMergeableState, error) {
	m.waitForMergeableCalled = true
	return m.waitForMergeableState, m.waitForMergeableErr
}

func (m *mockPRMergeAPI) mergePR(_ context.Context, _ string, _ github.MergeMethod) error {
	m.mergePRCalled = true
	return m.mergePRErr
}

func TestDoOrchestrateMerge(t *testing.T) {
	t.Parallel()

	baseOpts := orchestrateMergeOptions{
		branchName:  "feature-branch",
		prNumber:    42,
		prNodeID:    "PR_node123",
		mergeMethod: github.MergeMethodSquash,
		wait:        false,
	}

	t.Run("already merged PR returns success", func(t *testing.T) {
		t.Parallel()
		api := &mockPRMergeAPI{
			mergeableState: &github.PRMergeableState{State: "MERGED"},
		}
		out := output.NewTestOutput()

		outcome, err := doOrchestrateMerge(context.Background(), out, nil, api, baseOpts)

		require.NoError(t, err)
		require.Equal(t, OutcomeMerged, outcome)
		require.Contains(t, out.String(), "already merged")
		require.False(t, api.mergePRCalled)
	})

	t.Run("closed PR returns error", func(t *testing.T) {
		t.Parallel()
		api := &mockPRMergeAPI{
			mergeableState: &github.PRMergeableState{State: "CLOSED"},
		}
		out := output.NewTestOutput()

		_, err := doOrchestrateMerge(context.Background(), out, nil, api, baseOpts)

		require.Error(t, err)
		require.Contains(t, err.Error(), "CLOSED")
		require.Contains(t, err.Error(), "not open")
	})

	t.Run("UNKNOWN mergeability after retries returns retry error", func(t *testing.T) {
		t.Parallel()
		api := &mockPRMergeAPI{
			mergeableState: &github.PRMergeableState{
				State:          "OPEN",
				Mergeable:      false,
				MergeStateText: "UNKNOWN",
			},
		}
		out := output.NewTestOutput()

		_, err := doOrchestrateMerge(context.Background(), out, nil, api, baseOpts)

		require.Error(t, err)
		require.Contains(t, err.Error(), "still being calculated")
	})

	t.Run("not mergeable returns formatted error", func(t *testing.T) {
		t.Parallel()
		api := &mockPRMergeAPI{
			mergeableState: &github.PRMergeableState{
				State:          "OPEN",
				Mergeable:      false,
				MergeStateText: "DIRTY",
			},
		}
		out := output.NewTestOutput()

		_, err := doOrchestrateMerge(context.Background(), out, nil, api, baseOpts)

		require.Error(t, err)
		require.Contains(t, err.Error(), "not mergeable")
		require.Contains(t, err.Error(), "DIRTY")
	})

	t.Run("CLEAN PR merges directly", func(t *testing.T) {
		t.Parallel()
		api := &mockPRMergeAPI{
			mergeableState: &github.PRMergeableState{
				State:          "OPEN",
				Mergeable:      true,
				MergeStateText: "CLEAN",
			},
		}
		out := output.NewTestOutput()

		outcome, err := doOrchestrateMerge(context.Background(), out, nil, api, baseOpts)

		require.NoError(t, err)
		require.Equal(t, OutcomeMerged, outcome)
		require.True(t, api.mergePRCalled)
		require.False(t, api.enableAutoMergeCalled)
		require.Contains(t, out.String(), "merged successfully")
	})

	t.Run("HAS_HOOKS PR merges directly", func(t *testing.T) {
		t.Parallel()
		api := &mockPRMergeAPI{
			mergeableState: &github.PRMergeableState{
				State:          "OPEN",
				Mergeable:      true,
				MergeStateText: "HAS_HOOKS",
			},
		}
		out := output.NewTestOutput()

		outcome, err := doOrchestrateMerge(context.Background(), out, nil, api, baseOpts)

		require.NoError(t, err)
		require.Equal(t, OutcomeMerged, outcome)
		require.True(t, api.mergePRCalled)
	})

	t.Run("automerge enabled then wait", func(t *testing.T) {
		t.Parallel()
		api := &mockPRMergeAPI{
			mergeableState: &github.PRMergeableState{
				State:          "OPEN",
				Mergeable:      true,
				MergeStateText: "BLOCKED",
			},
		}
		out := output.NewTestOutput()
		opts := baseOpts
		opts.wait = true

		outcome, err := doOrchestrateMerge(context.Background(), out, nil, api, opts)

		require.NoError(t, err)
		require.Equal(t, OutcomeMerged, outcome)
		require.True(t, api.enableAutoMergeCalled)
		require.True(t, api.waitForPRMergeCalled)
		require.Contains(t, out.String(), "Automerge enabled")
	})

	t.Run("automerge enabled fire-and-forget", func(t *testing.T) {
		t.Parallel()
		api := &mockPRMergeAPI{
			mergeableState: &github.PRMergeableState{
				State:          "OPEN",
				Mergeable:      true,
				MergeStateText: "BLOCKED",
			},
		}
		out := output.NewTestOutput()

		outcome, err := doOrchestrateMerge(context.Background(), out, nil, api, baseOpts)

		require.NoError(t, err)
		require.Equal(t, OutcomeAutomergeEnabled, outcome)
		require.True(t, api.enableAutoMergeCalled)
		require.False(t, api.waitForPRMergeCalled)
	})

	t.Run("automerge clean status race condition merges directly", func(t *testing.T) {
		t.Parallel()
		api := &mockPRMergeAPI{
			mergeableState: &github.PRMergeableState{
				State:          "OPEN",
				Mergeable:      true,
				MergeStateText: "BLOCKED",
			},
			enableAutoMergeErr: github.ErrPRCleanStatus,
		}
		out := output.NewTestOutput()

		outcome, err := doOrchestrateMerge(context.Background(), out, nil, api, baseOpts)

		require.NoError(t, err)
		require.Equal(t, OutcomeMerged, outcome)
		require.True(t, api.mergePRCalled)
		require.Contains(t, out.String(), "merged successfully")
	})

	t.Run("automerge not enabled with wait polls then merges", func(t *testing.T) {
		t.Parallel()
		api := &mockPRMergeAPI{
			mergeableState: &github.PRMergeableState{
				State:          "OPEN",
				Mergeable:      true,
				MergeStateText: "BLOCKED",
			},
			enableAutoMergeErr: github.ErrAutoMergeNotEnabled,
			waitForMergeableState: &github.PRMergeableState{
				State:          "OPEN",
				Mergeable:      true,
				MergeStateText: "CLEAN",
			},
		}
		out := output.NewTestOutput()
		opts := baseOpts
		opts.wait = true

		outcome, err := doOrchestrateMerge(context.Background(), out, nil, api, opts)

		require.NoError(t, err)
		require.Equal(t, OutcomeMerged, outcome)
		require.True(t, api.waitForMergeableCalled)
		require.True(t, api.mergePRCalled)
		require.Contains(t, out.String(), "merged successfully")
	})

	t.Run("automerge not enabled without wait returns error", func(t *testing.T) {
		t.Parallel()
		api := &mockPRMergeAPI{
			mergeableState: &github.PRMergeableState{
				State:          "OPEN",
				Mergeable:      true,
				MergeStateText: "BLOCKED",
			},
			enableAutoMergeErr: github.ErrAutoMergeNotEnabled,
		}
		out := output.NewTestOutput()

		_, err := doOrchestrateMerge(context.Background(), out, nil, api, baseOpts)

		require.Error(t, err)
		require.Contains(t, err.Error(), "auto-merge is not enabled")
		require.Contains(t, err.Error(), "--wait")
		require.False(t, api.mergePRCalled)
	})

	t.Run("automerge not enabled PR merged externally", func(t *testing.T) {
		t.Parallel()
		api := &mockPRMergeAPI{
			mergeableState: &github.PRMergeableState{
				State:          "OPEN",
				Mergeable:      true,
				MergeStateText: "BLOCKED",
			},
			enableAutoMergeErr:  github.ErrAutoMergeNotEnabled,
			waitForMergeableErr: github.ErrPRAlreadyMerged,
		}
		out := output.NewTestOutput()
		opts := baseOpts
		opts.wait = true

		outcome, err := doOrchestrateMerge(context.Background(), out, nil, api, opts)

		require.NoError(t, err)
		require.Equal(t, OutcomeMerged, outcome)
		require.Contains(t, out.String(), "merged externally")
		require.False(t, api.mergePRCalled)
	})

	t.Run("other automerge error passes through", func(t *testing.T) {
		t.Parallel()
		api := &mockPRMergeAPI{
			mergeableState: &github.PRMergeableState{
				State:          "OPEN",
				Mergeable:      true,
				MergeStateText: "BLOCKED",
			},
			enableAutoMergeErr: fmt.Errorf("unexpected GraphQL error"),
		}
		out := output.NewTestOutput()

		_, err := doOrchestrateMerge(context.Background(), out, nil, api, baseOpts)

		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to enable automerge")
		require.Contains(t, err.Error(), "unexpected GraphQL error")
	})

	t.Run("direct merge failure returns error", func(t *testing.T) {
		t.Parallel()
		api := &mockPRMergeAPI{
			mergeableState: &github.PRMergeableState{
				State:          "OPEN",
				Mergeable:      true,
				MergeStateText: "CLEAN",
			},
			mergePRErr: fmt.Errorf("merge conflict detected"),
		}
		out := output.NewTestOutput()

		_, err := doOrchestrateMerge(context.Background(), out, nil, api, baseOpts)

		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to merge PR #42")
		require.Contains(t, err.Error(), "merge conflict detected")
	})

	t.Run("getMergeableState error returns wrapped error", func(t *testing.T) {
		t.Parallel()
		api := &mockPRMergeAPI{
			mergeableStateErr: fmt.Errorf("network timeout"),
		}
		out := output.NewTestOutput()

		_, err := doOrchestrateMerge(context.Background(), out, nil, api, baseOpts)

		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to check PR mergeable state")
		require.Contains(t, err.Error(), "network timeout")
	})
}

func TestIsReadyToMerge(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		mergeStateText string
		expected       bool
	}{
		{name: "CLEAN is ready", mergeStateText: "CLEAN", expected: true},
		{name: "HAS_HOOKS is ready", mergeStateText: "HAS_HOOKS", expected: true},
		{name: "BLOCKED is not ready", mergeStateText: "BLOCKED", expected: false},
		{name: "BEHIND is not ready", mergeStateText: "BEHIND", expected: false},
		{name: "DIRTY is not ready", mergeStateText: "DIRTY", expected: false},
		{name: "UNKNOWN is not ready", mergeStateText: "UNKNOWN", expected: false},
		{name: "empty is not ready", mergeStateText: "", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.expected, isReadyToMerge(tt.mergeStateText))
		})
	}
}

func TestFormatUnmergeableError(t *testing.T) {
	t.Parallel()

	t.Run("includes merge state text when present", func(t *testing.T) {
		t.Parallel()
		err := formatUnmergeableError(42, &github.PRMergeableState{
			MergeStateText: "DIRTY",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "PR #42")
		require.Contains(t, err.Error(), "DIRTY")
		require.Contains(t, err.Error(), "not mergeable")
	})

	t.Run("generic message when merge state text is empty", func(t *testing.T) {
		t.Parallel()
		err := formatUnmergeableError(99, &github.PRMergeableState{
			MergeStateText: "",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "PR #99")
		require.Contains(t, err.Error(), "not mergeable")
		require.NotContains(t, err.Error(), "()")
	})
}
