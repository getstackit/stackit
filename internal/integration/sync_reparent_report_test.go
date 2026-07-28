package integration

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions/sync"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

type reparentRecordingHandler struct {
	sync.NullHandler
	events  []sync.Event
	summary sync.Summary
}

func (h *reparentRecordingHandler) EmitEvent(event sync.Event) {
	h.events = append(h.events, event)
}

func (h *reparentRecordingHandler) Complete(summary sync.Summary) {
	h.summary = summary
}

// TestSyncReportsReparentPastMergedParent covers the common reparent path:
// cleanup deletes a landed parent and moves its child before restack runs, so
// restack sees nothing to reparent. The reparent must still reach the event
// stream and the summary, for every GitHub merge method.
func TestSyncReportsReparentPastMergedParent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// land puts b's changes onto a the way GitHub's merge method would.
		land func(t *testing.T, sh *scenario.Scenario)
	}{
		{
			name: "merge commit",
			land: func(t *testing.T, sh *scenario.Scenario) {
				sh.Checkout("a")
				require.NoError(t, sh.Scene.Repo.RunGitCommand("merge", "--no-ff", "-m", "Merge b", "b"))
			},
		},
		{
			name: "squash multi-commit",
			land: func(t *testing.T, sh *scenario.Scenario) {
				// The squash commit carries both of b's files as one change.
				require.NoError(t, sh.Scene.Repo.RunGitCommand("checkout", "a"))
				require.NoError(t, sh.Scene.Repo.RunGitCommand("merge", "--squash", "b"))
				require.NoError(t, sh.Scene.Repo.RunGitCommand("commit", "-m", "squash of b"))
			},
		},
		{
			name: "rebase",
			land: func(t *testing.T, sh *scenario.Scenario) {
				sh.Checkout("a")
				require.NoError(t, sh.Scene.Repo.RunGitCommand("cherry-pick", "a..b"))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sh := scenario.NewRemoteScenario(t)
			disableCommitSigning(t, sh)

			// main -> a -> b (two commits) -> c, with b landing on a.
			sh.CreateBranch("a").CommitChange("a.txt", "a").TrackBranch("a", "main")
			sh.CreateBranch("b").CommitChange("b.txt", "b1").CommitChange("b2.txt", "b2").TrackBranch("b", "a")
			sh.CreateBranch("c").CommitChange("c.txt", "c").TrackBranch("c", "b")
			tt.land(t, sh)
			markPrMerged(t, sh, "b", 2, "a")

			sh.Checkout("main")
			handler := &reparentRecordingHandler{}
			require.NoError(t, sync.Action(sh.Context, sync.Options{Restack: true}, handler))

			require.Equal(t, "a", sh.Engine.GetBranch("c").GetParent().GetName())
			var reparents []sync.Event
			for _, event := range handler.events {
				if event.Reparented {
					reparents = append(reparents, event)
				}
			}
			require.Len(t, reparents, 1, "exactly one reparent event, not one per phase")
			require.Equal(t, "c", reparents[0].Branch)
			require.Equal(t, "b", reparents[0].OldParent)
			require.Equal(t, "a", reparents[0].NewParent)
			require.Equal(t, 1, handler.summary.BranchesReparented)
			require.Contains(t, sync.FormatSummaryParts(handler.summary), "reparented 1")
		})
	}
}
