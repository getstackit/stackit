package engine_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

// These cover the non-plan RestackBranches path — the one reached by
// `continue`, `delete`, `squash`, `absorb`, `pluck` and `insert`, as opposed to
// the `restack` command's PlanRestack path. #1488 fixed worktree-anchor
// handling only in the plan; this is the sibling path (#1493).
//
// A faithful anchor is built by hand rather than with WithStack: a real
// worktree anchor holds no commits of its own (it marks where trunk was when
// the worktree was created), and WithStack commits on every branch it makes.
// That difference is the whole bug — a commit-less anchor is an ancestor of
// trunk, so the landed check treats it as already merged.
func withWorktreeAnchor(t *testing.T, s *scenario.Scenario, name string) {
	t.Helper()
	s.CreateBranchFromQuiet(name, "main").Rebuild()
	s.TrackBranch(name, "main")
	require.NoError(t, s.Engine.SetBranchType(s.Engine.GetBranch(name), git.BranchTypeWorktreeAnchor))
}

// TestRestackBranchesFastForwardsWorktreeAnchor covers the ordering half of
// #1493. An anchor holds no commits, so it is always an ancestor of trunk and
// branchLanded reports it as landed — returning "unneeded" before the
// fast-forward handler further down is ever reached. The anchor then never
// moves, and every consumer measuring a child against it drifts with each
// trunk advance.
func TestRestackBranchesFastForwardsWorktreeAnchor(t *testing.T) {
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
	withWorktreeAnchor(t, s, "anchor")

	oldSHA, err := s.Engine.GetBranch("anchor").GetRevision()
	require.NoError(t, err)

	s.Checkout("main").Commit("main update").Rebuild()
	trunkSHA, err := s.Engine.Trunk().GetRevision()
	require.NoError(t, err)
	require.NotEqual(t, oldSHA, trunkSHA)

	_, err = s.Engine.RestackBranches(context.Background(), engine.BranchesOf(s.Engine.GetBranch("anchor")))
	require.NoError(t, err)

	anchorSHA, err := s.Scene.Repo.GetBranchSHA("anchor")
	require.NoError(t, err)
	require.Equal(t, trunkSHA, anchorSHA,
		"anchor must fast-forward to trunk on the non-plan path too")
}

// TestRestackBranchesDoesNotReplayTrunkOntoAnchorChild covers the rebase-range
// half. Restacking a child of an anchor rebases onto trunk's tip but measures
// the other end of the range against the stale anchor, so trunk commits can
// fall inside the replay range. .claude/rules/multiplayer-safety.md requires
// <trunk>..<branch> to equal exactly the branch's own commits.
//
// Two rounds: one restack alone is fine, because the recorded revision really
// is where the branch is based. The second is where a stale anchor bites.
func TestRestackBranchesDoesNotReplayTrunkOntoAnchorChild(t *testing.T) {
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
	withWorktreeAnchor(t, s, "anchor")

	s.CreateBranchFromQuiet("feature", "anchor").
		CommitChange("feature", "feat: feature").
		Rebuild()
	s.TrackBranch("feature", "anchor")

	for round, msg := range []string{"trunk one", "trunk two"} {
		s.Checkout("main").Commit(msg).Rebuild()

		_, err := s.Engine.RestackBranches(context.Background(), engine.BranchesOf(s.Engine.GetBranch("feature")))
		require.NoError(t, err)

		require.Equal(t, 1, s.BranchCommitCount("feature"),
			"round %d: feature must contain only its own commit, not replayed trunk commits", round+1)
	}
}
