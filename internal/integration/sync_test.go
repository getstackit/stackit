package integration

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/internal/actions/sync"
	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/git"
	"stackit.dev/stackit/testhelpers"
	"stackit.dev/stackit/testhelpers/scenario"
)

const prStateMerged = "MERGED"

func TestSync(t *testing.T) {
	t.Parallel()
	t.Run("local parent is authoritative over GitHub PR base", func(t *testing.T) {
		t.Parallel()
		sh := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

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
		sh := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

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
		metas, readErrs := eng.Git().BatchReadMetadata(branchNames)
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
		sh := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

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
	sh := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

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
	sh := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

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
	sh := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

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

func TestSyncRemoteMetadata(t *testing.T) {
	t.Parallel()
	t.Run("loads remote metadata cache", func(t *testing.T) {
		t.Parallel()
		sh := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// Create a branch
		sh.CreateBranch("feature-a").
			CommitChange("file-a", "content-a").
			TrackBranch("feature-a", "main")

		eng := sh.Engine

		// Set local metadata
		branch := eng.GetBranch("feature-a")
		_, err := eng.SetLocked(context.Background(), []engine.Branch{branch}, engine.LockReasonNone)
		require.NoError(t, err)

		// Create remote metadata refs (simulating a successful fetch)
		remoteMeta := git.NewMetaFrom(git.MetaFields{
			LockReason: git.LockReasonUser,
			Scope:      new("remote-scope"),
		})
		createRemoteMetadataRefForSync(t, sh, "feature-a", remoteMeta)

		// Load remote metadata cache (this is what sync does after fetching)
		err = eng.LoadRemoteMetadataCache()
		require.NoError(t, err)

		// Verify remote metadata cache was loaded
		cache := eng.GetRemoteMetadataCache()
		cachedMeta := cache.Get("feature-a")
		require.NotNil(t, cachedMeta, "Remote metadata should be in cache")
		require.Equal(t, git.LockReasonUser, cachedMeta.GetLockReason(), "Remote metadata should show lock reason")
		require.Equal(t, "remote-scope", *cachedMeta.GetScope(), "Remote metadata should have scope")
	})

	t.Run("detects metadata conflicts", func(t *testing.T) {
		t.Parallel()
		sh := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		sh.CreateBranch("feature-b").
			CommitChange("file-b", "content-b").
			TrackBranch("feature-b", "main")

		eng := sh.Engine

		// Set local metadata: locked=false
		branch := eng.GetBranch("feature-b")
		_, err := eng.SetLocked(context.Background(), []engine.Branch{branch}, engine.LockReasonNone)
		require.NoError(t, err)

		// Create remote metadata refs: locked=true (conflict)
		remoteMeta := git.NewMetaFrom(git.MetaFields{
			LockReason: git.LockReasonUser,
		})
		createRemoteMetadataRefForSync(t, sh, "feature-b", remoteMeta)

		// Load remote metadata cache
		err = eng.LoadRemoteMetadataCache()
		require.NoError(t, err)

		// Compute diff to verify conflict detection
		diff, err := eng.ComputeMetadataDiff("feature-b")
		require.NoError(t, err)
		require.NotNil(t, diff)
		require.True(t, diff.HasConflict, "Should detect conflict between local and remote metadata")

		// Verify the specific field that differs
		require.Len(t, diff.Differences, 1)
		require.Equal(t, "lockReason", diff.Differences[0].Field)
		require.Equal(t, git.LockReasonNone, diff.Differences[0].LocalValue)
		require.Equal(t, git.LockReasonUser, diff.Differences[0].RemoteValue)
	})
}

func TestSyncSquashMergedRootPreservesChildCommitBoundaries(t *testing.T) {
	t.Parallel()
	sh := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

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

// createRemoteMetadataRefForSync creates a ref at refs/stackit/remote-metadata/<branch>
func createRemoteMetadataRefForSync(t *testing.T, sh *scenario.Scenario, branchName string, meta *git.Meta) {
	t.Helper()

	data, err := json.Marshal(meta)
	require.NoError(t, err)

	tmpFile := filepath.Join(sh.Scene.Dir, ".git", "tmp-meta-"+branchName)
	err = os.WriteFile(tmpFile, data, 0600)
	require.NoError(t, err)
	defer os.Remove(tmpFile)

	blobSha, err := sh.Scene.Repo.RunGitCommandAndGetOutput("hash-object", "-w", tmpFile)
	require.NoError(t, err)

	// Remove trailing newline
	if len(blobSha) > 0 && blobSha[len(blobSha)-1] == '\n' {
		blobSha = blobSha[:len(blobSha)-1]
	}

	refName := "refs/stackit/remote-metadata/" + branchName
	err = sh.Scene.Repo.RunGitCommand("update-ref", refName, blobSha)
	require.NoError(t, err)
}

// TestSyncDoesNotLeaveIndexState verifies that after sync cleans up merged branches
// and restacks remaining branches, the working directory is left in a clean state
// with no staged changes. This is a regression test for the bug where the Git index
// would retain stale state after multiple rebase operations during RestackBranches.
func TestSyncDoesNotLeaveIndexState(t *testing.T) {
	t.Parallel()
	t.Run("sync from main does not leave staged changes after cleanup and restack", func(t *testing.T) {
		t.Parallel()
		sh := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

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
		output, err := sh.Scene.Repo.RunGitCommandAndGetOutput("status", "--porcelain")
		require.NoError(t, err)
		require.Empty(t, strings.TrimSpace(output), "git status should be clean after sync, but got: %s", output)

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
		sh := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

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
		output, err := sh.Scene.Repo.RunGitCommandAndGetOutput("status", "--porcelain")
		require.NoError(t, err)
		require.Empty(t, strings.TrimSpace(output), "git status should be clean after sync from detached HEAD")
	})

	t.Run("sync with rebase that moves commits does not leave staged changes", func(t *testing.T) {
		t.Parallel()
		// This test creates a scenario where the rebase actually moves commits,
		// which is more likely to trigger index state issues.
		sh := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

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
		output, err := sh.Scene.Repo.RunGitCommandAndGetOutput("status", "--porcelain")
		require.NoError(t, err)
		require.Empty(t, strings.TrimSpace(output), "git status should be clean after sync with rebase, but got: %s", output)

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
		sh := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// Create a branch and track it
		sh.CreateBranch("feature").
			CommitChange("feature-file", "content").
			TrackBranch("feature", "main")

		eng := sh.Engine
		mainBranchName := eng.Trunk().GetName()

		// Set up remote and push everything
		_, err := sh.Scene.Repo.CreateBareRemote("origin")
		require.NoError(t, err)
		sh.Checkout("main")
		err = sh.Scene.Repo.PushBranch("origin", "main")
		require.NoError(t, err)
		sh.Checkout("feature")
		err = sh.Scene.Repo.PushBranch("origin", "feature")
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

		// Populate remote SHAs so GetBranchRemoteStatus can detect ahead
		err = eng.PopulateRemoteShas()
		require.NoError(t, err)

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
