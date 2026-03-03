package handlers

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/internal/engine"
)

func TestJSONRestackHandler(t *testing.T) {
	t.Parallel()

	t.Run("tracks restacked branches", func(t *testing.T) {
		t.Parallel()

		handler := NewJSONRestackHandler()
		handler.OnRestackStart(3)

		// Simulate branch restacking
		prNum := 123
		handler.OnRestackBranch("branch-a", RestackDone, "abc123", &prNum, engine.LockReasonNone, false, false, "main", false, "", "")
		handler.OnRestackBranch("branch-b", RestackDone, "def456", nil, engine.LockReasonNone, false, false, "branch-a", false, "", "")

		handler.OnRestackComplete(2, 0, nil)

		require.Equal(t, RestackJSONStatusSuccess, handler.Result.Status)
		require.Len(t, handler.Result.Restacked, 2)
		require.Equal(t, "branch-a", handler.Result.Restacked[0].Name)
		require.Equal(t, "main", handler.Result.Restacked[0].Parent)
		require.Equal(t, "abc123", handler.Result.Restacked[0].NewRev)
		require.Equal(t, 123, *handler.Result.Restacked[0].PRNumber)
		require.Equal(t, 3, handler.Result.TotalCount)
		require.Equal(t, 2, handler.Result.RestackCount)
	})

	t.Run("tracks skipped branches", func(t *testing.T) {
		t.Parallel()

		handler := NewJSONRestackHandler()
		handler.OnRestackStart(2)

		// Simulate branches that don't need restacking
		handler.OnRestackBranch("branch-a", RestackUnneeded, "", nil, engine.LockReasonNone, false, false, "main", false, "", "")
		handler.OnRestackBranch("branch-b", RestackUnneeded, "", nil, engine.LockReasonNone, false, false, "branch-a", false, "", "")

		handler.OnRestackComplete(0, 2, nil)

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
		handler.OnRestackBranch("branch-a", RestackDone, "abc123", nil, engine.LockReasonNone, false, false, "main", false, "", "")
		handler.OnRestackBranch("branch-b", RestackConflict, "", nil, engine.LockReasonNone, false, false, "branch-a", false, "", "")

		handler.OnRestackComplete(1, 0, []string{"branch-b"})

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
		handler.OnRestackBranch("branch-a", RestackConflict, "", nil, engine.LockReasonNone, false, false, "main", false, "", "")
		handler.OnRestackComplete(0, 0, []string{"branch-a"})

		handler.SetError(errors.New("restack stopped due to conflict on branch-a"))

		require.Equal(t, RestackJSONStatusConflict, handler.Result.Status)
		require.Equal(t, "restack stopped due to conflict on branch-a", handler.Result.Error)
		require.Len(t, handler.Result.Conflicts, 1)
	})

	t.Run("SetError does nothing for nil error", func(t *testing.T) {
		t.Parallel()

		handler := NewJSONRestackHandler()
		handler.OnRestackStart(1)
		handler.OnRestackComplete(0, 1, nil)

		// Call SetError with nil
		handler.SetError(nil)

		// Status should remain success
		require.Equal(t, RestackJSONStatusSuccess, handler.Result.Status)
		require.Empty(t, handler.Result.Error)
	})
}
