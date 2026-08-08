package handlers

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/engine"
)

func TestJSONRestackHandler(t *testing.T) {
	t.Parallel()

	t.Run("tracks restacked branches", func(t *testing.T) {
		t.Parallel()

		handler := NewJSONRestackHandler()
		handler.OnRestackStart(3)

		// Simulate branch restacking
		prNum := 123
		branchA := restackEvent("branch-a", RestackDone, "abc123", &prNum, "main")
		branchA.RerereResolvedCount = 2
		handler.OnRestackBranch(branchA)
		handler.OnRestackBranch(restackEvent("branch-b", RestackDone, "def456", nil, "branch-a"))

		handler.OnRestackComplete(RestackSummary{Restacked: 2})

		require.Equal(t, RestackJSONStatusSuccess, handler.Result.Status)
		require.Len(t, handler.Result.Restacked, 2)
		require.Equal(t, "branch-a", handler.Result.Restacked[0].Name)
		require.Equal(t, "main", handler.Result.Restacked[0].Parent)
		require.Equal(t, "abc123", handler.Result.Restacked[0].NewRev)
		require.Equal(t, 123, *handler.Result.Restacked[0].PRNumber)
		require.Equal(t, 2, handler.Result.Restacked[0].RerereResolvedCount)
		require.Equal(t, 3, handler.Result.TotalCount)
		require.Equal(t, 2, handler.Result.RestackCount)
	})

	t.Run("tracks skipped branches", func(t *testing.T) {
		t.Parallel()

		handler := NewJSONRestackHandler()
		handler.OnRestackStart(2)

		// Simulate branches that don't need restacking
		handler.OnRestackBranch(restackEvent("branch-a", RestackUnneeded, "", nil, "main"))
		handler.OnRestackBranch(restackEvent("branch-b", RestackUnneeded, "", nil, "branch-a"))

		handler.OnRestackComplete(RestackSummary{Skipped: 2})

		require.Equal(t, RestackJSONStatusSuccess, handler.Result.Status)
		require.Empty(t, handler.Result.Restacked)
		require.Len(t, handler.Result.Skipped, 2)
		require.Contains(t, handler.Result.Skipped, "branch-a")
		require.Contains(t, handler.Result.Skipped, "branch-b")
	})

	t.Run("tracks conflicts", func(t *testing.T) {
		t.Parallel()

		handler := NewJSONRestackHandler()
		handler.OnRestackStart(2)

		// Simulate a conflict
		handler.OnRestackBranch(restackEvent("branch-a", RestackDone, "abc123", nil, "main"))
		handler.OnRestackBranch(restackEvent("branch-b", RestackConflict, "", nil, "branch-a"))

		handler.OnRestackComplete(RestackSummary{Restacked: 1, Conflicts: []string{"branch-b"}})

		require.Equal(t, RestackJSONStatusConflict, handler.Result.Status)
		require.Len(t, handler.Result.Restacked, 1)
		require.Len(t, handler.Result.Conflicts, 1)
		require.Equal(t, "branch-b", handler.Result.Conflicts[0].Branch)
		require.Equal(t, "branch-a", handler.Result.Conflicts[0].Parent)
		require.Equal(t, 1, handler.Result.ConflictCount)
	})

	t.Run("SetError sets error status", func(t *testing.T) {
		t.Parallel()

		handler := NewJSONRestackHandler()
		handler.OnRestackStart(1)

		// Simulate an error
		handler.SetError(errors.New("something went wrong"))

		require.Equal(t, RestackJSONStatusError, handler.Result.Status)
		require.Equal(t, "something went wrong", handler.Result.Error)
	})

	t.Run("SetError preserves conflict status when conflicts exist", func(t *testing.T) {
		t.Parallel()

		handler := NewJSONRestackHandler()
		handler.OnRestackStart(1)
		handler.OnRestackBranch(restackEvent("branch-a", RestackConflict, "", nil, "main"))
		handler.OnRestackComplete(RestackSummary{Conflicts: []string{"branch-a"}})

		handler.SetError(errors.New("restack stopped due to conflict on branch-a"))

		require.Equal(t, RestackJSONStatusConflict, handler.Result.Status)
		require.Equal(t, "restack stopped due to conflict on branch-a", handler.Result.Error)
		require.Len(t, handler.Result.Conflicts, 1)
	})

	t.Run("branch events annotate branches and dedupe roots", func(t *testing.T) {
		t.Parallel()

		handler := NewJSONRestackHandler()
		handler.OnRestackStart(3)

		alpha := restackEvent("alpha", RestackDone, "aaa", nil, "main")
		alpha.StackRoot = "alpha"
		handler.OnRestackBranch(alpha)

		alphaChild := restackEvent("alpha-child", RestackDone, "bbb", nil, "alpha")
		alphaChild.StackRoot = "alpha"
		handler.OnRestackBranch(alphaChild)

		beta := restackEvent("beta", RestackConflict, "", nil, "main")
		beta.StackRoot = "beta"
		handler.OnRestackBranch(beta)

		handler.OnRestackComplete(RestackSummary{Restacked: 2, Conflicts: []string{"beta"}})

		// Per-branch annotation
		require.Equal(t, "alpha", handler.Result.Restacked[0].StackRoot)
		require.Equal(t, "alpha", handler.Result.Restacked[1].StackRoot)
		require.Equal(t, "beta", handler.Result.Conflicts[0].StackRoot)

		// Deduped top-level roots
		require.Equal(t, []string{"alpha", "beta"}, handler.Result.StackRoots)
	})

	t.Run("SetError does nothing for nil error", func(t *testing.T) {
		t.Parallel()

		handler := NewJSONRestackHandler()
		handler.OnRestackStart(1)
		handler.OnRestackComplete(RestackSummary{Skipped: 1})

		// Call SetError with nil
		handler.SetError(nil)

		// Status should remain success
		require.Equal(t, RestackJSONStatusSuccess, handler.Result.Status)
		require.Empty(t, handler.Result.Error)
	})
}

func restackEvent(branch string, result RestackResult, revision string, prNumber *int, parent string) RestackBranchEvent {
	return RestackBranchEvent{
		Branch:      branch,
		Result:      result,
		NewRevision: revision,
		PRNumber:    prNumber,
		LockReason:  engine.LockReasonNone,
		Parent:      parent,
	}
}
