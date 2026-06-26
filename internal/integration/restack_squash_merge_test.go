package integration

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/actions/sync"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/handlers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

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

// TestRestackDetectsSquashMergeWithoutPRState covers running `stackit restack`
// when the parent was squash-merged but no PR state has been recorded (e.g.
// the branch was merged directly without a PR, or st sync has not run yet).
// Aggregate patch-id comparison in IsMerged must detect the squash and cause
// the child to reparent to trunk without replaying the parent's commits.
func TestRestackDetectsSquashMergeWithoutPRState(t *testing.T) {
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

	// Squash-merge parent onto main WITHOUT recording PR state.
	// This simulates a branch merged via the GitHub UI before st sync has
	// had a chance to mark the PR as MERGED in local metadata.
	mainName := sh.Engine.Trunk().GetName()
	sh.Checkout("main")
	sh.CommitChange("file-main", "unrelated")
	sh.CommitChange("file-p", "v1\nv2")

	// Intentionally skip markPrMerged — IsMerged must detect the squash from
	// Git history. The unrelated trunk commit means a whole-tree comparison
	// would not match the parent tip.
	sh.Checkout("child")
	plan, err := actions.PlanRestack(sh.Context, actions.RestackOptions{
		BranchName: "child",
		Scope:      engine.StackRangeFull(),
	})
	require.NoError(t, err)
	require.NoError(t, actions.RestackAction(sh.Context, plan, &handlers.NullRestackHandler{}))

	sh.Rebuild()
	require.Equal(t, mainName, sh.Engine.GetBranch("child").GetParent().GetName(),
		"child should reparent to main when IsMerged detects squash via aggregate patch-id")

	cCount, err := sh.Engine.GetCommitCount(sh.Engine.GetBranch("child"))
	require.NoError(t, err)
	require.Equal(t, 1, cCount, "child should keep only its own commit")

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
