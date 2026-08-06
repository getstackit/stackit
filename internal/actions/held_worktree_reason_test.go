package actions

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

// TestHeldWorktreeReason covers the lookup behind the conflict-workflow
// diagnosis.
//
// Restack holds a branch whose worktree it cannot safely reset, and reports the
// hold as RestackUnneeded — indistinguishable from "already up to date". When a
// caller expected a rebase to start, that produced "expected conflict on X but
// rebase completed successfully", which describes the opposite of what happened
// and points nowhere near the fix (the other worktree).
func TestHeldWorktreeReason(t *testing.T) {
	t.Parallel()

	setup := func(t *testing.T) (*scenario.Scenario, string) {
		t.Helper()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()
		s.CreateBranch("feature").Commit("feature change")
		s.TrackBranch("feature", "main")
		s.Checkout("main")

		// A real second checkout of feature, so ListWorktrees reports it.
		worktreePath := filepath.Join(t.TempDir(), "feature-checkout")
		s.RunGit("worktree", "add", worktreePath, "feature")
		t.Cleanup(func() { s.RunGit("worktree", "remove", "--force", worktreePath) })
		return s, worktreePath
	}

	t.Run("clean checkout holds nothing", func(t *testing.T) {
		t.Parallel()
		s, _ := setup(t)
		require.Empty(t, heldWorktreeReason(s.Context, "feature"))
	})

	t.Run("uncommitted changes are reported", func(t *testing.T) {
		t.Parallel()
		s, worktreePath := setup(t)
		require.NoError(t, os.WriteFile(filepath.Join(worktreePath, "scratch.txt"), []byte("wip"), 0o600))

		reason := heldWorktreeReason(s.Context, "feature")
		require.Contains(t, reason, "uncommitted changes")
		require.Contains(t, reason, worktreePath,
			"the reason must name the worktree to act on, not just the branch")
	})

	t.Run("branch with no checkout holds nothing", func(t *testing.T) {
		t.Parallel()
		s, _ := setup(t)
		require.Empty(t, heldWorktreeReason(s.Context, "main"))
		require.Empty(t, heldWorktreeReason(s.Context, "does-not-exist"))
	})
}
