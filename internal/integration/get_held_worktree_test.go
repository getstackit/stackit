package integration

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/actions/submit"
	syncaction "github.com/getstackit/stackit/internal/actions/sync"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

// skipRecordingGetHandler captures the branches get reported as skipped, with
// the message explaining why. EmitEvent is only called from get's sequential
// sync loop, so no locking is needed.
type skipRecordingGetHandler struct {
	actions.GetNullHandler
	skipped map[string]string
}

func (h *skipRecordingGetHandler) EmitEvent(e actions.GetEvent) {
	if e.Type != actions.GetEventSkipped {
		return
	}
	if h.skipped == nil {
		h.skipped = map[string]string{}
	}
	h.skipped[e.Branch] = e.Message
}

// Updating an existing branch brings it to HEAD, and git refuses to check out a
// branch another worktree holds. get used to return that error from inside its
// sync loop, having already updated the branches before it — leaving the run
// half-applied and HEAD on a branch the user never asked for.
//
// The branches it can sync must still sync, and the one it cannot must be named
// along with the worktree holding it: the remedy lives in a directory the user
// is not looking at.
func TestGetSkipsBranchHeldByAnotherWorktree(t *testing.T) {
	t.Parallel()
	sh := scenario.NewRemoteScenario(t)

	// Build main -> a -> b and push metadata, so get can resolve b's ancestry
	// and pull a into the same sync set.
	sh.CreateBranch("a").CommitChange("a.txt", "a").TrackBranch("a", "main")
	sh.CreateBranch("b").CommitChange("b.txt", "b").TrackBranch("b", "a")

	config := testhelpers.NewMockGitHubServerConfig()
	rawClient, owner, repo := testhelpers.NewMockGitHubClient(t, config)
	sh.Context.GitHubClient = testhelpers.NewMockGitHubClientInterface(rawClient, owner, repo, config)

	require.NoError(t, submit.Action(sh.Context, submit.Options{NoEdit: true, Draft: true}, &noopHandler{}))
	require.NoError(t, syncaction.Action(sh.Context, syncaction.Options{}, nil))

	// Park a in a plain git worktree. Both a and b already exist locally, so
	// get takes the update path for each and needs them at HEAD in turn.
	sh.Checkout("b")
	worktreeDir := t.TempDir()
	require.NoError(t, sh.Scene.Repo.RunGitCommand("worktree", "add", worktreeDir, "a"))
	sh.Rebuild()

	aBefore, err := sh.Scene.Repo.GetRevision("a")
	require.NoError(t, err)

	handler := &skipRecordingGetHandler{}
	require.NoError(t, actions.GetAction(sh.Context, "b", actions.GetOptions{Restack: false}, handler),
		"a branch held elsewhere must not fail the whole run")
	sh.Rebuild()

	require.Contains(t, handler.skipped, "a", "the held branch must be reported, not silently passed over")
	require.Contains(t, handler.skipped["a"], worktreeDir,
		"the report has to name the worktree holding it")

	aAfter, err := sh.Scene.Repo.GetRevision("a")
	require.NoError(t, err)
	require.Equal(t, aBefore, aAfter, "a is left exactly as its worktree has it")

	// The branch get was actually asked for still lands, and HEAD ends on it
	// rather than wherever the loop happened to stop.
	require.Equal(t, "b", sh.Engine.CurrentBranchName())
}
