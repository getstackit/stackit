package integration

import (
	"testing"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/testhelpers"
	"stackit.dev/stackit/testhelpers/scenario"
)

func TestSyncUIReporting(t *testing.T) {
	t.Run("reports locked and frozen branches during sync", func(t *testing.T) {
		sh := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithInProcess(true)

		// Create a stack: main -> feature-a -> feature-b -> feature-c
		sh.WithInitialCommit().
			CreateBranch("feature-a").Commit("a1").TrackBranch("feature-a", "main").
			CreateBranch("feature-b").Commit("b1").TrackBranch("feature-b", "feature-a").
			CreateBranch("feature-c").Commit("c1").TrackBranch("feature-c", "feature-b")

		// Lock feature-b
		branch := sh.Engine.GetBranch("feature-b")
		_, err := sh.Engine.SetLocked([]engine.Branch{branch}, engine.LockReasonUser)
		require.NoError(t, err)

		// Freeze feature-c
		_, err = sh.Engine.SetFrozen([]engine.Branch{sh.Engine.GetBranch("feature-c")}, true)
		require.NoError(t, err)

		// Make feature-a need restacking by amending its parent (main)
		sh.Checkout("main").
			CommitChange("main-file", "updated main")

		// Rebuild engine to reflect changes
		sh.Rebuild()

		// Run st sync --restack
		output, err := sh.RunCliAndGetOutput("sync", "--restack")
		require.NoError(t, err)

		// Assert output contains specific locked/frozen messages
		require.Contains(t, output, "feature-b is locked")
		require.Contains(t, output, "feature-c is frozen")

		// Assert feature-a is restacked (or at least attempted)
		require.Contains(t, output, "Restacked feature-a")
	})
}
