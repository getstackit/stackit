package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/handlers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

// These tests model the real collaboration case: a teammate who does NOT use
// stackit pushes a branch, I stack my own work on top of it, and the teammate
// then squash- or rebase-merges their branch into main on GitHub. Their merged
// work carries ZERO stackit metadata (no recorded PR number, no MERGED state) —
// stackit only knows the branch as a local parent pointer, if that.
//
// The invariant under test: after I restack, my branch must contain ONLY my own
// commit. If detection misses the teammate's merge, restack replays their
// pre-merge commits into my branch (issue #1345's data-loss shape), and
// `main..mine` carries their commits as well as mine.
//
// My commit touches a unique file so the replay never conflicts — that isolates
// the "did I inherit foreign commits" question from any conflict noise.

// reparentCountAfterRestack restacks `mine` and returns the number of commits
// reachable from `mine` but not `main`. A correct restack onto main yields 1
// (just my commit); inheriting the teammate's pre-merge commits yields more.
func restackMineOntoTrunk(t *testing.T, sh *scenario.Scenario) {
	t.Helper()
	sh.Checkout("mine")
	plan, err := actions.PlanRestack(sh.Context, actions.RestackOptions{
		BranchName: "mine",
		Scope:      engine.StackRangeFull(),
	})
	require.NoError(t, err)
	require.NoError(t, actions.RestackAction(sh.Context, plan, &handlers.NullRestackHandler{}))
	sh.Rebuild()
}

func mineCommitsAboveTrunk(t *testing.T, sh *scenario.Scenario) int {
	t.Helper()
	count, err := sh.Scene.Repo.GetCommitCount("main", "mine")
	require.NoError(t, err)
	return count
}

// rebaseMergeOntoMain replays each of the listed commits onto main with new
// SHAs, simulating GitHub's "Rebase and merge". Patch-ids are preserved. The
// merge landed on the real trunk and was fetched, so origin/main is advanced to
// match: restack builds on synced trunk, not un-pushed local commits.
func rebaseMergeOntoMain(t *testing.T, sh *scenario.Scenario, commits []string) {
	t.Helper()
	sh.Checkout("main")
	for _, c := range commits {
		require.NoError(t, sh.Scene.Repo.RunGitCommand("cherry-pick", c))
	}
	sh.Rebuild()
	sh.SyncRemoteTrunkToLocal()
}

func coworkerCommits(t *testing.T, sh *scenario.Scenario) []string {
	t.Helper()
	out, err := sh.Scene.Repo.RunGitCommandAndGetOutput("rev-list", "--reverse", "main..coworker")
	require.NoError(t, err)
	return strings.Fields(out)
}

// --- Squash merges ---------------------------------------------------------

// TestUntrackedCoworkerMultiCommitSquash is the headline failing case: a
// teammate's MULTI-commit branch is squash-merged into main with no stackit
// metadata. `git cherry` cannot match the combined squash commit to any
// individual commit; the aggregate-patch-id scan must detect it even without
// a recorded PR number so my branch does not inherit the teammate's two
// pre-squash commits.
func TestUntrackedCoworkerMultiCommitSquash(t *testing.T) {
	t.Parallel()
	sh := scenario.NewRemoteScenario(t)
	disableCommitSigning(t, sh)

	// Teammate branch: two commits, tracked locally (so I can stack on it) but
	// NO PR metadata — exactly what a non-stackit collaborator's branch looks
	// like in my local view.
	sh.CreateBranch("coworker").
		CommitChange("shared", "v1").
		CommitChange("shared", "v1\nv2").
		TrackBranch("coworker", "main")

	// My branch on top, touching a unique file so replay never conflicts.
	sh.CreateBranch("mine").
		CommitChange("mine", "my work").
		TrackBranch("mine", "coworker")

	// GitHub squash-merges the teammate's branch: one combined commit on main.
	// The merge landed on the real trunk and was fetched, so origin/main matches.
	sh.Checkout("main")
	sh.CommitChange("shared", "v1\nv2")
	sh.Rebuild()
	sh.SyncRemoteTrunkToLocal()

	restackMineOntoTrunk(t, sh)

	require.Equal(t, "main", sh.Engine.GetBranch("mine").GetParent().GetName(),
		"mine should reparent to main past the squash-merged teammate branch")
	require.Equal(t, 1, mineCommitsAboveTrunk(t, sh),
		"mine must contain only my own commit, not the teammate's pre-squash commits")
	requireCleanWorkingTree(t, sh)
}

// TestUntrackedCoworkerSingleCommitSquash is the control: a SINGLE-commit branch
// squash-merged with no metadata. Here `git cherry` (cheap IsMerged) CAN match
// the lone commit to the squash commit, so detection should succeed without any
// PR number.
func TestUntrackedCoworkerSingleCommitSquash(t *testing.T) {
	t.Parallel()
	sh := scenario.NewRemoteScenario(t)
	disableCommitSigning(t, sh)

	sh.CreateBranch("coworker").
		CommitChange("shared", "only change").
		TrackBranch("coworker", "main")

	sh.CreateBranch("mine").
		CommitChange("mine", "my work").
		TrackBranch("mine", "coworker")

	sh.Checkout("main")
	sh.CommitChange("shared", "only change")
	sh.Rebuild()
	sh.SyncRemoteTrunkToLocal()

	restackMineOntoTrunk(t, sh)

	require.Equal(t, "main", sh.Engine.GetBranch("mine").GetParent().GetName(),
		"mine should reparent to main past the single-commit squash")
	require.Equal(t, 1, mineCommitsAboveTrunk(t, sh),
		"mine must contain only my own commit")
	requireCleanWorkingTree(t, sh)
}

// --- Rebase merges ---------------------------------------------------------

// TestUntrackedCoworkerMultiCommitRebaseMerge covers GitHub "Rebase and merge"
// of a MULTI-commit teammate branch with no metadata. Each commit is replayed
// onto main with a new SHA but the same patch-id, so `git cherry` matches every
// commit and cheap IsMerged detects the merge without a PR number.
func TestUntrackedCoworkerMultiCommitRebaseMerge(t *testing.T) {
	t.Parallel()
	sh := scenario.NewRemoteScenario(t)
	disableCommitSigning(t, sh)

	sh.CreateBranch("coworker").
		CommitChange("shared", "v1").
		CommitChange("shared", "v1\nv2").
		TrackBranch("coworker", "main")

	sh.CreateBranch("mine").
		CommitChange("mine", "my work").
		TrackBranch("mine", "coworker")

	rebaseMergeOntoMain(t, sh, coworkerCommits(t, sh))

	restackMineOntoTrunk(t, sh)

	require.Equal(t, "main", sh.Engine.GetBranch("mine").GetParent().GetName(),
		"mine should reparent to main past the rebase-merged teammate branch")
	require.Equal(t, 1, mineCommitsAboveTrunk(t, sh),
		"mine must contain only my own commit after a rebase merge")
	requireCleanWorkingTree(t, sh)
}

// TestUntrackedCoworkerSingleCommitRebaseMerge is the single-commit rebase-merge
// control.
func TestUntrackedCoworkerSingleCommitRebaseMerge(t *testing.T) {
	t.Parallel()
	sh := scenario.NewRemoteScenario(t)
	disableCommitSigning(t, sh)

	sh.CreateBranch("coworker").
		CommitChange("shared", "only change").
		TrackBranch("coworker", "main")

	sh.CreateBranch("mine").
		CommitChange("mine", "my work").
		TrackBranch("mine", "coworker")

	rebaseMergeOntoMain(t, sh, coworkerCommits(t, sh))

	restackMineOntoTrunk(t, sh)

	require.Equal(t, "main", sh.Engine.GetBranch("mine").GetParent().GetName(),
		"mine should reparent to main past the single-commit rebase merge")
	require.Equal(t, 1, mineCommitsAboveTrunk(t, sh),
		"mine must contain only my own commit")
	requireCleanWorkingTree(t, sh)
}

// --- Fully untracked parent ------------------------------------------------

// TestFullyUntrackedCoworkerMultiCommitSquash is the most literal "zero stackit
// metadata" case: the teammate branch has NO metadata ref at all (never tracked
// by stackit). My branch records it as a parent. After a multi-commit squash
// merge, restack must still strip the teammate's commits.
func TestFullyUntrackedCoworkerMultiCommitSquash(t *testing.T) {
	t.Parallel()
	sh := scenario.NewRemoteScenario(t)
	disableCommitSigning(t, sh)

	// coworker is a raw git branch with no stackit metadata of its own.
	sh.CreateBranch("coworker").
		CommitChange("shared", "v1").
		CommitChange("shared", "v1\nv2")

	// mine records coworker as parent (the only stackit metadata in play).
	sh.CreateBranch("mine").
		CommitChange("mine", "my work").
		TrackBranch("mine", "coworker")

	sh.Checkout("main")
	sh.CommitChange("shared", "v1\nv2")
	sh.Rebuild()
	sh.SyncRemoteTrunkToLocal()

	restackMineOntoTrunk(t, sh)

	require.Equal(t, 1, mineCommitsAboveTrunk(t, sh),
		"mine must contain only my own commit even when the merged parent was never tracked")
	requireCleanWorkingTree(t, sh)
}
