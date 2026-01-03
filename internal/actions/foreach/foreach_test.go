package foreach_test

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/internal/actions/foreach"
	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/testhelpers"
	"stackit.dev/stackit/testhelpers/scenario"
)

type testHandler struct {
	mu     sync.Mutex
	events []foreach.Event
}

func (h *testHandler) OnEvent(event foreach.Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = append(h.events, event)
}

func (h *testHandler) Events() []foreach.Event {
	h.mu.Lock()
	defer h.mu.Unlock()
	cp := make([]foreach.Event, len(h.events))
	copy(cp, h.events)
	return cp
}

func TestForeachAction(t *testing.T) {
	t.Parallel()
	t.Run("sequential execution on stack", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
				"branch3": "branch2",
			})

		s.Checkout("branch1")

		handler := &testHandler{}
		opts := foreach.Options{
			Command:  "echo",
			Args:     []string{"hi"},
			Scope:    engine.StackRange{IncludeCurrent: true, RecursiveChildren: true},
			FailFast: true,
		}

		err := foreach.Action(s.Context, opts, handler)
		require.NoError(t, err)

		events := handler.Events()
		require.NotEmpty(t, events)

		// Verify branches executed in order (upstack from branch1: branch1, branch2, branch3)
		var executedBranches []string
		for _, e := range events {
			if bpe, ok := e.(foreach.BranchProgressEvent); ok && bpe.Status == foreach.StatusDone {
				executedBranches = append(executedBranches, bpe.BranchName)
			}
		}
		require.Equal(t, []string{"branch1", "branch2", "branch3"}, executedBranches)
	})

	t.Run("fail-fast sequential", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
			})

		s.Checkout("branch1")

		handler := &testHandler{}
		opts := foreach.Options{
			Command:  "false",
			Scope:    engine.StackRange{IncludeCurrent: true, RecursiveChildren: true},
			FailFast: true,
		}

		err := foreach.Action(s.Context, opts, handler)
		require.Error(t, err)

		events := handler.Events()
		var failedBranches []string
		var doneBranches []string
		for _, e := range events {
			if bpe, ok := e.(foreach.BranchProgressEvent); ok {
				if bpe.Status == foreach.StatusError {
					failedBranches = append(failedBranches, bpe.BranchName)
				} else if bpe.Status == foreach.StatusDone {
					doneBranches = append(doneBranches, bpe.BranchName)
				}
			}
		}
		require.Equal(t, []string{"branch1"}, failedBranches)
		require.Empty(t, doneBranches)
	})

	t.Run("no-fail-fast sequential", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
			})

		s.Checkout("branch1")

		handler := &testHandler{}
		opts := foreach.Options{
			Command:  "false",
			Scope:    engine.StackRange{IncludeCurrent: true, RecursiveChildren: true},
			FailFast: false,
		}

		err := foreach.Action(s.Context, opts, handler)
		require.NoError(t, err) // Should not return error if FailFast is false

		events := handler.Events()
		var failedBranches []string
		for _, e := range events {
			if bpe, ok := e.(foreach.BranchProgressEvent); ok && bpe.Status == foreach.StatusError {
				failedBranches = append(failedBranches, bpe.BranchName)
			}
		}
		require.ElementsMatch(t, []string{"branch1", "branch2"}, failedBranches)
	})

	t.Run("parallel execution", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
				"branch3": "branch2",
			})

		s.Checkout("branch1")

		handler := &testHandler{}
		opts := foreach.Options{
			Command:  "echo",
			Args:     []string{"parallel"},
			Scope:    engine.StackRange{IncludeCurrent: true, RecursiveChildren: true},
			Parallel: true,
			Jobs:     2,
		}

		err := foreach.Action(s.Context, opts, handler)
		require.NoError(t, err)

		events := handler.Events()
		var executedBranches []string
		for _, e := range events {
			if bpe, ok := e.(foreach.BranchProgressEvent); ok && bpe.Status == foreach.StatusDone {
				executedBranches = append(executedBranches, bpe.BranchName)
			}
		}
		require.ElementsMatch(t, []string{"branch1", "branch2", "branch3"}, executedBranches)
	})

	t.Run("environment variable STACKIT_BRANCH", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"feature": "main",
			})

		s.Checkout("feature")

		handler := &testHandler{}
		opts := foreach.Options{
			Command: "echo $STACKIT_BRANCH",
			Scope:   engine.StackRange{IncludeCurrent: true},
		}

		err := foreach.Action(s.Context, opts, handler)
		require.NoError(t, err)

		events := handler.Events()
		found := false
		for _, e := range events {
			if bpe, ok := e.(foreach.BranchProgressEvent); ok && bpe.Status == foreach.StatusDone {
				require.Contains(t, bpe.Output, "feature")
				found = true
			}
		}
		require.True(t, found)
	})

	t.Run("various scopes", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"b1": "main",
				"b2": "b1",
				"b3": "b2",
			})

		s.Checkout("b2")

		testCases := []struct {
			name     string
			scope    engine.StackRange
			expected []string
		}{
			{"current", engine.StackRange{IncludeCurrent: true}, []string{"b2"}},
			{"upstack", engine.StackRange{IncludeCurrent: true, RecursiveChildren: true}, []string{"b2", "b3"}},
			{"downstack", engine.StackRange{IncludeCurrent: true, RecursiveParents: true}, []string{"b1", "b2"}},
			{"full stack", engine.StackRange{IncludeCurrent: true, RecursiveParents: true, RecursiveChildren: true}, []string{"b1", "b2", "b3"}},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				handler := &testHandler{}
				opts := foreach.Options{
					Command: "true",
					Scope:   tc.scope,
				}
				err := foreach.Action(s.Context, opts, handler)
				require.NoError(t, err)

				var executed []string
				for _, e := range handler.Events() {
					if bpe, ok := e.(foreach.BranchProgressEvent); ok && bpe.Status == foreach.StatusDone {
						executed = append(executed, bpe.BranchName)
					}
				}
				require.Equal(t, tc.expected, executed)
			})
		}
	})
}
