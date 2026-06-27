package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

// These tests cover scenario 1: the local trunk has commits that are NOT on its
// remote-tracking branch (origin/main). Restacking rebases trunk-anchored
// branches onto the LOCAL trunk tip, so un-pushed or divergent trunk commits
// would be baked into the stack — work that only exists locally and may never
// land as-is. The restack trunk guard refuses in that state and tells the user
// to reconcile trunk with the remote first.
//
// The distinction from scenario 2 (synced trunk, which must succeed) is purely
// whether origin/<trunk> carries the trunk's new commits. The synced-trunk
// tests advance origin/main via SyncRemoteTrunkToLocal; these deliberately do
// not, so local trunk stays ahead of / diverged from the remote.

// planRestackFeatureErr runs PlanRestack for the "feature" branch and returns
// the error (if any) without asserting success — the guard rejects at plan time.
func planRestackFeatureErr(t *testing.T, sh *scenario.Scenario) error {
	t.Helper()
	sh.Checkout("feature")
	_, err := actions.PlanRestack(sh.Context, actions.RestackOptions{
		BranchName: "feature",
		Scope:      engine.StackRangeFull(),
	})
	return err
}

// TestRestackGuardRejectsTrunkAheadOfRemote is the headline scenario 1 case: we
// committed directly on local main and never pushed, so local main is ahead of
// origin/main. Restacking would build the stack on those un-pushed commits, so
// the guard must reject before any rebase runs.
func TestRestackGuardRejectsTrunkAheadOfRemote(t *testing.T) {
	t.Parallel()
	sh := scenario.NewRemoteScenario(t)
	disableCommitSigning(t, sh)

	// A normal stack on top of synced trunk.
	sh.CreateBranch("feature").
		CommitChange("feature", "my work").
		TrackBranch("feature", "main")

	// Commit directly on local main and DO NOT sync origin/main: local trunk is
	// now ahead of its remote-tracking branch.
	sh.Checkout("main")
	sh.CommitChange("local-only", "un-pushed trunk commit")
	sh.Rebuild()

	err := planRestackFeatureErr(t, sh)
	require.Error(t, err, "restack must refuse while local trunk is ahead of origin/main")
	require.Contains(t, err.Error(), "origin/main",
		"the error should name the remote-tracking trunk the user must reconcile with")
	requireCleanWorkingTree(t, sh)
}

// TestRestackGuardRejectsTrunkDivergedFromRemote covers the diverged shape:
// local main and origin/main each have a unique commit on top of their common
// ancestor. Restacking onto the local (divergent) trunk is just as unsafe as the
// ahead case, so the guard must reject here too.
func TestRestackGuardRejectsTrunkDivergedFromRemote(t *testing.T) {
	t.Parallel()
	sh := scenario.NewRemoteScenario(t)
	disableCommitSigning(t, sh)

	sh.CreateBranch("feature").
		CommitChange("feature", "my work").
		TrackBranch("feature", "main")

	// Common ancestor = current main tip.
	sh.Checkout("main")
	base, err := sh.Scene.Repo.RunGitCommandAndGetOutput("rev-parse", "--verify", "main")
	require.NoError(t, err)
	base = trimSHA(base)

	// Build a divergent remote commit from the common ancestor and point
	// origin/main at it (a commit local main will never contain).
	require.NoError(t, sh.Scene.Repo.RunGitCommand("branch", "remote-work", base))
	require.NoError(t, sh.Scene.Repo.CheckoutBranch("remote-work"))
	sh.CommitChange("remote-file", "landed on origin only")
	remoteSha, err := sh.Scene.Repo.RunGitCommandAndGetOutput("rev-parse", "--verify", "remote-work")
	require.NoError(t, err)
	require.NoError(t, sh.Scene.Repo.RunGitCommand("update-ref", "refs/remotes/origin/main", trimSHA(remoteSha)))
	require.NoError(t, sh.Scene.Repo.CheckoutBranch("main"))
	require.NoError(t, sh.Scene.Repo.RunGitCommand("branch", "-D", "remote-work"))

	// Local main gets its own unique commit on top of the common ancestor.
	sh.CommitChange("local-file", "local divergent commit")
	sh.Rebuild()

	guardErr := planRestackFeatureErr(t, sh)
	require.Error(t, guardErr, "restack must refuse while local trunk has diverged from origin/main")
	require.Contains(t, guardErr.Error(), "origin/main")
	requireCleanWorkingTree(t, sh)
}

// TestRestackAllowsTrunkBehindRemote is the boundary control: local trunk is
// strictly behind origin/main (the remote has commits we haven't merged, but we
// have no un-pushed trunk commits). Restacking builds only on commits that exist
// on origin/main, so the guard must NOT fire.
func TestRestackAllowsTrunkBehindRemote(t *testing.T) {
	t.Parallel()
	sh := scenario.NewRemoteScenario(t)
	disableCommitSigning(t, sh)

	sh.CreateBranch("feature").
		CommitChange("feature", "my work").
		TrackBranch("feature", "main")

	// Advance origin/main ahead of local main (a descendant), leaving local main
	// strictly behind. Local trunk stays an ancestor of origin/main.
	sh.Checkout("main")
	require.NoError(t, sh.Scene.Repo.RunGitCommand("branch", "remote-ahead", "main"))
	require.NoError(t, sh.Scene.Repo.CheckoutBranch("remote-ahead"))
	sh.CommitChange("remote-file", "landed on origin")
	remoteSha, err := sh.Scene.Repo.RunGitCommandAndGetOutput("rev-parse", "--verify", "remote-ahead")
	require.NoError(t, err)
	require.NoError(t, sh.Scene.Repo.RunGitCommand("update-ref", "refs/remotes/origin/main", trimSHA(remoteSha)))
	require.NoError(t, sh.Scene.Repo.CheckoutBranch("main"))
	require.NoError(t, sh.Scene.Repo.RunGitCommand("branch", "-D", "remote-ahead"))
	sh.Rebuild()

	err = planRestackFeatureErr(t, sh)
	require.NoError(t, err, "restack must proceed when local trunk is merely behind origin/main")
	requireCleanWorkingTree(t, sh)
}

// TestRestackGuardNoOpWithoutRemote confirms local-only repos are unaffected:
// with no remote-tracking trunk ref, the guard cannot (and must not) fire even
// when local trunk has commits. A solo user with no remote restacks freely.
func TestRestackGuardNoOpWithoutRemote(t *testing.T) {
	t.Parallel()
	sh := scenario.NewScenario(t, testhelpers.InitialCommitSceneSetup)
	disableCommitSigning(t, sh)

	sh.CreateBranch("feature").
		CommitChange("feature", "my work").
		TrackBranch("feature", "main")

	// Commit on local main. There is no origin/main to compare against.
	sh.Checkout("main")
	sh.CommitChange("local-only", "trunk commit")
	sh.Rebuild()

	err := planRestackFeatureErr(t, sh)
	require.NoError(t, err, "a repo with no remote-tracking trunk must not be guarded")
	requireCleanWorkingTree(t, sh)
}

// TestRestackGuardSkipsWhenNoBranches makes sure the guard does not turn an
// empty restack (e.g. trunk with no children) into an error just because local
// trunk happens to be ahead of the remote.
func TestRestackGuardSkipsWhenNoBranches(t *testing.T) {
	t.Parallel()
	sh := scenario.NewRemoteScenario(t)
	disableCommitSigning(t, sh)

	// Trunk ahead of origin, but nothing is stacked on it.
	sh.Checkout("main")
	sh.CommitChange("local-only", "un-pushed trunk commit")
	sh.Rebuild()

	_, err := actions.PlanRestack(sh.Context, actions.RestackOptions{
		BranchName: "main",
		Scope:      engine.StackRangeFull(),
	})
	require.NoError(t, err, "an empty restack must not be rejected by the trunk guard")
}

// trimSHA strips trailing whitespace/newline from a rev-parse result.
func trimSHA(s string) string {
	return strings.TrimSpace(s)
}
