package integration

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/actions/sync"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/handlers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

// markPrWithState records a PR number and an explicit (possibly stale) state on
// a branch's metadata, without asserting the PR has merged. Used to simulate a
// branch whose PR was merged on GitHub before its local state synced to MERGED.
func markPrWithState(t *testing.T, sh *scenario.Scenario, branch string, prNumber int, state, base string) {
	t.Helper()
	meta, err := sh.Engine.Git().ReadMetadata(branch)
	require.NoError(t, err)
	num := prNumber
	s := state
	b := base
	meta = meta.WithPrInfo(&git.PrInfoPersistence{
		Number: &num,
		State:  &s,
		Base:   &b,
	})
	require.NoError(t, sh.Engine.Git().WriteMetadata(branch, meta))
}

// TestSquashMergeMultiCommitParent reproduces the case where a parent branch
// with MULTIPLE commits is squash-merged on GitHub. The squash commit's
// patch-id matches none of the parent's individual commits, so patch-id
// based merge detection (git cherry) cannot see the parent as merged.
// The child must still restack onto trunk replaying only its own commit.
func TestSquashMergeMultiCommitParent(t *testing.T) {
	t.Parallel()
	sh := scenario.NewRemoteScenario(t)
	disableCommitSigning(t, sh)

	// Parent: 3 commits evolving the same file (diffs build on each other,
	// so replaying them onto the squashed result conflicts).
	sh.CreateBranch("parent").
		CommitChange("file-p", "v1").
		CommitChange("file-p", "v1\nv2").
		CommitChange("file-p", "v1\nv2\nv3").
		TrackBranch("parent", "main")

	// Child: 1 unique commit on top.
	sh.CreateBranch("child").
		CommitChange("file-c", "c").
		TrackBranch("child", "parent")

	// GitHub squash-merges parent's PR: one combined commit lands on main.
	mainName := sh.Engine.Trunk().GetName()
	sh.Checkout("main")
	sh.CommitChange("file-p", "v1\nv2\nv3")
	markPrMerged(t, sh, "parent", 1, mainName)

	require.NoError(t, sync.Action(sh.Context, sync.Options{Restack: true}, nil))

	branches, err := sh.Scene.Repo.GetLocalBranches()
	require.NoError(t, err)
	require.NotContains(t, branches, "parent", "squash-merged parent should be deleted")
	require.Contains(t, branches, "child")

	require.Equal(t, mainName, sh.Engine.GetBranch("child").GetParent().GetName())

	// Child must keep ONLY its own commit — parent's 3 commits must not replay.
	cCount, err := sh.Engine.GetCommitCount(sh.Engine.GetBranch("child"))
	require.NoError(t, err)
	require.Equal(t, 1, cCount, "child should not inherit parent's squashed commits")

	requireCleanWorkingTree(t, sh)
}

// TestRestackAfterMultiCommitSquashParentDeleted covers the case where the
// squash-merged parent branch is already gone locally (deleted outside
// stackit's sync cleanup) and the child is restacked. Reparenting happens in
// restack itself; with the parent ref gone, the divergence-preservation check
// in setParentRecomputingDivergence cannot run and the child's divergence
// point falls back to the fork point, replaying the parent's commits.
func TestRestackAfterMultiCommitSquashParentDeleted(t *testing.T) {
	t.Parallel()
	sh := scenario.NewRemoteScenario(t)
	disableCommitSigning(t, sh)

	sh.CreateBranch("parent").
		CommitChange("file-p", "v1").
		CommitChange("file-p", "v1\nv2").
		CommitChange("file-p", "v1\nv2\nv3").
		TrackBranch("parent", "main")

	sh.CreateBranch("child").
		CommitChange("file-c", "c").
		TrackBranch("child", "parent")

	mainName := sh.Engine.Trunk().GetName()
	sh.Checkout("main")
	sh.CommitChange("file-p", "v1\nv2\nv3")
	// The squash landed on the real trunk and was fetched, so origin/main carries
	// it too; restack builds on synced trunk, not un-pushed local commits.
	sh.SyncRemoteTrunkToLocal()

	// Parent branch deleted locally (e.g. user cleanup after GitHub merge).
	require.NoError(t, sh.Scene.Repo.RunGitCommand("branch", "-D", "parent"))
	sh.Rebuild()

	sh.Checkout("child")
	plan, err := actions.PlanRestack(sh.Context, actions.RestackOptions{
		BranchName: "child",
		Scope:      engine.StackRangeFull(),
	})
	require.NoError(t, err)
	require.NoError(t, actions.RestackAction(sh.Context, plan, &handlers.NullRestackHandler{}))

	sh.Rebuild()
	require.Equal(t, mainName, sh.Engine.GetBranch("child").GetParent().GetName())

	cCount, err := sh.Engine.GetCommitCount(sh.Engine.GetBranch("child"))
	require.NoError(t, err)
	require.Equal(t, 1, cCount, "child should not inherit parent's squashed commits")

	requireCleanWorkingTree(t, sh)
}

// TestRestackDetectsSquashMergeWithStalePRState covers running `stackit restack`
// when the parent was squash-merged on GitHub but its local PR state has not yet
// synced to MERGED (e.g. the merge happened out of band, or a prior sync ran
// offline). The aggregate patch-id squash scan must detect the merge and cause
// the child to reparent to trunk without replaying the parent's commits.
func TestRestackDetectsSquashMergeWithStalePRState(t *testing.T) {
	t.Parallel()
	sh := scenario.NewRemoteScenario(t)
	disableCommitSigning(t, sh)

	sh.CreateBranch("parent").
		CommitChange("file-p", "v1").
		CommitChange("file-p", "v1\nv2").
		TrackBranch("parent", "main")

	sh.CreateBranch("child").
		CommitChange("file-c", "c").
		TrackBranch("child", "parent")

	// Squash-merge parent onto main. The PR number is recorded but its state is
	// stale (still OPEN) — simulating a GitHub UI squash-merge before st sync
	// marked the PR MERGED.
	mainName := sh.Engine.Trunk().GetName()
	sh.Checkout("main")
	sh.CommitChange("file-main", "unrelated")
	sh.CommitChange("file-p", "v1\nv2")

	// Stale PR state (OPEN, not MERGED) forces detection through the aggregate
	// patch-id scan rather than PR metadata. The unrelated trunk commit means a
	// whole-tree comparison would not match the parent tip.
	markPrWithState(t, sh, "parent", 42, "OPEN", mainName)
	// Trunk advanced from a real merge that was fetched, so origin/main matches.
	sh.SyncRemoteTrunkToLocal()

	sh.Checkout("child")
	plan, err := actions.PlanRestack(sh.Context, actions.RestackOptions{
		BranchName: "child",
		Scope:      engine.StackRangeFull(),
	})
	require.NoError(t, err)
	require.NoError(t, actions.RestackAction(sh.Context, plan, &handlers.NullRestackHandler{}))

	sh.Rebuild()
	require.Equal(t, mainName, sh.Engine.GetBranch("child").GetParent().GetName(),
		"child should reparent to main when the squash scan detects the parent merged")

	cCount, err := sh.Engine.GetCommitCount(sh.Engine.GetBranch("child"))
	require.NoError(t, err)
	require.Equal(t, 1, cCount, "child should keep only its own commit")

	requireCleanWorkingTree(t, sh)
}

// TestSyncCleansUpSquashMergedBranchWithStalePRState covers sync's branch
// cleanup when a PR was squash-merged on GitHub but its local state has not yet
// synced to MERGED (e.g. the merge happened out of band, or a prior sync ran
// offline). Ancestry-based merged detection misses squash merges, and the
// MERGED-state rule never fires, so without aggregate patch-id detection the
// merged branch would linger after sync. With a recorded PR number, cleanup must
// detect the squash and delete it.
func TestSyncCleansUpSquashMergedBranchWithStalePRState(t *testing.T) {
	t.Parallel()
	sh := scenario.NewRemoteScenario(t)
	disableCommitSigning(t, sh)

	// Multi-commit branch so the squash is genuinely combined (no single commit
	// patch-matches the squashed result).
	sh.CreateBranch("feature").
		CommitChange("file-f", "v1").
		CommitChange("file-f", "v1\nv2").
		TrackBranch("feature", "main")

	mainName := sh.Engine.Trunk().GetName()

	// GitHub squash-merges feature: one combined commit lands on main. An
	// unrelated trunk commit ensures a whole-tree comparison would not match.
	sh.Checkout("main")
	sh.CommitChange("file-unrelated", "x")
	sh.CommitChange("file-f", "v1\nv2")

	// PR number is recorded, but its state is stale (still OPEN) — not MERGED.
	markPrWithState(t, sh, "feature", 7, "OPEN", mainName)

	require.NoError(t, sync.Action(sh.Context, sync.Options{Restack: true}, nil))

	branches, err := sh.Scene.Repo.GetLocalBranches()
	require.NoError(t, err)
	require.NotContains(t, branches, "feature",
		"squash-merged branch with a stale PR state should be cleaned up by sync")

	requireCleanWorkingTree(t, sh)
}

// TestSyncKeepsUnmergedBranchWithPRNumber is the guard rail for the cleanup
// above: a branch that carries a PR number but is genuinely NOT merged (it has
// its own unlanded commit) must survive sync. This proves the squash-aware
// deletion rule keys on actual patch equivalence, not merely on PR metadata
// being present.
func TestSyncKeepsUnmergedBranchWithPRNumber(t *testing.T) {
	t.Parallel()
	sh := scenario.NewRemoteScenario(t)
	disableCommitSigning(t, sh)

	sh.CreateBranch("feature").
		CommitChange("file-f", "unique work").
		TrackBranch("feature", "main")

	mainName := sh.Engine.Trunk().GetName()

	// Trunk moves forward with unrelated work; feature's own change never lands.
	sh.Checkout("main")
	sh.CommitChange("file-unrelated", "x")

	markPrWithState(t, sh, "feature", 9, "OPEN", mainName)

	require.NoError(t, sync.Action(sh.Context, sync.Options{Restack: true}, nil))

	branches, err := sh.Scene.Repo.GetLocalBranches()
	require.NoError(t, err)
	require.Contains(t, branches, "feature",
		"an unmerged branch must not be deleted just because it has a PR number")

	cCount, err := sh.Engine.GetCommitCount(sh.Engine.GetBranch("feature"))
	require.NoError(t, err)
	require.Equal(t, 1, cCount, "feature must keep its own unlanded commit")

	requireCleanWorkingTree(t, sh)
}

// TestSquashMergedSiblingKeepsOwnCommit is the exact regression for issue #1345:
// two branches A and B both forked directly off main (siblings, not a stack).
// A is squash-merged on GitHub; after sync + restack, B must keep its own commit
// and stay parented to main. The reported data loss was B's ref being moved onto
// A's stale pre-merge tip, orphaning B's own work. This guards that B is never
// reparented onto, nor reset to, the merged sibling.
func TestSquashMergedSiblingKeepsOwnCommit(t *testing.T) {
	t.Parallel()
	sh := scenario.NewRemoteScenario(t)
	disableCommitSigning(t, sh)

	// A: two commits so the squash is genuinely multi-commit.
	sh.CreateBranch("branch-a").
		CommitChange("file-a", "a1").
		CommitChange("file-a", "a1\na2").
		TrackBranch("branch-a", "main")

	// B: a sibling forked off main, with its own unique commit.
	sh.Checkout("main")
	sh.CreateBranch("branch-b").
		CommitChange("file-b", "b").
		TrackBranch("branch-b", "main")

	mainName := sh.Engine.Trunk().GetName()

	// Capture A's pre-merge tip — the exact SHA B must never be moved onto.
	aStaleTip, err := sh.Engine.GetBranch("branch-a").GetRevision()
	require.NoError(t, err)

	// GitHub squash-merges A: one combined commit lands on main.
	sh.Checkout("main")
	sh.CommitChange("file-a", "a1\na2")
	markPrMerged(t, sh, "branch-a", 1, mainName)

	require.NoError(t, sync.Action(sh.Context, sync.Options{Restack: true}, nil))

	// A is cleaned up; B survives.
	branches, err := sh.Scene.Repo.GetLocalBranches()
	require.NoError(t, err)
	require.NotContains(t, branches, "branch-a", "squash-merged sibling should be deleted")
	require.Contains(t, branches, "branch-b")

	// B stays parented to main, never to the merged sibling.
	require.Equal(t, mainName, sh.Engine.GetBranch("branch-b").GetParent().GetName(),
		"sibling B must remain parented to main, not reparented onto merged A")

	// B's ref must not have been moved onto A's stale tip.
	bTip, err := sh.Engine.GetBranch("branch-b").GetRevision()
	require.NoError(t, err)
	require.NotEqual(t, aStaleTip, bTip,
		"sibling B must never be reset to A's stale pre-merge tip (issue #1345 data loss)")

	// B keeps exactly its own commit on top of main.
	cCount, err := sh.Engine.GetCommitCount(sh.Engine.GetBranch("branch-b"))
	require.NoError(t, err)
	require.Equal(t, 1, cCount, "sibling B must keep only its own commit")

	requireCleanWorkingTree(t, sh)
}

// TestRestackAfterMultiCommitSquashWithoutSync covers running `stackit restack`
// BEFORE sync has cleaned up the squash-merged parent. The parent branch still
// exists locally with a MERGED PR state; restack itself does the reparenting
// (restack_impl.go shouldReparentBranch path), not sync's clean_branches path.
func TestRestackAfterMultiCommitSquashWithoutSync(t *testing.T) {
	t.Parallel()
	sh := scenario.NewRemoteScenario(t)
	disableCommitSigning(t, sh)

	sh.CreateBranch("parent").
		CommitChange("file-p", "v1").
		CommitChange("file-p", "v1\nv2").
		CommitChange("file-p", "v1\nv2\nv3").
		TrackBranch("parent", "main")

	sh.CreateBranch("child").
		CommitChange("file-c", "c").
		TrackBranch("child", "parent")

	// GitHub squash-merges parent's PR: one combined commit lands on main.
	mainName := sh.Engine.Trunk().GetName()
	sh.Checkout("main")
	sh.CommitChange("file-p", "v1\nv2\nv3")
	markPrMerged(t, sh, "parent", 1, mainName)
	// The merge landed on the real trunk and was fetched, so origin/main matches.
	sh.SyncRemoteTrunkToLocal()

	// User runs `stackit restack` from the child without syncing first.
	sh.Checkout("child")
	plan, err := actions.PlanRestack(sh.Context, actions.RestackOptions{
		BranchName: "child",
		Scope:      engine.StackRangeFull(),
	})
	require.NoError(t, err)
	require.NoError(t, actions.RestackAction(sh.Context, plan, &handlers.NullRestackHandler{}))

	sh.Rebuild()
	require.Equal(t, mainName, sh.Engine.GetBranch("child").GetParent().GetName(),
		"child should reparent to main past the merged parent")

	// Child must keep ONLY its own commit — parent's 3 commits must not replay.
	cCount, err := sh.Engine.GetCommitCount(sh.Engine.GetBranch("child"))
	require.NoError(t, err)
	require.Equal(t, 1, cCount, "child should not inherit parent's squashed commits")

	requireCleanWorkingTree(t, sh)
}
