package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/actions/submit"
	syncaction "github.com/getstackit/stackit/internal/actions/sync"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/handlers"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

// These tests model the state `st get` faces after a parent PR merges: GitHub
// deletes the merged branch, but the child's pushed metadata still names it as
// the parent. get's fetch asks for every ancestor head in one refspec list, and
// git fails the whole fetch when one of them is gone — so the branch the user
// asked for never arrives.
//
// Two things have to hold once the fetch survives that. The branch must be
// tracked against an ancestor that still exists, and — because the landed
// parent's commits are still in the fetched branch — a restack must replay only
// the branch's own commits. The second is the multiplayer-safety invariant
// (.claude/rules/multiplayer-safety.md, "Reparent Past Landed Work, Never
// Replay It") and it fails for squash and rebase merges if the divergence point
// is taken as a plain merge-base against the new parent.

// landedParentHandler answers get's landed-ancestor prompt and records what it
// was told, standing in for the interactive CLI handler.
type landedParentHandler struct {
	actions.GetNullHandler
	decision actions.LandedAncestorDecision
	reports  []actions.LandedAncestorReport
}

func (h *landedParentHandler) ReportLandedAncestors(report actions.LandedAncestorReport) (actions.LandedAncestorDecision, error) {
	h.reports = append(h.reports, report)
	return h.decision, nil
}

// mergeMethod lands branch a on trunk the way one of GitHub's merge buttons
// would. Each leaves a different history, and only the merge commit keeps a's
// original SHAs reachable from trunk.
type mergeMethod func(t *testing.T, sh *scenario.Scenario)

func mergeCommitLanding(t *testing.T, sh *scenario.Scenario) {
	t.Helper()
	require.NoError(t, sh.Scene.Repo.RunGitCommand("merge", "--no-ff", "-m", "Merge pull request #1", "a"))
}

// squashLanding lands a's whole tree as one commit, the multi-commit squash
// case: none of a's original commits are reachable from trunk and no single
// commit on trunk patch-matches any of them.
func squashLanding(t *testing.T, sh *scenario.Scenario) {
	t.Helper()
	require.NoError(t, sh.Scene.Repo.RunGitCommand("checkout", "a", "--", "."))
	require.NoError(t, sh.Scene.Repo.RunGitCommand("commit", "-m", "Squash a (#1)"))
}

// rebaseLanding replays a's commits onto trunk with fresh SHAs.
func rebaseLanding(t *testing.T, sh *scenario.Scenario) {
	t.Helper()
	out, err := sh.Scene.Repo.RunGitCommandAndGetOutput("rev-list", "--reverse", "main..a")
	require.NoError(t, err)
	for _, commit := range strings.Fields(out) {
		require.NoError(t, sh.Scene.Repo.RunGitCommand("cherry-pick", commit))
	}
}

// forgottenMetadata names the remote stackit metadata refs a scenario deletes
// alongside the landed branch, which decides how much of the ancestry `get` can
// still reconstruct.
type forgottenMetadata int

const (
	// keepAllMetadata leaves every metadata ref on the remote.
	keepAllMetadata forgottenMetadata = iota
	// forgetLandedMetadata deletes only the landed parent's metadata ref, which
	// is what the author's own `stackit sync` does during branch cleanup. The
	// ancestry crawl then abandons the chain and falls back to GitHub, but the
	// child's own metadata still records the tip it was pushed on top of.
	forgetLandedMetadata
	// forgetAllMetadata deletes the child's metadata ref as well, the shape a
	// collaborator who does not use stackit leaves behind. Nothing records the
	// landed parent's tip, so nothing can safely rebase the child.
	forgetAllMetadata
)

// stackWithLandedParent builds main -> a -> b, submits it so both branches and
// their metadata are on the remote, lands a on trunk with the given merge
// method, deletes a's branch from the remote, and forgets both branches
// locally.
func stackWithLandedParent(t *testing.T, land mergeMethod, forget forgottenMetadata) *scenario.Scenario {
	t.Helper()

	sh := scenario.NewRemoteScenario(t)
	disableCommitSigning(t, sh)

	// a has two commits so the squash case is the one that matters: a single
	// commit would patch-match through `git cherry` and prove nothing.
	sh.CreateBranch("a").
		CommitChange("a1", "a one").
		CommitChange("a2", "a two").
		TrackBranch("a", "main")
	// b touches a file of its own so a replay never conflicts — that isolates
	// "did b inherit a's commits" from any conflict noise.
	sh.CreateBranch("b").
		CommitChange("b1", "b one").
		TrackBranch("b", "a")

	sh.Context.GitHubClient = newCountingGitHubClient(t, testhelpers.NewMockGitHubServerConfig())
	require.NoError(t, submit.Action(sh.Context, submit.Options{NoEdit: true, Draft: true}, &noopHandler{}))
	require.NoError(t, syncaction.Action(sh.Context, syncaction.Options{}, nil))

	sh.Checkout("main")
	land(t, sh)
	// Trunk moves on after the merge, as it always does in a shared repo. This
	// is what makes replaying a's commits harmful rather than merely wasteful:
	// applied on top of someone else's edit to the same file they conflict,
	// where skipping them entirely rebases cleanly.
	sh.CommitChange("a1", "a one, revised on trunk")
	require.NoError(t, sh.Scene.Repo.RunGitCommand("push", "origin", "main"))
	require.NoError(t, sh.Scene.Repo.RunGitCommand("push", "origin", ":refs/heads/a"))
	if forget >= forgetLandedMetadata {
		require.NoError(t, sh.Scene.Repo.RunGitCommand("push", "origin", ":refs/stackit/metadata/a"))
	}
	if forget >= forgetAllMetadata {
		require.NoError(t, sh.Scene.Repo.RunGitCommand("push", "origin", ":refs/stackit/metadata/b"))
	}

	// Forget both branches locally: this is a fresh clone's view, where get has
	// to recreate b from the remote and has no local a to fall back on.
	dropLocalBranch(t, sh, "b")
	dropLocalBranch(t, sh, "a")
	sh.Rebuild()

	return sh
}

// restackBranchOntoStack restacks a single branch and its upstack, the same
// work `st restack` does after an unfreeze.
func restackBranchOntoStack(t *testing.T, sh *scenario.Scenario, branch string) {
	t.Helper()
	sh.Checkout(branch)
	plan, err := actions.PlanRestack(sh.Context, actions.RestackOptions{
		BranchName: branch,
		Scope:      engine.StackRangeFull(),
	})
	require.NoError(t, err)
	require.NoError(t, actions.RestackAction(sh.Context, plan, &handlers.NullRestackHandler{}))
	sh.Rebuild()
}

// commitsInB counts the commits reachable from b but not from main. b owns
// exactly one; anything more means it inherited a's landed commits.
func commitsInB(t *testing.T, sh *scenario.Scenario) int {
	t.Helper()
	count, err := sh.Scene.Repo.GetCommitCount("main", "b")
	require.NoError(t, err)
	return count
}

func TestGetPastLandedParent(t *testing.T) {
	t.Parallel()

	// Every merge method must reach the same place: b fetched, tracked against
	// main, and carrying only its own commit after the restack.
	for _, tc := range []struct {
		name string
		land mergeMethod
	}{
		{"merge commit", mergeCommitLanding},
		{"multi-commit squash", squashLanding},
		{"rebase merge", rebaseLanding},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sh := stackWithLandedParent(t, tc.land, keepAllMetadata)

			handler := &landedParentHandler{decision: actions.UnfreezeAndRestack}
			require.NoError(t, actions.GetAction(sh.Context, "b", actions.GetOptions{Restack: true}, handler))
			sh.Rebuild()

			sh.ExpectStackStructure(map[string]string{"b": "main"})
			require.Equal(t, 1, commitsInB(t, sh),
				"b must contain only its own commit, not the landed parent's")
			require.False(t, sh.Engine.GetBranch("b").IsFrozen(),
				"confirming the prompt should leave b editable")
			requireCleanWorkingTree(t, sh)
		})
	}
}

func TestGetPastLandedParentReport(t *testing.T) {
	t.Parallel()

	t.Run("names the landed parent, its PR, and the branch left frozen", func(t *testing.T) {
		t.Parallel()
		sh := stackWithLandedParent(t, squashLanding, keepAllMetadata)

		handler := &landedParentHandler{decision: actions.LeaveFrozen}
		require.NoError(t, actions.GetAction(sh.Context, "b", actions.GetOptions{Restack: true}, handler))

		require.Len(t, handler.reports, 1)
		report := handler.reports[0]
		require.True(t, report.CanRestack)
		require.Equal(t, []string{"b"}, report.Unfreezable(),
			"get freezes new branches, so b is what stays stale")
		require.Empty(t, report.Unanchored(),
			"b's own remote metadata records the tip it was pushed on top of")
		require.Len(t, report.Reanchored, 1)
		require.Equal(t, "b", report.Reanchored[0].Branch)
		require.Equal(t, "a", report.Reanchored[0].LandedParent)
		require.Equal(t, "main", report.Reanchored[0].NewParent)
		require.NotNil(t, report.Reanchored[0].LandedPR,
			"a's PR number is in the metadata it was pushed with")
	})

	// Declining is get's contract, not a failure: the branch keeps mirroring
	// the remote. It is still tracked against a parent that exists, so the
	// stack renders, and the divergence point recorded for it is the landed
	// tip — so a later unfreeze + restack still replays only b's commit.
	t.Run("declining leaves the branch frozen and unrebased", func(t *testing.T) {
		t.Parallel()
		sh := stackWithLandedParent(t, squashLanding, keepAllMetadata)

		handler := &landedParentHandler{decision: actions.LeaveFrozen}
		require.NoError(t, actions.GetAction(sh.Context, "b", actions.GetOptions{Restack: true}, handler))
		sh.Rebuild()

		sh.ExpectStackStructure(map[string]string{"b": "main"})
		require.True(t, sh.Engine.GetBranch("b").IsFrozen())
		require.Equal(t, 3, commitsInB(t, sh),
			"a frozen branch still mirrors the remote, landed commits and all")

		// The remedy get points at has to work.
		require.NoError(t, actions.UnfreezeAction(sh.Context, "b"))
		sh.Rebuild()
		restackBranchOntoStack(t, sh, "b")
		require.Equal(t, 1, commitsInB(t, sh))
	})

	// A parent that is gone from the remote but still checked out locally is a
	// branch the user may still be working with. get leaves the relationship
	// alone; restack's own landed-parent handling reparents past it.
	t.Run("does not re-anchor past a parent that still exists locally", func(t *testing.T) {
		t.Parallel()
		sh := scenario.NewRemoteScenario(t)
		disableCommitSigning(t, sh)

		sh.CreateBranch("a").
			CommitChange("a1", "a one").
			CommitChange("a2", "a two").
			TrackBranch("a", "main")
		sh.CreateBranch("b").
			CommitChange("b1", "b one").
			TrackBranch("b", "a")

		sh.Context.GitHubClient = newCountingGitHubClient(t, testhelpers.NewMockGitHubServerConfig())
		require.NoError(t, submit.Action(sh.Context, submit.Options{NoEdit: true, Draft: true}, &noopHandler{}))
		require.NoError(t, syncaction.Action(sh.Context, syncaction.Options{}, nil))

		sh.Checkout("main")
		squashLanding(t, sh)
		require.NoError(t, sh.Scene.Repo.RunGitCommand("push", "origin", "main"))
		require.NoError(t, sh.Scene.Repo.RunGitCommand("push", "origin", ":refs/heads/a"))
		sh.Rebuild()

		handler := &landedParentHandler{decision: actions.UnfreezeAndRestack}
		require.NoError(t, actions.GetAction(sh.Context, "b", actions.GetOptions{Restack: true}, handler))
		sh.Rebuild()

		require.Empty(t, handler.reports, "a is still local, so nothing was re-anchored")
		require.Equal(t, 1, commitsInB(t, sh),
			"restack should reparent b past the landed local parent on its own")
	})
}

// The ancestry crawl abandons the whole chain the moment one ancestor has no
// metadata — and that is the state the author's own `stackit sync` leaves a
// merged parent in, since branch cleanup deletes its remote metadata ref. get
// then falls back to a GitHub crawl, which reads PR bases and knows no
// revisions. Without harvesting the anchor from the child's own metadata, the
// branch is re-anchored with nothing to anchor against and the restack replays
// the landed commits.
func TestGetPastLandedParentWithoutParentMetadata(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		land mergeMethod
	}{
		{"merge commit", mergeCommitLanding},
		{"multi-commit squash", squashLanding},
		{"rebase merge", rebaseLanding},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sh := stackWithLandedParent(t, tc.land, forgetLandedMetadata)

			handler := &landedParentHandler{decision: actions.UnfreezeAndRestack}
			require.NoError(t, actions.GetAction(sh.Context, "b", actions.GetOptions{Restack: true}, handler))
			sh.Rebuild()

			require.Len(t, handler.reports, 1)
			require.Empty(t, handler.reports[0].Unanchored(),
				"b's own metadata survives and records the tip it was pushed on top of")
			sh.ExpectStackStructure(map[string]string{"b": "main"})
			require.Equal(t, 1, commitsInB(t, sh),
				"b must contain only its own commit, not the landed parent's")
			requireCleanWorkingTree(t, sh)
		})
	}
}

// With no stackit metadata anywhere on the remote there is nothing recording
// the tip b was pushed on top of, so nothing can tell the landed commits apart
// from b's own. Re-anchoring is still right — tracking b against a branch that
// no longer exists would be worse — but rebasing is not, so get must report the
// branch as unanchored and leave it frozen rather than offer a replay.
func TestGetPastLandedParentWithNoMetadataAtAll(t *testing.T) {
	t.Parallel()
	sh := stackWithLandedParent(t, squashLanding, forgetAllMetadata)

	handler := &landedParentHandler{decision: actions.UnfreezeAndRestack}
	require.NoError(t, actions.GetAction(sh.Context, "b", actions.GetOptions{Restack: true}, handler))
	sh.Rebuild()

	require.Len(t, handler.reports, 1)
	report := handler.reports[0]
	require.Equal(t, []string{"b"}, report.Unanchored(),
		"nothing recorded a's tip, so b cannot be rebased safely")
	require.Empty(t, report.Unfreezable(),
		"an unanchored branch must never be offered for unfreeze")

	sh.ExpectStackStructure(map[string]string{"b": "main"})
	require.True(t, sh.Engine.GetBranch("b").IsFrozen(),
		"b stays frozen even though the handler asked to unfreeze")
	require.Equal(t, 3, commitsInB(t, sh),
		"b goes on mirroring the remote rather than replaying the landed commits")
	requireCleanWorkingTree(t, sh)
}

// --restack=false leaves nothing for an unfreeze to feed, so get must not act
// on a handler that asks for one anyway.
func TestGetPastLandedParentWithoutRestack(t *testing.T) {
	t.Parallel()
	sh := stackWithLandedParent(t, squashLanding, keepAllMetadata)

	handler := &landedParentHandler{decision: actions.UnfreezeAndRestack}
	require.NoError(t, actions.GetAction(sh.Context, "b", actions.GetOptions{Restack: false}, handler))
	sh.Rebuild()

	require.Len(t, handler.reports, 1)
	require.False(t, handler.reports[0].CanRestack)
	require.True(t, sh.Engine.GetBranch("b").IsFrozen(),
		"unfreezing without a restack would drop the protection and rebase nothing")
}
