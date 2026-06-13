package sync

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/testhelpers/scenario"
)

// setupBehindBranch pushes branch to origin, adds one more commit, pushes that,
// then resets the local branch one commit back. The local branch is left BEHIND
// its remote by exactly one (fast-forwardable) commit. Returns the origin SHA the
// branch should fast-forward to. The branch must already exist and be checked
// out-able.
func setupBehindBranch(t *testing.T, s *scenario.Scenario, branch string) string {
	t.Helper()
	s.Checkout(branch)
	require.NoError(t, s.Scene.Repo.PushBranch("origin", branch))
	s.CommitChange(branch+"-advance", "advance "+branch)
	want, err := s.Scene.Repo.GetRevision(branch)
	require.NoError(t, err)
	require.NoError(t, s.Scene.Repo.PushBranch("origin", branch))
	require.NoError(t, s.Scene.Repo.RunGitCommand("reset", "--hard", "HEAD~1"))
	return want
}

// TestSyncStackBranchesBatchFastForward verifies that several branches that are
// behind their remote are all fast-forwarded by syncStackBranches using a single
// batched fetch (the per-branch fetch N+1 was removed).
func TestSyncStackBranchesBatchFastForward(t *testing.T) {
	s := scenario.NewRemoteScenario(t)

	// Linear stack: main -> a -> b -> c, each tracked.
	s.CreateBranch("a").CommitChange("a-1", "a one").TrackBranch("a", "main")
	s.CreateBranch("b").CommitChange("b-1", "b one").TrackBranch("b", "a")
	s.CreateBranch("c").CommitChange("c-1", "c one").TrackBranch("c", "b")

	// Make each branch one commit behind its remote.
	want := map[string]string{
		"a": setupBehindBranch(t, s, "a"),
		"b": setupBehindBranch(t, s, "b"),
		"c": setupBehindBranch(t, s, "c"),
	}

	// Move off the stack so the fast-forwards are pure ref updates.
	s.Checkout("main")

	summary := &Summary{}
	// Pass nil statuses to exercise the on-demand fallback path.
	require.NoError(t, syncStackBranches(s.Context, nil, nil, &NullHandler{}, summary))

	require.Equal(t, 3, summary.BranchesSynced, "all three behind branches should be synced")
	require.Empty(t, summary.ConflictBranches, "no branch should conflict")

	for branch, wantSha := range want {
		got, err := s.Scene.Repo.GetRevision(branch)
		require.NoError(t, err)
		require.Equal(t, wantSha, got, "branch %s should be fast-forwarded to its remote tip", branch)
	}
}

// TestSyncStackBranchesSkipsDiverged verifies that a branch that has diverged
// from its remote (both sides have unique commits) is left untouched: it fails
// the Behind() gate, so it is neither fast-forwarded nor reported as a conflict.
func TestSyncStackBranchesSkipsDiverged(t *testing.T) {
	s := scenario.NewRemoteScenario(t)

	s.CreateBranch("d").CommitChange("d-1", "d one").TrackBranch("d", "main")
	s.Checkout("d")
	require.NoError(t, s.Scene.Repo.PushBranch("origin", "d"))

	// Remote advances with one commit...
	s.CommitChange("d-remote", "remote change")
	require.NoError(t, s.Scene.Repo.PushBranch("origin", "d"))
	require.NoError(t, s.Scene.Repo.RunGitCommand("reset", "--hard", "HEAD~1"))

	// ...and local advances with a different commit -> diverged.
	s.CommitChange("d-local", "local change")
	localSha, err := s.Scene.Repo.GetRevision("d")
	require.NoError(t, err)
	s.Rebuild()

	summary := &Summary{}
	// Pass nil statuses to exercise the on-demand fallback path.
	require.NoError(t, syncStackBranches(s.Context, nil, nil, &NullHandler{}, summary))

	require.Equal(t, 0, summary.BranchesSynced, "diverged branch should not be synced")
	require.NotContains(t, summary.ConflictBranches, "d", "diverged branch is skipped, not flagged as conflict")

	got, err := s.Scene.Repo.GetRevision("d")
	require.NoError(t, err)
	require.Equal(t, localSha, got, "diverged branch should be left untouched")
}

// TestSyncActionFastForwardsBehindBranches verifies the full sync.Action
// pipeline fast-forwards behind branches end-to-end. Uses independent branches
// off main so no restack is triggered, isolating the branch-sync behavior.
func TestSyncActionFastForwardsBehindBranches(t *testing.T) {
	s := scenario.NewRemoteScenario(t)

	want := map[string]string{}
	for _, name := range []string{"x", "y", "z"} {
		s.Checkout("main")
		s.CreateBranch(name).CommitChange(name+"-1", name+" one").TrackBranch(name, "main")
		want[name] = setupBehindBranch(t, s, name)
	}

	s.Checkout("main")

	require.NoError(t, Action(s.Context, Options{}, nil))

	for branch, wantSha := range want {
		got, err := s.Scene.Repo.GetRevision(branch)
		require.NoError(t, err)
		require.Equal(t, wantSha, got, "branch %s should be fast-forwarded by sync.Action", branch)
	}
}
