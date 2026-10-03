package branch

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/handlers"
	"github.com/getstackit/stackit/internal/output"
)

func TestSimpleGetHandlerReportsIncompleteOutcome(t *testing.T) {
	t.Parallel()
	held := []handlers.RestackHeldInfo{{Branch: "feat-ui", Reason: "worktree /tmp/ui has uncommitted changes"}}

	t.Run("hold makes get incomplete", func(t *testing.T) {
		t.Parallel()
		out := output.NewTestOutput()
		h := NewSimpleGetHandler(out)
		h.OnRestackComplete(handlers.RestackSummary{Held: held})
		h.Complete(actions.GetSummary{TargetBranch: "feat-ui", BranchesUpdated: 1, Held: held})

		got := out.String()
		require.Contains(t, got, "⚠ Restack incomplete: held 1 (worktree)")
		require.Contains(t, got, "⚠ Get incomplete: held 1 (worktree), updated 1")
		require.NotContains(t, got, "Everything is up to date")
		require.NotContains(t, got, "✅ Summary")
	})

	t.Run("conflict ends get with resolve advice", func(t *testing.T) {
		t.Parallel()
		out := output.NewTestOutput()
		NewSimpleGetHandler(out).OnRestackComplete(handlers.RestackSummary{Restacked: 1, Skipped: 1, Conflicts: []string{"feat-ui"}})

		got := out.String()
		require.Contains(t, got, "⚠ Restack incomplete: restacked 1, skipped 1 (conflict)")
		require.Contains(t, got, "st restack --branch feat-ui")
	})

	t.Run("clean restack keeps the compact line", func(t *testing.T) {
		t.Parallel()
		out := output.NewTestOutput()
		h := NewSimpleGetHandler(out)
		h.OnRestackComplete(handlers.RestackSummary{Restacked: 2})
		h.Complete(actions.GetSummary{TargetBranch: "feat-ui", Restacked: 2})

		got := out.String()
		require.Contains(t, got, "restacked 2")
		require.Contains(t, got, "✅ Summary: restacked 2")
		require.NotContains(t, got, "incomplete")
	})
}
