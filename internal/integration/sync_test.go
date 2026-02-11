package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions/sync"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

const prStateMerged = "MERGED"

func TestSync(t *testing.T) {
	t.Parallel()
	t.Run("local parent is authoritative over GitHub PR base", func(t *testing.T) {
		t.Parallel()
		sh := scenario.NewRemoteScenario(t)

		// 1. Create a diamond-like structure:
		// main -> feature-a -> feature-b
		sh.CreateBranch("feature-a").
			CommitChange("file-a", "content-a").
			TrackBranch("feature-a", "main")

		sh.CreateBranch("feature-b").
			CommitChange("file-b", "content-b").
			TrackBranch("feature-b", "feature-a")

		eng := sh.Engine
		mainBranchName := eng.Trunk().GetName()

		// Verify initial parent of feature-b is feature-a
		branchB := eng.GetBranch("feature-b")
		require.Equal(t, "feature-a", branchB.GetParent().GetName())

		// 2. Simulate GitHub PR metadata for feature-b pointing to main instead of feature-a
		// This simulates someone manually changing the PR base on GitHub
		t.Log("Simulating changed PR base on GitHub...")
		meta, err := eng.Git().ReadMetadata("feature-b")
		require.NoError(t, err)

		prInfo := meta.GetPrInfo()
		if prInfo == nil {
			prInfo = &git.PrInfoPersistence{}
		}
		newBase := mainBranchName
		prInfo.Base = &newBase
		meta = meta.WithPrInfo(prInfo)

		err = eng.Git().WriteMetadata("feature-b", meta)
		require.NoError(t, err)

		// 3. Run sync
		// Local parent is authoritative - GitHub PR base does NOT override local parent
		err = sync.Action(sh.Context, sync.Options{}, nil)
		require.NoError(t, err)

		// 4. Verify local parent of feature-b is STILL feature-a (not changed to match GitHub)
		// Local metadata is authoritative; if a GitHub client were available,
		// sync would push the local parent to GitHub (not pull from GitHub)
		branchBAfter := eng.GetBranch("feature-b")
		require.Equal(t, "feature-a", branchBAfter.GetParent().GetName(), "Local parent should remain authoritative and not be changed by GitHub PR base")
	})

	t.Run("handles consolidation and deletion of merged branches", func(t *testing.T) {
		t.Parallel()
		sh := scenario.NewRemoteScenario(t)

		// 1. Create a chain of branches: main -> branch-a -> branch-b -> branch-c
		branchNames := []string{"branch-a", "branch-b", "branch-c"}
		currentParent := "main"
		for _, name := range branchNames {
			sh.CreateBranch(name).
				CommitChange(name+"-file", "content").
				TrackBranch(name, currentParent)
			currentParent = name
		}

		eng := sh.Engine
		mainBranchName := eng.Trunk().GetName()

		// Batch read metadata for all individual branches at once
		metas, readErrs := eng.Git().BatchReadMetadataForBranches(branchNames)
		for branch, readErr := range readErrs {
			require.NoError(t, readErr, "failed to read metadata for %s", branch)
		}

		// Mark individual branches as merged
		for i, branch := range branchNames {
			meta := metas[branch]
			if meta == nil {
				meta = git.NewMeta()
			}
			prInfo := meta.GetPrInfo()
			if prInfo == nil {
				prInfo = &git.PrInfoPersistence{}
			}
			prNum := i + 1
			state := prStateMerged
			base := mainBranchName
			prInfo.Number = &prNum
			prInfo.State = &state
			prInfo.Base = &base
			meta = meta.WithPrInfo(prInfo)
			err := eng.Git().WriteMetadata(branch, meta)
			require.NoError(t, err)
		}

		// 2. Create a "merge" branch representing a Squash & Merge of the whole stack
		// This branch will have the same content as branch-c but only one commit.
		mergeBranch := "merged-feature"
		sh.Checkout("main").
			CreateBranch(mergeBranch).
			CommitChange("merge-file", "merged content")

		// Mark merge branch as merged
		meta, err := eng.Git().ReadMetadata(mergeBranch)
		require.NoError(t, err)
		prNum := 100
		state := "CLOSED"
		base := mainBranchName
		meta = meta.WithPrInfo(&git.PrInfoPersistence{
			Number: &prNum,
			State:  &state,
			Base:   &base,
		})
		err = eng.Git().WriteMetadata(mergeBranch, meta)
		require.NoError(t, err)

		// 3. Run sync (which should call clean_branches)
		err = sync.Action(sh.Context, sync.Options{}, nil)
		require.NoError(t, err)

		// 4. Verify all merged branches are deleted
		allLocalBranches, _ := sh.Scene.Repo.GetLocalBranches()
		for _, name := range branchNames {
			require.NotContains(t, allLocalBranches, name, "Merged branch %s should have been deleted", name)
		}
		require.NotContains(t, allLocalBranches, mergeBranch, "Merged merge branch should have been deleted")
	})

	t.Run("handles diamond dependency during sync", func(t *testing.T) {
		t.Parallel()
		sh := scenario.NewRemoteScenario(t)

		// Create: main -> a -> b
		//                \-> c
		sh.CreateBranch("a").CommitChange("a", "a").TrackBranch("a", "main")
		sh.CreateBranch("b").CommitChange("b", "b").TrackBranch("b", "a")
		sh.Checkout("main")
		sh.CreateBranch("c").CommitChange("c", "c").TrackBranch("c", "a")

		// Update GitHub info: 'a' is merged, 'b' is open pointing to 'main', 'c' is open pointing to 'a'
		eng := sh.Engine
		mainBranchName := eng.Trunk().GetName()

		// a: MERGED into main
		metaA, _ := eng.Git().ReadMetadata("a")
		prNumA := 1
		stateA := prStateMerged
		baseA := mainBranchName
		metaA = metaA.WithPrInfo(&git.PrInfoPersistence{
			Number: &prNumA,
			State:  &stateA,
			Base:   &baseA,
		})
		_ = eng.Git().WriteMetadata("a", metaA)

		// b: OPEN pointing to main
		metaB, _ := eng.Git().ReadMetadata("b")
		prNumB := 2
		stateB := "OPEN"
		baseB := mainBranchName
		metaB = metaB.WithPrInfo(&git.PrInfoPersistence{
			Number: &prNumB,
			State:  &stateB,
			Base:   &baseB,
		})
		_ = eng.Git().WriteMetadata("b", metaB)

		// 1. Run sync
		err := sync.Action(sh.Context, sync.Options{}, nil)
		require.NoError(t, err)

		// 2. Verify 'a' is deleted
		allLocalBranches, _ := sh.Scene.Repo.GetLocalBranches()
		require.NotContains(t, allLocalBranches, "a")

		// 3. Verify 'b' is reparented to 'main' (because GitHub says so)
		require.Equal(t, "main", eng.GetBranch("b").GetParent().GetName())

		// 4. Verify 'c' is reparented to 'main' (because 'a' was deleted)
		require.Equal(t, "main", eng.GetBranch("c").GetParent().GetName())
	})
}

func TestSyncDraftPRs(t *testing.T) {
	t.Parallel()
	sh := scenario.NewRemoteScenario(t)

	// Create branch-a on main
	sh.CreateBranch("branch-a").
		CommitChange("file-a", "content-a").
		TrackBranch("branch-a", "main")

	eng := sh.Engine
	mainBranchName := eng.Trunk().GetName()

	// 1. Simulate GitHub PR metadata for branch-a being a DRAFT
	t.Log("Simulating DRAFT PR on GitHub...")
	meta, err := eng.Git().ReadMetadata("branch-a")
	require.NoError(t, err)

	prNum := 1
	state := "OPEN"
	base := mainBranchName
	isDraft := true
	meta = meta.WithPrInfo(&git.PrInfoPersistence{
		Number:  &prNum,
		State:   &state,
		Base:    &base,
		IsDraft: &isDraft,
	})

	err = eng.Git().WriteMetadata("branch-a", meta)
	require.NoError(t, err)

	// 2. Verify it's a draft locally
	branch := eng.GetBranch("branch-a")
	prInfo, _ := branch.GetPrInfo()
	require.True(t, prInfo.IsDraft())

	// 3. Run sync
	err = sync.Action(sh.Context, sync.Options{}, nil)
	require.NoError(t, err)

	// 4. Verify branch still exists (drafts shouldn't be deleted)
	allLocalBranches, _ := sh.Scene.Repo.GetLocalBranches()
	require.Contains(t, allLocalBranches, "branch-a")
}

func TestSyncCleanupDiamond(t *testing.T) {
	t.Parallel()
	sh := scenario.NewRemoteScenario(t)

	// Create: main -> a -> b
	//                \-> c
	sh.CreateBranch("a").CommitChange("a", "a").TrackBranch("a", "main")
	sh.CreateBranch("b").CommitChange("b", "b").TrackBranch("b", "a")
	sh.Checkout("a")
	sh.CreateBranch("c").CommitChange("c", "c").TrackBranch("c", "a")

	eng := sh.Engine
	mainBranchName := eng.Trunk().GetName()

	// Mark 'a' as merged
	metaA, _ := eng.Git().ReadMetadata("a")
	prNum := 1
	state := prStateMerged
	base := mainBranchName
	metaA = metaA.WithPrInfo(&git.PrInfoPersistence{
		Number: &prNum,
		State:  &state,
		Base:   &base,
	})
	_ = eng.Git().WriteMetadata("a", metaA)

	// Mark 'b' as merged
	metaB, _ := eng.Git().ReadMetadata("b")
	prNumB := 2
	stateB := "MERGED"
	baseB := mainBranchName
	metaB = metaB.WithPrInfo(&git.PrInfoPersistence{
		Number: &prNumB,
		State:  &stateB,
		Base:   &baseB,
	})
	_ = eng.Git().WriteMetadata("b", metaB)

	// Run sync
	err := sync.Action(sh.Context, sync.Options{}, nil)
	require.NoError(t, err)

	// Verify 'a' and 'b' are deleted, but 'c' remains (not merged)
	allLocalBranches, _ := sh.Scene.Repo.GetLocalBranches()
	require.NotContains(t, allLocalBranches, "a")
	require.NotContains(t, allLocalBranches, "b")
	require.Contains(t, allLocalBranches, "c")

	// Verify 'c' is reparented to 'main'
	require.Equal(t, "main", eng.GetBranch("c").GetParent().GetName())
}

func TestSyncStaleDraftCleanup(t *testing.T) {
	t.Parallel()
	// Tests the fix for the "stale draft" bug where empty branches
	// weren't being cleaned up if they were ancestors of other branches.
	sh := scenario.NewRemoteScenario(t)

	// Create: main -> a -> b
	sh.CreateBranch("a").CommitChange("a", "a").TrackBranch("a", "main")
	sh.CreateBranch("b").CommitChange("b", "b").TrackBranch("b", "a")

	// Now 'a' is merged into main (e.g. via squash and merge)
	// and 'b' is NOT merged yet.
	sh.Checkout("main")
	sh.CommitChange("a", "a") // Simulate squash-merge of 'a'

	eng := sh.Engine
	mainBranchName := eng.Trunk().GetName()

	// Mark 'a' as merged in metadata
	metaA, _ := eng.Git().ReadMetadata("a")
	prNum := 1
	state := prStateMerged
	base := mainBranchName
	metaA = metaA.WithPrInfo(&git.PrInfoPersistence{
		Number: &prNum,
		State:  &state,
		Base:   &base,
	})
	_ = eng.Git().WriteMetadata("a", metaA)

	// 'a' is now empty relative to main
	isEmpty, _ := eng.IsBranchEmpty(sh.Context.Context, "a")
	require.True(t, isEmpty)

	// Run sync
	err := sync.Action(sh.Context, sync.Options{}, nil)
	require.NoError(t, err)

	// 'a' should be deleted because it's merged and empty
	allLocalBranches, _ := sh.Scene.Repo.GetLocalBranches()
	require.NotContains(t, allLocalBranches, "a")

	// 'b' should be reparented to main
	require.Equal(t, "main", eng.GetBranch("b").GetParent().GetName())
}

func TestSyncSquashMergedRootPreservesChildCommitBoundaries(t *testing.T) {
	t.Parallel()
	sh := scenario.NewRemoteScenario(t)

	// Create stack: main -> A(2 commits) -> B -> C
	sh.CreateBranch("branch-a").
		CommitChange("shared.txt", "a-v1").
		CommitChange("shared.txt", "a-v2").
		TrackBranch("branch-a", "main")

	sh.CreateBranch("branch-b").
		CommitChange("child-b.txt", "b-change").
		TrackBranch("branch-b", "branch-a")

	sh.CreateBranch("branch-c").
		CommitChange("child-c.txt", "c-change").
		TrackBranch("branch-c", "branch-b")

	eng := sh.Engine
	mainBranchName := eng.Trunk().GetName()

	// Simulate squash merge of branch-a: apply final A tree state on main as one commit.
	sh.Checkout("main")
	sh.CommitChange("shared.txt", "a-v2")

	// Mark branch-a PR as merged so sync cleanup deletes it.
	metaA, err := eng.Git().ReadMetadata("branch-a")
	require.NoError(t, err)
	prNum := 1
	state := prStateMerged
	base := mainBranchName
	metaA = metaA.WithPrInfo(&git.PrInfoPersistence{
		Number: &prNum,
		State:  &state,
		Base:   &base,
	})
	err = eng.Git().WriteMetadata("branch-a", metaA)
	require.NoError(t, err)

	// Run sync+restack from main.
	sh.Checkout("main")
	err = sync.Action(sh.Context, sync.Options{Restack: true}, nil)
	require.NoError(t, err)

	allLocalBranches, err := sh.Scene.Repo.GetLocalBranches()
	require.NoError(t, err)
	require.NotContains(t, allLocalBranches, "branch-a")

	require.Equal(t, "main", eng.GetBranch("branch-b").GetParent().GetName())
	require.Equal(t, "branch-b", eng.GetBranch("branch-c").GetParent().GetName())

	// Core regression assertions: B and C keep their own commit boundaries.
	// If A commits were replayed into B/C, these counts would be inflated.
	bCount, err := eng.GetCommitCount(eng.GetBranch("branch-b"))
	require.NoError(t, err)
	require.Equal(t, 1, bCount)

	cCount, err := eng.GetCommitCount(eng.GetBranch("branch-c"))
	require.NoError(t, err)
	require.Equal(t, 1, cCount)

	sh.ExpectBranchFixed("branch-b").
		ExpectBranchFixed("branch-c")
}

// TestSyncDoesNotLeaveIndexState verifies that after sync cleans up merged branches
// and restacks remaining branches, the working directory is left in a clean state
// with no staged changes. This is a regression test for the bug where the Git index
// would retain stale state after multiple rebase operations during RestackBranches.
func TestSyncDoesNotLeaveIndexState(t *testing.T) {
	t.Parallel()
	t.Run("sync from main does not leave staged changes after cleanup and restack", func(t *testing.T) {
		t.Parallel()
		sh := scenario.NewRemoteScenario(t)

		// 1. Create a chain: main -> branch-a -> branch-b
		sh.CreateBranch("branch-a").
			CommitChange("file-a", "content-a").
			TrackBranch("branch-a", "main")

		sh.CreateBranch("branch-b").
			CommitChange("file-b", "content-b").
			TrackBranch("branch-b", "branch-a")

		eng := sh.Engine
		mainBranchName := eng.Trunk().GetName()

		// 2. Simulate squash-merge: advance main with the same content as branch-a
		// This is what happens when a PR is squash-merged on GitHub
		sh.Checkout("main").
			CommitChange("file-a", "content-a")

		// 3. Mark branch-a as merged
		metaA, err := eng.Git().ReadMetadata("branch-a")
		require.NoError(t, err)
		prNum := 1
		state := prStateMerged
		base := mainBranchName
		metaA = metaA.WithPrInfo(&git.PrInfoPersistence{
			Number: &prNum,
			State:  &state,
			Base:   &base,
		})
		err = eng.Git().WriteMetadata("branch-a", metaA)
		require.NoError(t, err)

		// 4. Stay on main (user's exact scenario)
		sh.Checkout("main")

		// 5. Run sync with restack (this will delete branch-a and restack branch-b onto new main)
		err = sync.Action(sh.Context, sync.Options{Restack: true}, nil)
		require.NoError(t, err)

		// 6. CRITICAL: Verify main has no staged or unstaged changes after sync
		// This is the core assertion that catches the bug
		requireCleanWorkingTree(t, sh)

		// 7. Verify branch-a was deleted (cleanup occurred)
		allLocalBranches, err := sh.Scene.Repo.GetLocalBranches()
		require.NoError(t, err)
		require.NotContains(t, allLocalBranches, "branch-a", "branch-a should have been deleted")

		// 8. Verify branch-b was reparented to main
		branchB := eng.GetBranch("branch-b")
		require.NotNil(t, branchB.GetParent())
		require.Equal(t, mainBranchName, branchB.GetParent().GetName(), "branch-b should be reparented to main")
	})

	t.Run("sync from detached HEAD does not leave staged changes", func(t *testing.T) {
		t.Parallel()
		sh := scenario.NewRemoteScenario(t)

		// Create a simple stack
		sh.CreateBranch("feature").
			CommitChange("feature.txt", "feature content").
			TrackBranch("feature", "main")

		eng := sh.Engine
		mainBranchName := eng.Trunk().GetName()

		// Mark feature as merged
		meta, err := eng.Git().ReadMetadata("feature")
		require.NoError(t, err)
		prNum := 1
		state := prStateMerged
		base := mainBranchName
		meta = meta.WithPrInfo(&git.PrInfoPersistence{
			Number: &prNum,
			State:  &state,
			Base:   &base,
		})
		err = eng.Git().WriteMetadata("feature", meta)
		require.NoError(t, err)

		// Detach HEAD at main
		mainRev, err := sh.Scene.Repo.RunGitCommandAndGetOutput("rev-parse", "main")
		require.NoError(t, err)
		err = sh.Scene.Repo.RunGitCommand("checkout", "--detach", strings.TrimSpace(mainRev))
		require.NoError(t, err)

		// Run sync
		err = sync.Action(sh.Context, sync.Options{Restack: true}, nil)
		require.NoError(t, err)

		// Verify clean status
		requireCleanWorkingTree(t, sh)
	})

	t.Run("sync with rebase that moves commits does not leave staged changes", func(t *testing.T) {
		t.Parallel()
		// This test creates a scenario where the rebase actually moves commits,
		// which is more likely to trigger index state issues.
		sh := scenario.NewRemoteScenario(t)

		// 1. Create initial stack: main -> feature -> child
		sh.CreateBranch("feature").
			CommitChange("feature.txt", "feature v1").
			TrackBranch("feature", "main")

		sh.CreateBranch("child").
			CommitChange("child.txt", "child content").
			TrackBranch("child", "feature")

		eng := sh.Engine
		mainBranchName := eng.Trunk().GetName()

		// 2. Go back to main and add a new commit (simulating trunk advancing)
		sh.Checkout("main").
			CommitChange("main-new.txt", "new content on main")

		// 3. Mark feature as merged (simulating squash-merge on GitHub)
		// Also simulate main having the feature's content
		sh.CommitChange("feature.txt", "feature v1")

		metaF, err := eng.Git().ReadMetadata("feature")
		require.NoError(t, err)
		prNum := 1
		state := prStateMerged
		base := mainBranchName
		metaF = metaF.WithPrInfo(&git.PrInfoPersistence{
			Number: &prNum,
			State:  &state,
			Base:   &base,
		})
		err = eng.Git().WriteMetadata("feature", metaF)
		require.NoError(t, err)

		// 4. Stay on main and run sync
		// This should:
		// - Delete feature (merged)
		// - Reparent child to main
		// - Restack child onto the new main (this requires actual commit movement)
		err = sync.Action(sh.Context, sync.Options{Restack: true}, nil)
		require.NoError(t, err)

		// 5. Verify main has clean status
		requireCleanWorkingTree(t, sh)

		// 6. Verify feature was deleted
		allLocalBranches, err := sh.Scene.Repo.GetLocalBranches()
		require.NoError(t, err)
		require.NotContains(t, allLocalBranches, "feature")

		// 7. Verify child was reparented to main
		childBranch := eng.GetBranch("child")
		require.NotNil(t, childBranch.GetParent())
		require.Equal(t, mainBranchName, childBranch.GetParent().GetName())
	})

	t.Run("sync skips deletion of branch with unpushed commits", func(t *testing.T) {
		t.Parallel()
		sh := scenario.NewRemoteScenario(t)

		// Create a branch and track it
		sh.CreateBranch("feature").
			CommitChange("feature-file", "content").
			TrackBranch("feature", "main")

		eng := sh.Engine
		mainBranchName := eng.Trunk().GetName()

		// Push the branch once so the extra commit below is detected as unpushed.
		sh.Checkout("feature")
		err := sh.Scene.Repo.PushBranch("origin", "feature")
		require.NoError(t, err)

		// Add an unpushed local commit
		sh.CommitChange("extra.txt", "unpushed work")

		// Mark feature as merged via PR metadata
		meta, err := eng.Git().ReadMetadata("feature")
		require.NoError(t, err)
		prNum := 1
		state := prStateMerged
		base := mainBranchName
		meta = meta.WithPrInfo(&git.PrInfoPersistence{
			Number: &prNum,
			State:  &state,
			Base:   &base,
		})
		err = eng.Git().WriteMetadata("feature", meta)
		require.NoError(t, err)

		// Go back to main for sync
		sh.Checkout("main")

		// Run sync (non-interactive - should skip unpushed branches)
		err = sync.Action(sh.Context, sync.Options{}, nil)
		require.NoError(t, err)

		// Feature branch should NOT be deleted because it has unpushed commits
		allLocalBranches, err := sh.Scene.Repo.GetLocalBranches()
		require.NoError(t, err)
		require.Contains(t, allLocalBranches, "feature",
			"branch with unpushed changes should not be deleted during sync")

		// A warning should have been emitted (check output buffer)
		require.Contains(t, sh.Output.String(), "unpushed",
			"sync should warn about branches with unpushed changes")
	})
}

// disableCommitSigning stops the test repo from inheriting commit.gpgsign=true
// from a global config. The engine's restack invokes `git commit` without
// stripping global config, so any signing requirement from the host env
// breaks rebase. Local config wins over global, so this is safe in CI too.
func disableCommitSigning(t *testing.T, sh *scenario.Scenario) {
	t.Helper()
	require.NoError(t, sh.Scene.Repo.RunGitCommand("config", "commit.gpgsign", "false"))
	require.NoError(t, sh.Scene.Repo.RunGitCommand("config", "tag.gpgsign", "false"))
}

// setupTwoBranchSquashScenario creates main->branch-a->branch-b, simulates a squash-merge
// of branch-a onto main, and marks it MERGED. Returns the trunk name.
// Used by the 8 TestSquashMerge* and TestUserDeleted/Rebased tests.
func setupTwoBranchSquashScenario(t *testing.T, sh *scenario.Scenario) string {
	t.Helper()
	sh.CreateBranch("branch-a").
		CommitChange("file-a", "a").
		TrackBranch("branch-a", "main")
	sh.CreateBranch("branch-b").
		CommitChange("file-b", "b").
		TrackBranch("branch-b", "branch-a")
	mainName := sh.Engine.Trunk().GetName()
	sh.Checkout("main")
	sh.CommitChange("file-a", "a")
	markPrMerged(t, sh, "branch-a", 1, mainName)
	return mainName
}

// requireCleanWorkingTree asserts that the git index and working tree are clean.
func requireCleanWorkingTree(t *testing.T, sh *scenario.Scenario) {
	t.Helper()
	out, err := sh.Scene.Repo.RunGitCommandAndGetOutput("status", "--porcelain")
	require.NoError(t, err)
	require.Empty(t, strings.TrimSpace(out), "expected clean working tree")
}

// markPrMerged sets the PR metadata for branch to MERGED with the given base.
// Mirrors the inline pattern used elsewhere in sync_test.go.
func markPrMerged(t *testing.T, sh *scenario.Scenario, branch string, prNumber int, base string) {
	t.Helper()
	meta, err := sh.Engine.Git().ReadMetadata(branch)
	require.NoError(t, err)
	num := prNumber
	state := prStateMerged
	b := base
	meta = meta.WithPrInfo(&git.PrInfoPersistence{
		Number: &num,
		State:  &state,
		Base:   &b,
	})
	require.NoError(t, sh.Engine.Git().WriteMetadata(branch, meta))
}

// TestSquashMergeMiddleOfStack covers the case where a middle PR (B) is
// squash-merged on GitHub while its parent (A) is still open. The squash
// commit lands on A (the PR base), not on trunk. C must reparent to A,
// not skip over to trunk.
func TestSquashMergeMiddleOfStack(t *testing.T) {
	t.Parallel()
	sh := scenario.NewRemoteScenario(t)
	disableCommitSigning(t, sh)

	// main -> A -> B -> C
	sh.CreateBranch("branch-a").
		CommitChange("file-a", "a").
		TrackBranch("branch-a", "main")
	sh.CreateBranch("branch-b").
		CommitChange("file-b", "b").
		TrackBranch("branch-b", "branch-a")
	sh.CreateBranch("branch-c").
		CommitChange("file-c", "c").
		TrackBranch("branch-c", "branch-b")

	// GitHub squash-merges B's PR (base=A). The squash commit lands on A.
	sh.Checkout("branch-a")
	sh.CommitChange("file-b", "b")
	markPrMerged(t, sh, "branch-b", 2, "branch-a")

	sh.Checkout("main")
	require.NoError(t, sync.Action(sh.Context, sync.Options{Restack: true}, nil))

	branches, err := sh.Scene.Repo.GetLocalBranches()
	require.NoError(t, err)
	require.NotContains(t, branches, "branch-b", "merged middle branch should be deleted")
	require.Contains(t, branches, "branch-a", "open parent should remain")
	require.Contains(t, branches, "branch-c", "open child should remain")

	// C must hop to A (B's parent), not skip to main.
	require.Equal(t, "branch-a", sh.Engine.GetBranch("branch-c").GetParent().GetName(),
		"C should reparent to A, not main")
	require.Equal(t, "main", sh.Engine.GetBranch("branch-a").GetParent().GetName())

	// C keeps its own commits — A's squash content shouldn't replay into C.
	cCount, err := sh.Engine.GetCommitCount(sh.Engine.GetBranch("branch-c"))
	require.NoError(t, err)
	require.Equal(t, 1, cCount)
}

// TestSquashMergeMultipleAdjacentMergedInOneSync covers when both A and B
// were squash-merged on GitHub between syncs. C must reparent across both
// deletions to main in a single pass.
func TestSquashMergeMultipleAdjacentMergedInOneSync(t *testing.T) {
	t.Parallel()
	sh := scenario.NewRemoteScenario(t)
	disableCommitSigning(t, sh)

	sh.CreateBranch("branch-a").
		CommitChange("file-a", "a").
		TrackBranch("branch-a", "main")
	sh.CreateBranch("branch-b").
		CommitChange("file-b", "b").
		TrackBranch("branch-b", "branch-a")
	sh.CreateBranch("branch-c").
		CommitChange("file-c", "c").
		TrackBranch("branch-c", "branch-b")

	// Squash A and then B onto trunk (sequential GitHub merges).
	mainName := sh.Engine.Trunk().GetName()
	sh.Checkout("main")
	sh.CommitChange("file-a", "a")
	sh.CommitChange("file-b", "b")

	markPrMerged(t, sh, "branch-a", 1, mainName)
	markPrMerged(t, sh, "branch-b", 2, mainName)

	require.NoError(t, sync.Action(sh.Context, sync.Options{Restack: true}, nil))

	branches, err := sh.Scene.Repo.GetLocalBranches()
	require.NoError(t, err)
	require.NotContains(t, branches, "branch-a")
	require.NotContains(t, branches, "branch-b")
	require.Contains(t, branches, "branch-c")

	// findNonDeletingAncestor must walk past both deletions to main.
	require.Equal(t, mainName, sh.Engine.GetBranch("branch-c").GetParent().GetName())

	// C should not have inherited A's or B's commits during restack.
	cCount, err := sh.Engine.GetCommitCount(sh.Engine.GetBranch("branch-c"))
	require.NoError(t, err)
	require.Equal(t, 1, cCount)

	requireCleanWorkingTree(t, sh)
}

// TestSquashMergeDiamondAllChildrenMerged covers a diamond where A is open
// and both children (B and C) get squash-merged on GitHub. Both children
// should be cleaned up; A is unaffected.
func TestSquashMergeDiamondAllChildrenMerged(t *testing.T) {
	t.Parallel()
	sh := scenario.NewRemoteScenario(t)
	disableCommitSigning(t, sh)

	// main -> A -> {B, C}
	sh.CreateBranch("branch-a").
		CommitChange("file-a", "a").
		TrackBranch("branch-a", "main")
	sh.CreateBranch("branch-b").
		CommitChange("file-b", "b").
		TrackBranch("branch-b", "branch-a")
	sh.Checkout("branch-a")
	sh.CreateBranch("branch-c").
		CommitChange("file-c", "c").
		TrackBranch("branch-c", "branch-a")

	// Both B and C squash-merged into A.
	sh.Checkout("branch-a")
	sh.CommitChange("file-b", "b")
	sh.CommitChange("file-c", "c")
	markPrMerged(t, sh, "branch-b", 2, "branch-a")
	markPrMerged(t, sh, "branch-c", 3, "branch-a")

	sh.Checkout("main")
	require.NoError(t, sync.Action(sh.Context, sync.Options{Restack: true}, nil))

	branches, err := sh.Scene.Repo.GetLocalBranches()
	require.NoError(t, err)
	require.NotContains(t, branches, "branch-b")
	require.NotContains(t, branches, "branch-c")
	require.Contains(t, branches, "branch-a", "open parent should survive losing both children")

	require.Equal(t, "main", sh.Engine.GetBranch("branch-a").GetParent().GetName())
}

// TestSquashMergeSyncWhileOnMergedBranch is a safety-invariants test:
// when the user runs sync while checked out on the doomed branch, sync
// must move HEAD off the branch before deletion, not leave detached HEAD.
// Per .claude/rules/safety-invariants.md "No Detached HEAD State".
func TestSquashMergeSyncWhileOnMergedBranch(t *testing.T) {
	t.Parallel()
	sh := scenario.NewRemoteScenario(t)
	disableCommitSigning(t, sh)

	setupTwoBranchSquashScenario(t, sh)

	// User is sitting on the about-to-be-deleted branch when they sync.
	sh.Checkout("branch-a")
	require.NoError(t, sync.Action(sh.Context, sync.Options{Restack: true}, nil))

	branches, err := sh.Scene.Repo.GetLocalBranches()
	require.NoError(t, err)
	require.NotContains(t, branches, "branch-a")
	require.Contains(t, branches, "branch-b")

	// HEAD must point at a branch — not detached.
	headRef, err := sh.Scene.Repo.RunGitCommandAndGetOutput("symbolic-ref", "--quiet", "HEAD")
	require.NoError(t, err, "HEAD must be a symbolic ref (not detached) after sync deletes the checked-out branch")
	require.NotEmpty(t, strings.TrimSpace(headRef))

	requireCleanWorkingTree(t, sh)
}

// TestSquashMergeSyncWhileOnChildOfMergedBranch verifies that when the
// current branch is reparented mid-sync, HEAD continues to track that
// branch (now at its restacked commit) — the user does not get yanked
// onto trunk.
func TestSquashMergeSyncWhileOnChildOfMergedBranch(t *testing.T) {
	t.Parallel()
	sh := scenario.NewRemoteScenario(t)
	disableCommitSigning(t, sh)

	mainName := setupTwoBranchSquashScenario(t, sh)

	// User is on the surviving child while syncing.
	sh.Checkout("branch-b")
	require.NoError(t, sync.Action(sh.Context, sync.Options{Restack: true}, nil))

	branches, err := sh.Scene.Repo.GetLocalBranches()
	require.NoError(t, err)
	require.NotContains(t, branches, "branch-a")
	require.Contains(t, branches, "branch-b")

	require.Equal(t, mainName, sh.Engine.GetBranch("branch-b").GetParent().GetName())

	// User stays on branch-b even though its parent changed under it.
	headRef, err := sh.Scene.Repo.RunGitCommandAndGetOutput("symbolic-ref", "--short", "HEAD")
	require.NoError(t, err)
	require.Equal(t, "branch-b", strings.TrimSpace(headRef))

	bCount, err := sh.Engine.GetCommitCount(sh.Engine.GetBranch("branch-b"))
	require.NoError(t, err)
	require.Equal(t, 1, bCount, "B should keep its single commit after restack onto trunk")

	requireCleanWorkingTree(t, sh)
}

// TestSquashMergeNoRestackLeavesGitRefsUntouched verifies that
// `--no-restack` still cleans up merged branches and updates metadata
// parents, but does NOT advance the children's git refs. This documents
// the partial-state contract for users who pass --no-restack expecting
// only cleanup.
func TestSquashMergeNoRestackLeavesGitRefsUntouched(t *testing.T) {
	t.Parallel()
	sh := scenario.NewRemoteScenario(t)
	disableCommitSigning(t, sh)

	mainName := setupTwoBranchSquashScenario(t, sh)

	bSHAbeforeSync, err := sh.Scene.Repo.RunGitCommandAndGetOutput("rev-parse", "branch-b")
	require.NoError(t, err)

	require.NoError(t, sync.Action(sh.Context, sync.Options{NoRestack: true}, nil))

	branches, err := sh.Scene.Repo.GetLocalBranches()
	require.NoError(t, err)
	require.NotContains(t, branches, "branch-a", "cleanup should still run with --no-restack")
	require.Contains(t, branches, "branch-b")

	// Metadata parent updated to main even though restack was skipped.
	require.Equal(t, mainName, sh.Engine.GetBranch("branch-b").GetParent().GetName())

	// Git ref untouched: branch-b still points at its original commit.
	bSHAafterSync, err := sh.Scene.Repo.RunGitCommandAndGetOutput("rev-parse", "branch-b")
	require.NoError(t, err)
	require.Equal(t, strings.TrimSpace(bSHAbeforeSync), strings.TrimSpace(bSHAafterSync),
		"--no-restack must not move branch-b's git ref")

	requireCleanWorkingTree(t, sh)
}

// TestSquashMergeChildBecomesEmpty covers the case where a squash merge
// subsumes the child's diff (e.g., the merger combined A and B into one
// squash commit on trunk). After cleanup + restack, B is empty against
// trunk but should not be auto-deleted because no PR is marked merged
// for B.
func TestSquashMergeChildBecomesEmpty(t *testing.T) {
	t.Parallel()
	sh := scenario.NewRemoteScenario(t)
	disableCommitSigning(t, sh)

	// A introduces shared=v1; B advances it to v2.
	sh.CreateBranch("branch-a").
		CommitChange("shared", "v1").
		TrackBranch("branch-a", "main")
	sh.CreateBranch("branch-b").
		CommitChange("shared", "v2").
		TrackBranch("branch-b", "branch-a")

	// Squash-merge of A includes B's content too: trunk jumps straight
	// to v2 in one commit. (The reviewer combined the two PRs.)
	mainName := sh.Engine.Trunk().GetName()
	sh.Checkout("main")
	sh.CommitChange("shared", "v2")
	markPrMerged(t, sh, "branch-a", 1, mainName)

	require.NoError(t, sync.Action(sh.Context, sync.Options{Restack: true}, nil))

	branches, err := sh.Scene.Repo.GetLocalBranches()
	require.NoError(t, err)
	require.NotContains(t, branches, "branch-a")
	require.Contains(t, branches, "branch-b", "B has no PR; should survive even if empty")

	require.Equal(t, mainName, sh.Engine.GetBranch("branch-b").GetParent().GetName())

	empty, err := sh.Engine.IsBranchEmpty(sh.Context.Context, "branch-b")
	require.NoError(t, err)
	require.True(t, empty, "B should be empty relative to main after squash subsumed its diff")

	requireCleanWorkingTree(t, sh)
}

// User-induced inconsistency cases:
// the user did something locally that left stackit's view of the world
// out of sync with reality. Each test documents what sync actually does.

// TestUserDeletedMergedBranchBeforeSync: user runs `git branch -D` on the
// merged branch directly (forgetting to use stackit). The engine sees the
// branch is gone; the child's metadata still references it. Sync should
// recover gracefully and reparent the child rather than crash.
func TestUserDeletedMergedBranchBeforeSync(t *testing.T) {
	t.Parallel()
	sh := scenario.NewRemoteScenario(t)
	disableCommitSigning(t, sh)

	mainName := setupTwoBranchSquashScenario(t, sh)

	// User reaches for raw git instead of stackit.
	require.NoError(t, sh.Scene.Repo.RunGitCommand("branch", "-D", "branch-a"))

	// Refresh the engine so it sees that branch-a is gone — equivalent to a
	// fresh CLI invocation.
	require.NoError(t, sh.Engine.Rebuild(mainName))

	// Sync runs and shouldn't error.
	require.NoError(t, sync.Action(sh.Context, sync.Options{Restack: true}, nil))

	branches, err := sh.Scene.Repo.GetLocalBranches()
	require.NoError(t, err)
	require.NotContains(t, branches, "branch-a")
	require.Contains(t, branches, "branch-b")

	// B must reparent to trunk now that A is gone, with A's stale metadata
	// cleaned up. The ghost-detection path in buildDeletionPlanAndReparent
	// synthesizes a deletion entry for A so the existing reparent + cleanup
	// machinery runs against it.
	bParent := sh.Engine.GetBranch("branch-b").GetParent()
	require.NotNil(t, bParent, "B must have a parent entry after sync")
	require.Equal(t, mainName, bParent.GetName(),
		"B should reparent to trunk now that A is gone")

	// Stale metadata for the ghost should be cleaned up too — otherwise
	// the next sync would re-detect it and churn pointlessly.
	meta, err := sh.Engine.Git().ReadMetadata("branch-a")
	require.NoError(t, err)
	require.True(t, meta == nil || meta.GetParentBranchName() == nil,
		"A's stale metadata should be cleared after sync")

	requireCleanWorkingTree(t, sh)
}

// TestUserManuallyRebasedChildBeforeSync: between the GitHub squash-merge
// and `stackit sync`, the user did `git rebase main child` directly.
// This rewrites child's history so the divergence point captured in
// metadata (parent's pre-squash tip) is no longer an ancestor of child.
//
// This test pins down today's behavior so we notice if it changes —
// either to a clearer error message or to graceful auto-recovery.
func TestUserManuallyRebasedChildBeforeSync(t *testing.T) {
	t.Parallel()
	sh := scenario.NewRemoteScenario(t)
	disableCommitSigning(t, sh)

	mainName := setupTwoBranchSquashScenario(t, sh)

	// User reaches for `git rebase` directly, rewriting B's history.
	// B is now based on main's new tip — A's old tip is no longer in B's history.
	require.NoError(t, sh.Scene.Repo.RunGitCommand("rebase", mainName, "branch-b"))

	err := sync.Action(sh.Context, sync.Options{Restack: true}, nil)
	// Document current behavior: errors loudly, A is left in place.
	// If a future change makes sync recover gracefully, update both branches
	// of this assertion together.
	if err != nil {
		require.Contains(t, err.Error(), "divergence point",
			"if sync errors here, the message should still mention divergence point so users can diagnose")
		branches, lerr := sh.Scene.Repo.GetLocalBranches()
		require.NoError(t, lerr)
		require.Contains(t, branches, "branch-a",
			"sync aborted before deletion — A should still exist")
		require.Contains(t, branches, "branch-b")
		return
	}
	// Future-graceful path: if sync ever handles this without error,
	// it must still leave the world consistent.
	branches, lerr := sh.Scene.Repo.GetLocalBranches()
	require.NoError(t, lerr)
	require.NotContains(t, branches, "branch-a")
	require.Contains(t, branches, "branch-b")
	require.Equal(t, mainName, sh.Engine.GetBranch("branch-b").GetParent().GetName())
}

// TestMergeCommitNotSquash: GitHub merged the PR with the merge-commit
// strategy (not squash), so trunk has A's actual commits in its history.
// The MERGED metadata path AND the IsMerged git path both fire; verify
// cleanup runs cleanly.
func TestMergeCommitNotSquash(t *testing.T) {
	t.Parallel()
	sh := scenario.NewRemoteScenario(t)
	disableCommitSigning(t, sh)

	sh.CreateBranch("branch-a").
		CommitChange("file-a", "a").
		TrackBranch("branch-a", "main")
	sh.CreateBranch("branch-b").
		CommitChange("file-b", "b").
		TrackBranch("branch-b", "branch-a")

	mainName := sh.Engine.Trunk().GetName()

	// Merge-commit strategy: trunk now contains A's real commits via a merge,
	// not a single squash. A's tip becomes an ancestor of trunk.
	sh.Checkout("main")
	require.NoError(t, sh.Scene.Repo.RunGitCommand("merge", "--no-ff", "branch-a", "-m", "Merge A"))
	markPrMerged(t, sh, "branch-a", 1, mainName)

	require.NoError(t, sync.Action(sh.Context, sync.Options{Restack: true}, nil))

	branches, err := sh.Scene.Repo.GetLocalBranches()
	require.NoError(t, err)
	require.NotContains(t, branches, "branch-a", "merge-committed branch should be cleaned up too")
	require.Contains(t, branches, "branch-b")

	require.Equal(t, mainName, sh.Engine.GetBranch("branch-b").GetParent().GetName())

	// B's commit boundary must be preserved — A's commits are already in trunk.
	bCount, err := sh.Engine.GetCommitCount(sh.Engine.GetBranch("branch-b"))
	require.NoError(t, err)
	require.Equal(t, 1, bCount, "B should have its own commit only, not a duplicate of A's")

	requireCleanWorkingTree(t, sh)
}

// TestUserLocallyAdvancedTrunkBeforeSync: user did `git pull` (or some
// equivalent) and trunk locally is already at the post-squash state when
// sync runs. The MERGED metadata + already-current trunk shouldn't cause
// double-application or restack churn on the surviving child.
func TestUserLocallyAdvancedTrunkBeforeSync(t *testing.T) {
	t.Parallel()
	sh := scenario.NewRemoteScenario(t)
	disableCommitSigning(t, sh)

	sh.CreateBranch("branch-a").
		CommitChange("file-a", "a").
		TrackBranch("branch-a", "main")
	sh.CreateBranch("branch-b").
		CommitChange("file-b", "b").
		TrackBranch("branch-b", "branch-a")

	mainName := sh.Engine.Trunk().GetName()

	// Squash already on trunk locally before sync starts.
	sh.Checkout("main")
	sh.CommitChange("file-a", "a")
	mainSHABeforeSync, err := sh.Scene.Repo.RunGitCommandAndGetOutput("rev-parse", mainName)
	require.NoError(t, err)

	markPrMerged(t, sh, "branch-a", 1, mainName)
	require.NoError(t, sync.Action(sh.Context, sync.Options{Restack: true}, nil))

	// Trunk shouldn't have moved — sync had nothing to apply on top of it.
	mainSHAAfterSync, err := sh.Scene.Repo.RunGitCommandAndGetOutput("rev-parse", mainName)
	require.NoError(t, err)
	require.Equal(t, strings.TrimSpace(mainSHABeforeSync), strings.TrimSpace(mainSHAAfterSync),
		"trunk must not move during sync when it's already at the merged state")

	branches, err := sh.Scene.Repo.GetLocalBranches()
	require.NoError(t, err)
	require.NotContains(t, branches, "branch-a")
	require.Contains(t, branches, "branch-b")

	require.Equal(t, mainName, sh.Engine.GetBranch("branch-b").GetParent().GetName())

	bCount, err := sh.Engine.GetCommitCount(sh.Engine.GetBranch("branch-b"))
	require.NoError(t, err)
	require.Equal(t, 1, bCount)

	requireCleanWorkingTree(t, sh)
}
