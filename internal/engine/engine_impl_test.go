package engine_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/errors"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestTrackBranch(t *testing.T) {
	t.Parallel()
	t.Run("tracks branch with parent", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// Create feature branch
		s.CreateBranch("feature").
			Commit("feature change")
		s.Checkout("main")

		// Track branch
		err := s.Engine.TrackBranch(context.Background(), "feature", "main")
		require.NoError(t, err)

		// Verify parent relationship
		branch := s.Engine.GetBranch("feature")
		parent := branch.GetParent()
		require.NotNil(t, parent)
		require.Equal(t, "main", parent.GetName())

		// Verify children relationship
		graph := engine.BuildStackGraph(s.Engine, engine.SortStrategyAlphabetical, nil)
		childNames := graph.Children(s.Engine.Trunk())
		require.Contains(t, childNames, "feature")
	})

	t.Run("tracks branch with non-trunk parent", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// Create both branches first
		s.CreateBranch("branch1").
			Commit("branch1 change").
			CreateBranch("branch2").
			Commit("branch2 change").
			Checkout("main")

		// Track branch1 first
		err := s.Engine.TrackBranch(context.Background(), "branch1", "main")
		require.NoError(t, err)

		// Track branch2 (branch1 is now tracked and in the branch list)
		err = s.Engine.TrackBranch(context.Background(), "branch2", "branch1")
		require.NoError(t, err)

		// Verify relationships
		branch1 := s.Engine.GetBranch("branch1")
		parent1 := branch1.GetParent()
		require.NotNil(t, parent1)
		require.Equal(t, "main", parent1.GetName())
		branch2 := s.Engine.GetBranch("branch2")
		parent2 := branch2.GetParent()
		require.NotNil(t, parent2)
		require.Equal(t, "branch1", parent2.GetName())
		graph := engine.BuildStackGraph(s.Engine, engine.SortStrategyAlphabetical, nil)
		mainChildNames := graph.Children(s.Engine.Trunk())
		require.Contains(t, mainChildNames, "branch1")
		branch1ChildNames := graph.Children(s.Engine.GetBranch("branch1"))
		require.Contains(t, branch1ChildNames, "branch2")
	})

	t.Run("fails when branch does not exist", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		err := s.Engine.TrackBranch(context.Background(), "nonexistent", "main")
		require.Error(t, err)
		require.Contains(t, err.Error(), "does not exist")
	})

	t.Run("fails when parent does not exist", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// Create feature branch
		s.CreateBranch("feature").
			Commit("feature change").
			Checkout("main")

		// Try to track with nonexistent parent - should fail
		err := s.Engine.TrackBranch(context.Background(), "feature", "nonexistent")
		require.Error(t, err)
		require.Contains(t, err.Error(), "parent branch nonexistent does not exist")
	})
}

func TestLinearStacksRejectForks(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
	s.CreateBranch("base").Commit("base change").
		CreateBranch("first-child").Commit("first child change").
		Checkout("base").CreateBranch("second-child").Commit("second child change").
		Checkout("main")

	eng, err := engine.NewEngine(engine.Options{
		RepoRoot:     s.Scene.Dir,
		Trunk:        "main",
		LinearStacks: true,
	})
	require.NoError(t, err)

	require.NoError(t, eng.TrackBranch(context.Background(), "base", "main"))
	require.NoError(t, eng.TrackBranch(context.Background(), "first-child", "base"))
	err = eng.TrackBranch(context.Background(), "second-child", "base")
	require.Error(t, err)
	require.Contains(t, err.Error(), "linear stacks are enabled")
	require.Contains(t, err.Error(), "stack.shape to tree")

	// Independent roots remain valid: linear mode is a forest of chains.
	require.NoError(t, eng.TrackBranch(context.Background(), "second-child", "main"))
}

func TestLinearStacksAllowDeletingMiddleBranch(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
	s.CreateBranch("base").Commit("base change").
		CreateBranch("middle").Commit("middle change").
		CreateBranch("child").Commit("child change").
		Checkout("main")

	eng, err := engine.NewEngine(engine.Options{
		RepoRoot:     s.Scene.Dir,
		Trunk:        "main",
		LinearStacks: true,
	})
	require.NoError(t, err)
	require.NoError(t, eng.TrackBranch(context.Background(), "base", "main"))
	require.NoError(t, eng.TrackBranch(context.Background(), "middle", "base"))
	require.NoError(t, eng.TrackBranch(context.Background(), "child", "middle"))

	require.NoError(t, eng.DeleteBranch(context.Background(), eng.GetBranch("middle")))
	require.Equal(t, "base", eng.GetBranch("child").GetParentOrTrunk())
	require.False(t, eng.BranchNames().Contains("middle"))
}

func TestGetWorkingTreeStatus(t *testing.T) {
	t.Parallel()

	t.Run("clean repo", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		staged, unstaged, untracked, err := s.Engine.GetWorkingTreeStatus(context.Background())
		require.NoError(t, err)
		require.False(t, staged)
		require.False(t, unstaged)
		require.False(t, untracked)
	})

	t.Run("unstaged tracked change", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		require.NoError(t, s.Scene.Repo.CreateChange("2", "1", true))

		staged, unstaged, untracked, err := s.Engine.GetWorkingTreeStatus(context.Background())
		require.NoError(t, err)
		require.False(t, staged)
		require.True(t, unstaged)
		require.False(t, untracked)
	})

	t.Run("staged tracked change", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		require.NoError(t, s.Scene.Repo.CreateChange("2", "1", false))

		staged, unstaged, untracked, err := s.Engine.GetWorkingTreeStatus(context.Background())
		require.NoError(t, err)
		require.True(t, staged)
		require.False(t, unstaged)
		require.False(t, untracked)
	})

	t.Run("untracked file", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		require.NoError(t, s.Scene.Repo.CreateChange("new", "untracked", true))

		staged, unstaged, untracked, err := s.Engine.GetWorkingTreeStatus(context.Background())
		require.NoError(t, err)
		require.False(t, staged)
		require.False(t, unstaged)
		require.True(t, untracked)
	})
}

func TestSetParent(t *testing.T) {
	t.Parallel()
	t.Run("updates parent relationship", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// Create all branches first
		s.CreateBranch("branch1").
			Commit("branch1 change").
			CreateBranch("branch2").
			Commit("branch2 change").
			Checkout("branch1").
			CreateBranch("branch3").
			Commit("branch3 change").
			Checkout("main")

		// Track branches
		err := s.Engine.TrackBranch(context.Background(), "branch1", "main")
		require.NoError(t, err)
		err = s.Engine.TrackBranch(context.Background(), "branch2", "branch1")
		require.NoError(t, err)
		err = s.Engine.TrackBranch(context.Background(), "branch3", "branch1")
		require.NoError(t, err)

		// Verify initial state
		branch2 := s.Engine.GetBranch("branch2")
		parent2 := branch2.GetParent()
		require.NotNil(t, parent2)
		require.Equal(t, "branch1", parent2.GetName())

		// Change parent of branch2 to main
		err = s.Engine.SetParent(context.Background(), s.Engine.GetBranch("branch2"), s.Engine.GetBranch("main"), engine.DivergenceRecompute)
		require.NoError(t, err)

		// Verify new parent
		branchparent2After := s.Engine.GetBranch("branch2")
		parent2After := branchparent2After.GetParent()
		require.NotNil(t, parent2After)
		require.Equal(t, "main", parent2After.GetName())
		graph := engine.BuildStackGraph(s.Engine, engine.SortStrategyAlphabetical, nil)
		mainChildNames := graph.Children(s.Engine.Trunk())
		require.Contains(t, mainChildNames, "branch2")
		branch1ChildNames := graph.Children(s.Engine.GetBranch("branch1"))
		require.NotContains(t, branch1ChildNames, "branch2")
	})
}

func TestDeleteBranch(t *testing.T) {
	t.Parallel()
	t.Run("deletes branch and updates children", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// Create branch structure: main -> branch1 -> branch2, branch3
		s.CreateBranch("branch1").
			Commit("branch1 change").
			CreateBranch("branch2").
			Commit("branch2 change").
			Checkout("branch1").
			CreateBranch("branch3").
			Commit("branch3 change").
			Checkout("main")

		// Track all branches
		err := s.Engine.TrackBranch(context.Background(), "branch1", "main")
		require.NoError(t, err)
		err = s.Engine.TrackBranch(context.Background(), "branch2", "branch1")
		require.NoError(t, err)
		err = s.Engine.TrackBranch(context.Background(), "branch3", "branch1")
		require.NoError(t, err)

		// Verify initial state
		graph := engine.BuildStackGraph(s.Engine, engine.SortStrategyAlphabetical, nil)
		branch1ChildNames := graph.Children(s.Engine.GetBranch("branch1"))
		require.Contains(t, branch1ChildNames, "branch2")
		require.Contains(t, branch1ChildNames, "branch3")

		// Delete branch1
		err = s.Engine.DeleteBranch(context.Background(), s.Engine.GetBranch("branch1"))
		require.NoError(t, err)

		// Verify branch1 is removed
		require.False(t, s.Engine.GetBranch("branch1").IsTracked())
		allBranches := s.Engine.AllBranches()
		require.NotContains(t, allBranches.Names(), "branch1")

		// Verify children now point to main
		branchparent2 := s.Engine.GetBranch("branch2")
		parent2 := branchparent2.GetParent()
		require.NotNil(t, parent2)
		require.Equal(t, "main", parent2.GetName())
		branchparent3 := s.Engine.GetBranch("branch3")
		parent3 := branchparent3.GetParent()
		require.NotNil(t, parent3)
		require.Equal(t, "main", parent3.GetName())
		graph = engine.BuildStackGraph(s.Engine, engine.SortStrategyAlphabetical, nil)
		mainChildNames := graph.Children(s.Engine.Trunk())
		require.Contains(t, mainChildNames, "branch2")
		require.Contains(t, mainChildNames, "branch3")
	})

	t.Run("deletes branch with multiple siblings and children", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"P":   "main",
				"C1":  "P",
				"GC1": "C1",
				"C2":  "P",
				"C3":  "P",
				"GC3": "C3",
			})

		// Verify initial parent of children is P
		branchparentC1 := s.Engine.GetBranch("C1")
		parentC1 := branchparentC1.GetParent()
		require.NotNil(t, parentC1)
		require.Equal(t, "P", parentC1.GetName())
		branchparentC2 := s.Engine.GetBranch("C2")
		parentC2 := branchparentC2.GetParent()
		require.NotNil(t, parentC2)
		require.Equal(t, "P", parentC2.GetName())
		branchparentC3 := s.Engine.GetBranch("C3")
		parentC3 := branchparentC3.GetParent()
		require.NotNil(t, parentC3)
		require.Equal(t, "P", parentC3.GetName())

		// Delete P
		err := s.Engine.DeleteBranch(context.Background(), s.Engine.GetBranch("P"))
		require.NoError(t, err)

		// Verify P is removed
		require.False(t, s.Engine.GetBranch("P").IsTracked())

		// Verify all direct children of P now point to main
		branchparentC1After := s.Engine.GetBranch("C1")
		parentC1After := branchparentC1After.GetParent()
		require.NotNil(t, parentC1After)
		require.Equal(t, "main", parentC1After.GetName())
		branchparentC2After := s.Engine.GetBranch("C2")
		parentC2After := branchparentC2After.GetParent()
		require.NotNil(t, parentC2After)
		require.Equal(t, "main", parentC2After.GetName())
		branchparentC3After := s.Engine.GetBranch("C3")
		parentC3After := branchparentC3After.GetParent()
		require.NotNil(t, parentC3After)
		require.Equal(t, "main", parentC3After.GetName())

		// Verify grandchildren still point to their parents
		branchparentGC1 := s.Engine.GetBranch("GC1")
		parentGC1 := branchparentGC1.GetParent()
		require.NotNil(t, parentGC1)
		require.Equal(t, "C1", parentGC1.GetName())
		branchparentGC3 := s.Engine.GetBranch("GC3")
		parentGC3 := branchparentGC3.GetParent()
		require.NotNil(t, parentGC3)
		require.Equal(t, "C3", parentGC3.GetName())

		// Verify main's children list contains C1, C2, C3
		graph := engine.BuildStackGraph(s.Engine, engine.SortStrategyAlphabetical, nil)
		mainChildNames := graph.Children(s.Engine.Trunk())
		require.Contains(t, mainChildNames, "C1")
		require.Contains(t, mainChildNames, "C2")
		require.Contains(t, mainChildNames, "C3")
	})

	t.Run("returns error when child cannot be reparented", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// Create main -> branch1 -> branch2 and track both.
		s.CreateBranch("branch1").
			Commit("branch1 change").
			CreateBranch("branch2").
			Commit("branch2 change").
			Checkout("main")
		require.NoError(t, s.Engine.TrackBranch(context.Background(), "branch1", "main"))
		require.NoError(t, s.Engine.TrackBranch(context.Background(), "branch2", "branch1"))

		// Force branch2 onto an unrelated orphan root so merge-base(branch2, main) fails.
		s.Checkout("branch2")
		s.RunGit("checkout", "--orphan", "orphan-tmp")
		s.RunGit("commit", "--allow-empty", "-m", "orphan root")
		orphanRev, err := s.Scene.Repo.GetRevision("HEAD")
		require.NoError(t, err)
		s.Checkout("main")
		s.RunGit("branch", "-f", "branch2", orphanRev)

		require.NoError(t, s.Engine.Rebuild("main"))

		err = s.Engine.DeleteBranch(context.Background(), s.Engine.GetBranch("branch1"))
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to reparent")
		require.Contains(t, err.Error(), "branch2")
	})

	t.Run("fails when trying to delete trunk", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		err := s.Engine.DeleteBranch(context.Background(), s.Engine.GetBranch("main"))
		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot delete trunk")
	})
}

func TestStackGraphRange(t *testing.T) {
	t.Parallel()
	t.Run("returns downstack (ancestors) excluding trunk", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
			})

		// Get downstack from branch2 - should NOT include trunk (main)
		graph := engine.BuildStackGraph(s.Engine, engine.SortStrategyAlphabetical, nil)
		stack := graph.Range(s.Engine.GetBranch("branch2"), engine.StackRange{RecursiveParents: true})
		stackNames := stack.Names()
		require.Equal(t, []string{"branch1"}, stackNames)
		require.NotContains(t, stackNames, "main", "trunk should not be included in ancestors")
	})

	t.Run("returns upstack (descendants)", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
				"branch3": "branch1",
			})

		// Get upstack from branch1
		graph := engine.BuildStackGraph(s.Engine, engine.SortStrategyAlphabetical, nil)
		stack := graph.Range(s.Engine.GetBranch("branch1"), engine.StackRange{RecursiveChildren: true})
		stackNames := stack.Names()
		require.Contains(t, stackNames, "branch2")
		require.Contains(t, stackNames, "branch3")
		require.Len(t, stackNames, 2)
	})

	t.Run("returns only current branch", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
			})

		graph := engine.BuildStackGraph(s.Engine, engine.SortStrategyAlphabetical, nil)
		stack := graph.Range(s.Engine.GetBranch("branch1"), engine.StackRange{IncludeCurrent: true})
		stackNames := stack.Names()
		require.Equal(t, []string{"branch1"}, stackNames)
	})

	t.Run("returns full stack (downstack + current + upstack) excluding trunk", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
				"branch3": "branch2",
			})

		// Get full stack from branch2 - should NOT include trunk (main)
		graph := engine.BuildStackGraph(s.Engine, engine.SortStrategyAlphabetical, nil)
		stack := graph.Range(s.Engine.GetBranch("branch2"), engine.StackRange{
			RecursiveParents:  true,
			IncludeCurrent:    true,
			RecursiveChildren: true,
		})
		stackNames := stack.Names()
		require.NotContains(t, stackNames, "main", "trunk should not be included in ancestors")
		require.Contains(t, stackNames, "branch1")
		require.Contains(t, stackNames, "branch2")
		require.Contains(t, stackNames, "branch3")
		require.Len(t, stack, 3)
	})

	t.Run("returns branching stacks in DFS order (parents before children)", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"stackA":       "main",
				"stackA-child": "stackA",
				"stackB":       "main",
				"stackB-child": "stackB",
			})

		// Get all descendants from trunk
		graph := engine.BuildStackGraph(s.Engine, engine.SortStrategyAlphabetical, nil)
		stack := graph.Range(s.Engine.Trunk(), engine.StackRange{RecursiveChildren: true})

		// Should have all 4 branches
		require.Len(t, stack, 4)
		stackNames := stack.Names()
		require.Contains(t, stackNames, "stackA")
		require.Contains(t, stackNames, "stackA-child")
		require.Contains(t, stackNames, "stackB")
		require.Contains(t, stackNames, "stackB-child")

		// Verify topological order: parents must come before their children
		stackAIdx := indexOf(stackNames, "stackA")
		stackAChildIdx := indexOf(stackNames, "stackA-child")
		stackBIdx := indexOf(stackNames, "stackB")
		stackBChildIdx := indexOf(stackNames, "stackB-child")

		require.Less(t, stackAIdx, stackAChildIdx, "stackA should come before stackA-child")
		require.Less(t, stackBIdx, stackBChildIdx, "stackB should come before stackB-child")
	})
}

// indexOf returns the index of item in slice, or -1 if not found
func indexOf(slice []string, item string) int {
	for i, s := range slice {
		if s == item {
			return i
		}
	}
	return -1
}

func TestNewEngine_Scoping(t *testing.T) {
	// 1. Create a "main" repo and stay "inside" it (scenario.NewScenario calls os.Chdir)
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
	mainDir := s.Scene.Dir

	// 2. Create a "separate" repo in a different location
	otherDir := t.TempDir()
	otherRepo, err := testhelpers.NewGitRepo(otherDir)
	require.NoError(t, err)

	// Create a unique branch in the other repo
	err = otherRepo.CreateChangeAndCommit("initial", "init")
	require.NoError(t, err)
	err = otherRepo.CreateAndCheckoutBranch("unique-branch-in-other-repo")
	require.NoError(t, err)

	// 3. Initialize an engine for otherDir while our CWD is still mainDir
	eng, err := engine.NewEngine(engine.Options{
		RepoRoot: otherDir,
		Trunk:    "main",
	})
	require.NoError(t, err)

	// 4. VERIFY: The engine should see the branch from otherDir
	// If the bug exists, the engine's Git runner will have used os.Getwd()
	// and will incorrectly see the current branch of mainDir (which is "main")
	curr := eng.CurrentBranch()
	require.NotNil(t, curr)
	require.Equal(t, "unique-branch-in-other-repo", curr.GetName(),
		"Engine must be scoped to its RepoRoot (%s), but seems to be looking at CWD (%s)", otherDir, mainDir)
}

func TestRestackBranches(t *testing.T) {
	t.Parallel()
	t.Run("restacks branch when parent has moved", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
			})

		// Add commit to main (parent moves forward)
		s.Checkout("main").
			Commit("main update")

		// Restack branch1 (should rebase onto new main)
		branch1 := s.Engine.GetBranch("branch1")
		batchResult, err := s.Engine.RestackBranches(context.Background(), engine.BranchesOf(branch1))
		require.NoError(t, err)
		require.Equal(t, engine.RestackDone, batchResult.Results["branch1"].Result)

		// Verify branch1 is now fixed
		require.True(t, s.Engine.GetBranch("branch1").IsBranchUpToDate())
	})

	t.Run("returns unneeded when branch is already fixed", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
			})

		// Branch is already fixed (no changes to main)
		branch1 := s.Engine.GetBranch("branch1")
		batchResult, err := s.Engine.RestackBranches(context.Background(), engine.BranchesOf(branch1))
		require.NoError(t, err)
		require.Equal(t, engine.RestackUnneeded, batchResult.Results["branch1"].Result)
	})

	t.Run("reports progress as each branch is processed", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
			})

		s.Checkout("main").
			Commit("main update")

		branches := []engine.Branch{
			s.Engine.GetBranch("branch1"),
			s.Engine.GetBranch("branch2"),
		}
		var progressed []string

		batchResult, err := s.Engine.RestackBranchesWithProgress(context.Background(), branches, func(branch engine.Branch, result engine.RestackBranchResult) {
			progressed = append(progressed, branch.GetName())
			require.Contains(t, []engine.RestackResult{engine.RestackDone, engine.RestackUnneeded}, result.Result)
		})
		require.NoError(t, err)
		require.Equal(t, []string{"branch1", "branch2"}, progressed)
		require.Equal(t, engine.RestackDone, batchResult.Results["branch1"].Result)
		require.Equal(t, engine.RestackDone, batchResult.Results["branch2"].Result)
	})

	t.Run("auto-tracks branch when branch is not tracked", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			CreateBranch("branch1").
			Commit("branch1 change")

		// Don't track the branch explicitly
		// RestackBranches should auto-discover parent (main) and succeed
		// In this case, main is still at the fork point, so FindMostRecentTrackedAncestors finds it.
		branch1 := s.Engine.GetBranch("branch1")
		batchResult, err := s.Engine.RestackBranches(context.Background(), engine.BranchesOf(branch1))
		require.NoError(t, err)
		require.Equal(t, engine.RestackUnneeded, batchResult.Results["branch1"].Result)

		// Verify it is now tracked
		require.True(t, s.Engine.GetBranch("branch1").IsTracked())
		branchparent1 := s.Engine.GetBranch("branch1")
		parent1 := branchparent1.GetParent()
		require.NotNil(t, parent1)
		require.Equal(t, "main", parent1.GetName())
	})
}

func TestRebuild(t *testing.T) {
	t.Parallel()
	t.Run("rebuilds cache from Git state", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
			})

		// Verify initial state
		allBranches := s.Engine.AllBranches()
		require.Contains(t, allBranches.Names(), "branch1")
		branchparent1 := s.Engine.GetBranch("branch1")
		parent1 := branchparent1.GetParent()
		require.NotNil(t, parent1)
		require.Equal(t, "main", parent1.GetName())

		// Create new branch externally (not tracked)
		s.CreateBranchQuiet("branch2").
			Commit("branch2 change").
			CheckoutQuiet("main")

		// Rebuild should pick up new branch
		err := s.Engine.Rebuild("main")
		require.NoError(t, err)

		// New branch should be in list
		allBranches2 := s.Engine.AllBranches()
		require.Contains(t, allBranches2.Names(), "branch2")
		// But not tracked yet
		require.False(t, s.Engine.GetBranch("branch2").IsTracked())
	})
}

func TestRebuildBranches(t *testing.T) {
	t.Parallel()

	// descendantsOf walks the childrenMap-backed traversal and returns each
	// branch's depth below start. This is the accessor that breaks if a scoped
	// rebuild leaves childrenMap inconsistent.
	descendantsOf := func(eng engine.Engine, start string) map[string]int {
		got := map[string]int{}
		for b, depth := range eng.BranchesDepthFirst(eng.GetBranch(start)) {
			got[b.GetName()] = depth
		}
		return got
	}

	t.Run("reparent merges without disturbing the rest of the graph", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithLinearStack("a", "b", "c") // main -> a -> b -> c

		// Externally reparent b onto main (as a raw metadata write the engine
		// did not observe through its own mutation path).
		main := "main"
		meta, err := s.Engine.Git().ReadMetadata("b")
		require.NoError(t, err)
		require.NoError(t, s.Engine.Git().WriteMetadata("b", meta.WithParentBranchName(&main)))

		// Engine still shows the pre-write graph until we refresh.
		require.Equal(t, "a", s.Engine.GetBranch("b").GetParent().GetName())

		require.NoError(t, s.Engine.RebuildBranches([]string{"b"}))

		// b now parents onto main; a and c are untouched.
		require.Equal(t, "main", s.Engine.GetBranch("b").GetParent().GetName())
		require.Equal(t, "main", s.Engine.GetBranch("a").GetParent().GetName())
		require.Equal(t, "b", s.Engine.GetBranch("c").GetParent().GetName())

		// childrenMap is consistent: a lost b, main gained b, b keeps c.
		fromMain := descendantsOf(s.Engine, "main")
		require.Equal(t, 1, fromMain["a"], "a stays a direct child of main")
		require.Equal(t, 1, fromMain["b"], "b is now a direct child of main")
		require.Equal(t, 2, fromMain["c"], "c stays under b")
		// The traversal includes the start node at depth 0, so a with no
		// children yields only itself.
		require.Equal(t, map[string]int{"a": 0}, descendantsOf(s.Engine, "a"), "a has no children after b moved off")
	})

	t.Run("drops a branch that became untracked", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithLinearStack("a", "b") // main -> a -> b

		require.NoError(t, s.Engine.Git().DeleteMetadata(context.Background(), "b"))

		require.NoError(t, s.Engine.RebuildBranches([]string{"b"}))

		require.False(t, s.Engine.GetBranch("b").IsTracked(), "b is no longer tracked")
		// a's children no longer include b.
		require.NotContains(t, descendantsOf(s.Engine, "a"), "b")
	})

	t.Run("dropping an untracked branch keeps the children still parented to it", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithLinearStack("a", "b", "c") // main -> a -> b -> c

		// Untrack the middle branch. c's metadata still names b as its parent,
		// so a full rebuild would keep the b -> c edge; a scoped rebuild must
		// not diverge by erasing it.
		require.NoError(t, s.Engine.Git().DeleteMetadata(context.Background(), "b"))

		require.NoError(t, s.Engine.RebuildBranches([]string{"b"}))

		require.False(t, s.Engine.GetBranch("b").IsTracked(), "b is no longer tracked")
		require.NotContains(t, descendantsOf(s.Engine, "a"), "b")
		require.Equal(t, 1, descendantsOf(s.Engine, "b")["c"], "c is still a child of b")
	})

	t.Run("only refreshes the listed branches", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithLinearStack("a", "b") // main -> a -> b

		// Reparent a onto main->... actually change a's recorded parent revision
		// externally, then rebuild only b. a must stay as the engine last saw it.
		newRev := "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
		metaA, err := s.Engine.Git().ReadMetadata("a")
		require.NoError(t, err)
		require.NoError(t, s.Engine.Git().WriteMetadata("a", metaA.WithParentBranchRevision(&newRev)))

		require.NoError(t, s.Engine.RebuildBranches([]string{"b"}))

		// a was not in the list, so its cached state is unchanged (still tracked
		// under main, no surprise refresh).
		require.Equal(t, "main", s.Engine.GetBranch("a").GetParent().GetName())
		require.True(t, s.Engine.GetBranch("a").IsTracked())
	})
}

func TestCreateBranchUpdatesBranchInventory(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

	s.CreateBranchQuiet("branch3").
		Commit("branch3 change").
		CheckoutQuiet("main")

	require.NoError(t, s.Engine.CreateBranch(context.Background(), "branch2", "main"))

	allBranches := s.Engine.AllBranches()
	require.Contains(t, allBranches.Names(), "branch2")
	require.NotContains(t, allBranches.Names(), "branch3")

	require.False(t, s.Engine.GetBranch("branch2").IsTracked())
	require.NoError(t, s.Engine.TrackBranch(context.Background(), "branch2", "main"))
	require.Equal(t, "main", s.Engine.GetBranch("branch2").GetParentOrTrunk())
}

func TestIsBranchTracked(t *testing.T) {
	t.Parallel()
	t.Run("returns true for tracked branch", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			CreateBranch("branch1").
			Commit("branch1 change").
			Checkout("main")

		require.False(t, s.Engine.GetBranch("branch1").IsTracked())

		err := s.Engine.TrackBranch(context.Background(), "branch1", "main")
		require.NoError(t, err)

		require.True(t, s.Engine.GetBranch("branch1").IsTracked())
	})

	t.Run("returns false for untracked branch", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			CreateBranch("branch1").
			Commit("branch1 change").
			Checkout("main")

		require.False(t, s.Engine.GetBranch("branch1").IsTracked())
	})
}

func TestIsTrunk(t *testing.T) {
	t.Parallel()
	t.Run("returns true for trunk branch", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		require.True(t, s.Engine.GetBranch("main").IsTrunk())
		require.False(t, s.Engine.GetBranch("other").IsTrunk())
	})
}

func TestGetParentOrTrunk(t *testing.T) {
	t.Parallel()
	t.Run("returns parent when exists", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
			})

		branch := s.Engine.GetBranch("branch1")
		parent := branch.GetParentOrTrunk()
		require.Equal(t, "main", parent)
	})

	t.Run("returns trunk when no parent", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			CreateBranch("branch1").
			Commit("branch1 change").
			Checkout("main")

		// Don't track branch1
		branch := s.Engine.GetBranch("branch1")
		parent := branch.GetParentOrTrunk()
		require.Equal(t, "main", parent) // Should return trunk
	})
}

func TestIsMergedIntoTrunk(t *testing.T) {
	t.Parallel()
	t.Run("returns false for unmerged branch", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			CreateBranch("branch1").
			Commit("branch1 change").
			Checkout("main")

		merged, err := s.Engine.IsMergedIntoTrunk(context.Background(), "branch1")
		require.NoError(t, err)
		require.False(t, merged)
	})
}

func TestGetDeletionStatusesSingleBranch(t *testing.T) {
	t.Parallel()

	t.Run("never considers trunk for deletion", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		statuses, err := s.Engine.GetDeletionStatuses(context.Background(), []string{"main"})
		require.NoError(t, err)
		status := statuses["main"]
		require.False(t, status.SafeToDelete)
	})

	t.Run("considers merged branch for deletion", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
			})

		// Merge branch1 into main
		s.CheckoutQuiet("main").
			RunGit("merge", "branch1")

		err := s.Engine.Rebuild("main")
		require.NoError(t, err)

		statuses, err := s.Engine.GetDeletionStatuses(context.Background(), []string{"branch1"})
		require.NoError(t, err)
		status := statuses["branch1"]
		require.True(t, status.SafeToDelete)
	})

	t.Run("worktree anchor is never deletable", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
			})

		// Merge branch1 into main so it would normally be deletable
		s.CheckoutQuiet("main").
			RunGit("merge", "branch1")

		err := s.Engine.Rebuild("main")
		require.NoError(t, err)

		// Mark as worktree anchor
		branch := s.Engine.GetBranch("branch1")
		err = s.Engine.SetBranchType(branch, git.BranchTypeWorktreeAnchor)
		require.NoError(t, err)

		statuses, err := s.Engine.GetDeletionStatuses(context.Background(), []string{"branch1"})
		require.NoError(t, err)
		status := statuses["branch1"]
		require.False(t, status.SafeToDelete, "worktree anchor should not be deletable even if merged")
	})
}

func TestGetDeletionStatuses(t *testing.T) {
	t.Parallel()

	t.Run("empty list returns empty map", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		statuses, err := s.Engine.GetDeletionStatuses(context.Background(), []string{})
		require.NoError(t, err)
		require.Empty(t, statuses)
	})

	t.Run("trunk not deletable and merged branch deletable", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "main",
			})

		// Merge branch1 into main (branch2 stays unmerged)
		s.CheckoutQuiet("main").
			RunGit("merge", "branch1")

		err := s.Engine.Rebuild("main")
		require.NoError(t, err)

		statuses, err := s.Engine.GetDeletionStatuses(context.Background(), []string{"main", "branch1", "branch2"})
		require.NoError(t, err)
		require.Len(t, statuses, 3)

		// Trunk is never deletable
		require.False(t, statuses["main"].SafeToDelete)

		// Merged branch is deletable
		require.True(t, statuses["branch1"].SafeToDelete)
		require.Contains(t, statuses["branch1"].Reason, "merged into")

		// Unmerged branch is not deletable
		require.False(t, statuses["branch2"].SafeToDelete)
	})

	t.Run("worktree anchor never deletable even if merged", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
			})

		// Merge branch1 into main
		s.CheckoutQuiet("main").
			RunGit("merge", "branch1")

		err := s.Engine.Rebuild("main")
		require.NoError(t, err)

		// Mark as worktree anchor
		branch := s.Engine.GetBranch("branch1")
		err = s.Engine.SetBranchType(branch, git.BranchTypeWorktreeAnchor)
		require.NoError(t, err)

		statuses, err := s.Engine.GetDeletionStatuses(context.Background(), []string{"branch1"})
		require.NoError(t, err)
		require.False(t, statuses["branch1"].SafeToDelete)
	})
}

func TestIsBranchEmpty(t *testing.T) {
	t.Parallel()
	t.Run("returns false for branch with changes", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			CreateBranch("branch1").
			CommitChange("file1", "branch1 change").
			Checkout("main")

		empty, err := s.Engine.IsBranchEmpty(context.Background(), "branch1")
		require.NoError(t, err)
		require.False(t, empty)
	})

	t.Run("returns true for branch with no changes", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			CreateBranch("branch1").
			Checkout("main")

		empty, err := s.Engine.IsBranchEmpty(context.Background(), "branch1")
		require.NoError(t, err)
		require.True(t, empty)
	})
}

func TestBatchIsBranchEmpty(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		CreateBranch("withchanges").
		CommitChange("file1", "real change").
		Checkout("main").
		CreateBranch("emptycommit").
		Commit("empty commit"). // --allow-empty: tree matches parent
		Checkout("main").
		CreateBranch("nochanges"). // no commit at all: tree matches parent
		Checkout("main")

	result := s.Engine.BatchIsBranchEmpty([]string{"withchanges", "emptycommit", "nochanges"})

	require.False(t, result["withchanges"], "branch with real changes is not empty")
	require.True(t, result["emptycommit"], "branch with an empty commit is empty")
	require.True(t, result["nochanges"], "branch with no commits is empty")
}

func TestUpsertPrInfo(t *testing.T) {
	t.Parallel()
	t.Run("creates PR info for branch", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
			})

		prInfo := testhelpers.NewTestPrInfoFull(
			123,
			"Test PR",
			"Test body",
			"OPEN",
			"main",
			"https://github.com/owner/repo/pull/123",
			false,
		)

		branch := s.Engine.GetBranch("branch1")
		err := s.Engine.UpsertPrInfo(context.Background(), branch, prInfo)
		require.NoError(t, err)

		// Verify PR info
		retrieved, err := branch.GetPrInfo()
		require.NoError(t, err)
		require.NotNil(t, retrieved)
		require.Equal(t, 123, *retrieved.Number())
		require.Equal(t, "Test PR", retrieved.Title())
		require.Equal(t, "Test body", retrieved.Body())
		require.False(t, retrieved.IsDraft())
	})

	t.Run("updates existing PR info", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
			})

		prInfo := testhelpers.NewTestPrInfoWithTitle(123, "Original Title").
			WithMergeBranch("stackit-merge-branch")

		branch := s.Engine.GetBranch("branch1")
		err := s.Engine.UpsertPrInfo(context.Background(), branch, prInfo)
		require.NoError(t, err)

		// Update PR info
		prInfo = prInfo.WithTitleAndBody("Updated Title", "Updated body")
		err = s.Engine.UpsertPrInfo(context.Background(), branch, prInfo)
		require.NoError(t, err)

		// Verify updated PR info
		retrieved, err := branch.GetPrInfo()
		require.NoError(t, err)
		require.NotNil(t, retrieved)
		require.Equal(t, "Updated Title", retrieved.Title())
		require.Equal(t, "Updated body", retrieved.Body())
		require.Equal(t, "stackit-merge-branch", retrieved.MergeBranch())
	})
}

func TestReset(t *testing.T) {
	t.Parallel()
	t.Run("resets engine with new trunk", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
			})

		// Reset with same trunk
		err := s.Engine.Reset("main")
		require.NoError(t, err)

		// Branch should still exist but not be tracked
		allBranches := s.Engine.AllBranches()
		require.Contains(t, allBranches.Names(), "branch1")
		require.False(t, s.Engine.GetBranch("branch1").IsTracked())
	})
}

func TestConcurrentAccess(t *testing.T) {
	t.Parallel()
	t.Run("handles concurrent reads safely", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
			})

		// Concurrent reads should be safe
		done := make(chan bool, 10)
		for range 10 {
			go func() {
				branch := s.Engine.GetBranch("branch1")
				_ = branch.GetParent()
				graph := engine.BuildStackGraph(s.Engine, engine.SortStrategyAlphabetical, nil)
				_ = graph.Children(s.Engine.Trunk())
				_ = s.Engine.GetBranch("branch1").IsTracked()
				_ = s.Engine.AllBranches()
				done <- true
			}()
		}

		// Wait for all goroutines
		for range 10 {
			<-done
		}
	})
}

func TestBranchRemoteStatuses(t *testing.T) {
	t.Parallel()

	getBranchStatus := func(s *scenario.Scenario, name string) engine.BranchRemoteStatus {
		branch := s.Engine.GetBranch(name)
		return s.Engine.ReadBranchRemoteStatuses(context.Background(), engine.BranchesOf(branch)).ForBranch(branch)
	}

	t.Run("returns true when branch matches remote", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		_, err := s.Scene.Repo.CreateBareRemote("origin")
		require.NoError(t, err)
		err = s.Scene.Repo.PushBranch("origin", "main")
		require.NoError(t, err)

		s.CreateBranch("feature").Commit("feature change")
		err = s.Scene.Repo.PushBranch("origin", "feature")
		require.NoError(t, err)
		s.Checkout("main")

		status := getBranchStatus(s, "feature")
		require.True(t, status.Matches(), "branch should match remote after push")
		require.False(t, status.Ahead())
		require.False(t, status.Behind())
		require.False(t, status.Diverged())
	})

	t.Run("returns false when branch has local changes", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		_, err := s.Scene.Repo.CreateBareRemote("origin")
		require.NoError(t, err)
		err = s.Scene.Repo.PushBranch("origin", "main")
		require.NoError(t, err)

		s.CreateBranch("feature").Commit("feature change")
		err = s.Scene.Repo.PushBranch("origin", "feature")
		require.NoError(t, err)
		s.Commit("local change").Checkout("main")

		status := getBranchStatus(s, "feature")
		require.False(t, status.Matches(), "branch should not match remote with local changes")
		require.True(t, status.Ahead())
		require.False(t, status.Behind())
		require.False(t, status.Diverged())
	})

	t.Run("returns false when branch does not exist on remote", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		_, err := s.Scene.Repo.CreateBareRemote("origin")
		require.NoError(t, err)
		err = s.Scene.Repo.PushBranch("origin", "main")
		require.NoError(t, err)

		s.CreateBranch("feature").Commit("feature change").Checkout("main")

		status := getBranchStatus(s, "feature")
		require.False(t, status.Matches(), "branch should not match when it doesn't exist on remote")
		require.True(t, status.MissingRemote())
	})

	t.Run("returns false after amend (branch diverged)", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		_, err := s.Scene.Repo.CreateBareRemote("origin")
		require.NoError(t, err)
		err = s.Scene.Repo.PushBranch("origin", "main")
		require.NoError(t, err)

		s.CreateBranch("feature").Commit("feature change")
		err = s.Scene.Repo.PushBranch("origin", "feature")
		require.NoError(t, err)

		err = s.Scene.Repo.CreateChangeAndAmend("amended change", "amended")
		require.NoError(t, err)
		s.Checkout("main")

		status := getBranchStatus(s, "feature")
		require.False(t, status.Matches(), "branch should not match remote after amend")
		require.True(t, status.Diverged())
		require.False(t, status.Ahead())
		require.False(t, status.Behind())
	})
}

func TestReadBranchRemoteStatuses(t *testing.T) {
	t.Parallel()
	t.Run("reads statuses for all requested remote branches", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// Create a bare remote
		_, err := s.Scene.Repo.CreateBareRemote("origin")
		require.NoError(t, err)

		// Push main
		err = s.Scene.Repo.PushBranch("origin", "main")
		require.NoError(t, err)

		// Create and push multiple branches - checkout main between each
		s.CreateBranch("feature1").
			Commit("feature1 change")
		err = s.Scene.Repo.PushBranch("origin", "feature1")
		require.NoError(t, err)
		s.Checkout("main")

		s.CreateBranch("feature2").
			Commit("feature2 change")
		err = s.Scene.Repo.PushBranch("origin", "feature2")
		require.NoError(t, err)
		s.Checkout("main")

		branches := engine.BranchesOf(
			s.Engine.GetBranch("main"),
			s.Engine.GetBranch("feature1"),
			s.Engine.GetBranch("feature2"),
		)
		statuses := s.Engine.ReadBranchRemoteStatuses(context.Background(), branches)
		for _, branchName := range []string{"main", "feature1", "feature2"} {
			status := statuses[branchName]
			require.True(t, status.Matches(), "branch %s should match remote", branchName)
		}
	})

	t.Run("handles empty remote gracefully", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// Create a bare remote but don't push anything
		_, err := s.Scene.Repo.CreateBareRemote("origin")
		require.NoError(t, err)

		// Branches should not match (nothing on remote)
		main := s.Engine.GetBranch("main")
		status := s.Engine.ReadBranchRemoteStatuses(context.Background(), engine.BranchesOf(main)).ForBranch(main)
		require.False(t, status.Matches(), "main should not match empty remote")
	})

	t.Run("computes common ancestor for multiple diverged branches concurrently", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		_, err := s.Scene.Repo.CreateBareRemote("origin")
		require.NoError(t, err)
		err = s.Scene.Repo.PushBranch("origin", "main")
		require.NoError(t, err)

		// Push feature1 and feature2, then diverge each with a local-only commit
		// so every branch needs its own merge-base computation.
		s.CreateBranch("feature1").Commit("feature1 base")
		err = s.Scene.Repo.PushBranch("origin", "feature1")
		require.NoError(t, err)
		s.Commit("feature1 local-only change")

		s.Checkout("main")
		s.CreateBranch("feature2").Commit("feature2 base")
		err = s.Scene.Repo.PushBranch("origin", "feature2")
		require.NoError(t, err)
		s.Commit("feature2 local-only change")

		branches := engine.BranchesOf(
			s.Engine.GetBranch("feature1"),
			s.Engine.GetBranch("feature2"),
		)
		statuses := s.Engine.ReadBranchRemoteStatuses(context.Background(), branches)

		for _, branchName := range []string{"feature1", "feature2"} {
			status := statuses[branchName]
			require.False(t, status.Matches(), "branch %s should have diverged from remote", branchName)
			require.NotEmpty(t, status.CommonAncestor, "branch %s should have a common ancestor with remote", branchName)
			require.NotEqual(t, status.LocalSha, status.CommonAncestor, "branch %s local sha should be ahead of the common ancestor", branchName)
			require.Equal(t, status.RemoteSha, status.CommonAncestor, "branch %s common ancestor should be the pushed remote sha", branchName)
		}
	})
}

func TestTrunkRemoteState(t *testing.T) {
	t.Parallel()

	// setRemoteTrunk points refs/remotes/origin/main at the given ref (resolved to
	// a SHA), simulating a fetched remote-tracking trunk without any network.
	setRemoteTrunk := func(t *testing.T, s *scenario.Scenario, ref string) {
		t.Helper()
		sha, err := s.Scene.Repo.RunGitCommandAndGetOutput("rev-parse", "--verify", ref)
		require.NoError(t, err)
		require.NoError(t, s.Scene.Repo.RunGitCommand("update-ref", "refs/remotes/origin/main", strings.TrimSpace(sha)))
	}

	t.Run("no remote-tracking ref is not guarded", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		state := s.Engine.TrunkRemoteState(context.Background())
		require.False(t, state.HasRemoteRef, "with no origin/main the guard must stay inert")
		require.False(t, state.AheadOrDiverged)
	})

	t.Run("trunk equal to remote is not ahead", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		setRemoteTrunk(t, s, "main")

		state := s.Engine.TrunkRemoteState(context.Background())
		require.True(t, state.HasRemoteRef)
		require.False(t, state.AheadOrDiverged, "synced trunk must not be flagged")
		require.Equal(t, state.LocalSha, state.RemoteSha)
		require.Equal(t, "origin/main", state.RemoteRef)
	})

	t.Run("trunk ahead of remote is flagged", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		setRemoteTrunk(t, s, "main")

		// Local main advances past the recorded remote tip (un-pushed commit).
		s.Commit("un-pushed trunk commit")

		state := s.Engine.TrunkRemoteState(context.Background())
		require.True(t, state.HasRemoteRef)
		require.True(t, state.AheadOrDiverged, "local trunk ahead of origin/main must be flagged")
		require.NotEqual(t, state.LocalSha, state.RemoteSha)
	})

	t.Run("trunk behind remote is not flagged", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// Build a descendant commit and point origin/main at it, leaving local main
		// strictly behind (still an ancestor of the remote).
		require.NoError(t, s.Scene.Repo.RunGitCommand("branch", "remote-ahead", "main"))
		require.NoError(t, s.Scene.Repo.CheckoutBranch("remote-ahead"))
		s.Commit("landed on origin")
		setRemoteTrunk(t, s, "remote-ahead")
		require.NoError(t, s.Scene.Repo.CheckoutBranch("main"))

		state := s.Engine.TrunkRemoteState(context.Background())
		require.True(t, state.HasRemoteRef)
		require.False(t, state.AheadOrDiverged, "trunk behind origin/main builds only on remote commits")
	})

	t.Run("trunk diverged from remote is flagged", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// Common ancestor = current main tip.
		base, err := s.Scene.Repo.RunGitCommandAndGetOutput("rev-parse", "--verify", "main")
		require.NoError(t, err)
		base = strings.TrimSpace(base)

		// Remote-only commit on top of the common ancestor.
		require.NoError(t, s.Scene.Repo.RunGitCommand("branch", "remote-work", base))
		require.NoError(t, s.Scene.Repo.CheckoutBranch("remote-work"))
		s.Commit("landed on origin only")
		setRemoteTrunk(t, s, "remote-work")
		require.NoError(t, s.Scene.Repo.CheckoutBranch("main"))

		// Local main gets its own divergent commit on the same ancestor.
		s.Commit("local divergent commit")

		state := s.Engine.TrunkRemoteState(context.Background())
		require.True(t, state.HasRemoteRef)
		require.True(t, state.AheadOrDiverged, "diverged trunk must be flagged like the ahead case")
	})
}

func TestEdgeCases(t *testing.T) {
	t.Parallel()
	t.Run("handles branch with no parent gracefully", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			CreateBranch("branch1").
			Commit("branch1 change").
			Checkout("main")

		// Branch exists but not tracked
		branch := s.Engine.GetBranch("branch1")
		parent := branch.GetParent()
		require.Empty(t, parent)

		// GetParentOrTrunk should return trunk
		// branch already declared above
		parentName := branch.GetParentOrTrunk()
		require.Equal(t, "main", parentName)
	})

	t.Run("handles multiple children correctly", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// Create multiple branches from main
		branchNames := []string{"branch1", "branch2", "branch3", "branch4", "branch5"}
		for _, branchName := range branchNames {
			s.CreateBranch(branchName).
				Commit(branchName + " change").
				Checkout("main")
		}

		// Track all branches
		for _, branchName := range branchNames {
			err := s.Engine.TrackBranch(context.Background(), branchName, "main")
			require.NoError(t, err)
		}

		// Verify all are children of main
		graph := engine.BuildStackGraph(s.Engine, engine.SortStrategyAlphabetical, nil)
		children := graph.Children(s.Engine.Trunk())
		require.Len(t, children, 5)
	})
}

func TestDetachAndResetBranchChanges(t *testing.T) {
	t.Parallel()
	t.Run("detaches and soft resets to parent merge base", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, nil)
		err := s.Scene.Repo.CreateChangeAndCommit("initial content", "shared")
		require.NoError(t, err)

		// Create feature branch that modifies the existing file
		s.CreateBranch("feature")
		err = s.Scene.Repo.CreateChangeAndCommit("feature content", "shared")
		require.NoError(t, err)

		// Get the main branch commit (merge base)
		mainCommit, _ := s.Scene.Repo.GetRevision("main")

		// Track branch
		err = s.Engine.TrackBranch(context.Background(), "feature", "main")
		require.NoError(t, err)

		// Call DetachAndResetBranchChanges
		err = s.Engine.DetachAndResetBranchChanges(context.Background(), "feature")
		require.NoError(t, err)

		// Verify HEAD is detached
		currentBranch := s.Engine.CurrentBranch()
		require.Nil(t, currentBranch, "should be in detached HEAD state")

		// Verify we're at the merge base commit
		headCommit, _ := s.Scene.Repo.GetRevision("HEAD")
		require.Equal(t, mainCommit, headCommit, "HEAD should be at parent merge base")

		// Verify the feature changes are now unstaged (modified tracked file)
		hasUnstaged, _ := s.Scene.Repo.HasUnstagedChanges()
		require.True(t, hasUnstaged, "feature changes should appear as unstaged")
	})

	t.Run("works with multi-commit branch modifying same file", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, nil)
		err := s.Scene.Repo.CreateChangeAndCommit("initial", "shared")
		require.NoError(t, err)

		// Create feature branch with multiple commits modifying the same file
		s.CreateBranch("feature").
			CommitChange("shared", "commit 1").
			CommitChange("shared", "commit 2").
			CommitChange("shared", "commit 3")

		// Get main commit
		mainCommit, _ := s.Scene.Repo.GetRevision("main")

		// Track branch
		err = s.Engine.TrackBranch(context.Background(), "feature", "main")
		require.NoError(t, err)

		// Call DetachAndResetBranchChanges
		err = s.Engine.DetachAndResetBranchChanges(context.Background(), "feature")
		require.NoError(t, err)

		// Verify we're at main
		headCommit, _ := s.Scene.Repo.GetRevision("HEAD")
		require.Equal(t, mainCommit, headCommit)

		// Verify changes are unstaged
		hasUnstaged, _ := s.Scene.Repo.HasUnstagedChanges()
		require.True(t, hasUnstaged)
	})

	t.Run("works with stacked branches", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, nil)
		err := s.Scene.Repo.CreateChangeAndCommit("initial", "shared")
		require.NoError(t, err)

		// Create branch1 on main
		s.CreateBranch("branch1").
			CommitChange("shared", "branch1 change")

		// Get branch1 commit (this will be the merge base for branch2)
		branch1Commit, _ := s.Scene.Repo.GetRevision("branch1")

		// Create branch2 on branch1
		s.CreateBranch("branch2").
			CommitChange("shared", "branch2 change")

		// Track branches
		err = s.Engine.TrackBranch(context.Background(), "branch1", "main")
		require.NoError(t, err)
		err = s.Engine.TrackBranch(context.Background(), "branch2", "branch1")
		require.NoError(t, err)

		// Call DetachAndResetBranchChanges on branch2
		err = s.Engine.DetachAndResetBranchChanges(context.Background(), "branch2")
		require.NoError(t, err)

		// Verify we're at branch1's commit (the parent)
		headCommit, _ := s.Scene.Repo.GetRevision("HEAD")
		require.Equal(t, branch1Commit, headCommit, "HEAD should be at branch1 (parent of branch2)")

		// Verify branch2 changes are unstaged
		hasUnstaged, _ := s.Scene.Repo.HasUnstagedChanges()
		require.True(t, hasUnstaged)
	})

	t.Run("handles untracked branch using trunk as parent", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, nil)
		err := s.Scene.Repo.CreateChangeAndCommit("initial", "shared")
		require.NoError(t, err)

		// Create feature branch (not tracked)
		s.CreateBranch("feature").
			CommitChange("shared", "feature change")

		mainCommit, _ := s.Scene.Repo.GetRevision("main")

		// Call DetachAndResetBranchChanges without tracking
		err = s.Engine.DetachAndResetBranchChanges(context.Background(), "feature")
		require.NoError(t, err)

		// Should use trunk (main) as the parent
		headCommit, _ := s.Scene.Repo.GetRevision("HEAD")
		require.Equal(t, mainCommit, headCommit, "should use trunk as parent for untracked branch")

		// Verify changes are unstaged
		hasUnstaged, _ := s.Scene.Repo.HasUnstagedChanges()
		require.True(t, hasUnstaged)
	})

	t.Run("handles new files as untracked", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// Create feature branch with a NEW file (doesn't exist on main)
		s.CreateBranch("feature")
		err := s.Scene.Repo.CreateChangeAndCommit("new file content", "newfile")
		require.NoError(t, err)

		mainCommit, _ := s.Scene.Repo.GetRevision("main")

		// Track branch
		err = s.Engine.TrackBranch(context.Background(), "feature", "main")
		require.NoError(t, err)

		// Call DetachAndResetBranchChanges
		err = s.Engine.DetachAndResetBranchChanges(context.Background(), "feature")
		require.NoError(t, err)

		// Verify we're at main
		headCommit, _ := s.Scene.Repo.GetRevision("HEAD")
		require.Equal(t, mainCommit, headCommit)

		// New files should appear as untracked (not unstaged)
		hasUntracked, _ := s.Scene.Repo.HasUntrackedFiles()
		require.True(t, hasUntracked, "new files should appear as untracked")
	})
}

func TestSetParentScenarios(t *testing.T) {
	t.Parallel()
	t.Run("preserves divergence point when multi-commit parent was squash-merged", func(t *testing.T) {
		t.Parallel()
		// Scenario: main -> branch1 (3 commits) -> branch2. GitHub
		// squash-merges branch1, so no individual commit patch-matches the
		// squashed result and patch-id detection (git cherry) cannot see the
		// merge. Only the MERGED PR state in metadata can.
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		s.CreateBranch("branch1").
			CommitChange("file1.txt", "v1").
			CommitChange("file1.txt", "v1\nv2").
			CommitChange("file1.txt", "v1\nv2\nv3").
			TrackBranch("branch1", "main")

		branch1SHA, _ := s.Engine.GetBranch("branch1").GetRevision()

		s.CreateBranch("branch2").
			CommitChange("file2.txt", "feat: branch2").
			TrackBranch("branch2", "branch1")

		// Squash commit lands on main; record the MERGED PR state.
		s.Checkout("main").
			CommitChange("file1.txt", "v1\nv2\nv3")

		meta, err := s.Engine.Git().ReadMetadata("branch1")
		require.NoError(t, err)
		prNumber := 1
		state := git.PRStateMerged
		base := "main"
		meta = meta.WithPrInfo(&git.PrInfoPersistence{Number: &prNumber, State: &state, Base: &base})
		require.NoError(t, s.Engine.Git().WriteMetadata("branch1", meta))
		s.Rebuild()

		// Reparent branch2 to main (what restack/sync do after the merge).
		err = s.Engine.SetParent(context.Background(), s.Engine.GetBranch("branch2"), s.Engine.GetBranch("main"), engine.DivergenceRecompute)
		require.NoError(t, err)

		// The divergence point must stay at branch1's pre-merge tip so a
		// restack replays only branch2's own commits, not branch1's.
		meta2, err := s.Engine.Git().ReadMetadata("branch2")
		require.NoError(t, err)
		require.Equal(t, branch1SHA, *meta2.GetParentBranchRevision(),
			"divergence point should be preserved for a squash-merged parent")
	})

	t.Run("preserves divergence point when parent is rebased and merged into trunk", func(t *testing.T) {
		t.Parallel()
		// Scenario: main -> branch1 -> branch2
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// 1. Create branch1
		s.CreateBranch("branch1").
			CommitChange("file1.txt", "feat: branch1").
			TrackBranch("branch1", "main")

		branch1OriginalSHA, _ := s.Engine.GetBranch("branch1").GetRevision()

		// 2. Create branch2 on top of branch1
		s.CreateBranch("branch2").
			CommitChange("file2.txt", "feat: branch2").
			TrackBranch("branch2", "branch1")

		// branch2 diverged from branch1 at branch1OriginalSHA

		// 3. Move main forward
		s.Checkout("main").
			CommitChange("main.txt", "feat: main")

		// 4. Rebase branch1 onto main (changing its SHA)
		s.Checkout("branch1")
		branch1 := s.Engine.GetBranch("branch1")
		_, _ = s.Engine.RestackBranches(context.Background(), engine.BranchesOf(branch1))
		branch1NewSHA, _ := s.Engine.GetBranch("branch1").GetRevision()
		require.NotEqual(t, branch1OriginalSHA, branch1NewSHA)

		// 5. Merge branch1 into main
		s.Checkout("main")
		s.RunGit("merge", "branch1", "--no-ff", "-m", "Merge branch1")

		// 6. Reparent branch2 to main (what happens during 'stackit merge' or 'stackit sync')
		err := s.Engine.SetParent(context.Background(), s.Engine.GetBranch("branch2"), s.Engine.GetBranch("main"), engine.DivergenceRecompute)
		require.NoError(t, err)

		// VERIFY: ParentBranchRevision should still be branch1OriginalSHA
		// because it's a valid ancestor and the old parent (branch1) was merged into main.
		meta, _ := s.Engine.Git().ReadMetadata("branch2")
		require.Equal(t, branch1OriginalSHA, *meta.GetParentBranchRevision(), "Divergence point should be preserved to avoid conflicts during restack")
	})

	t.Run("updates divergence point when parent is folded into child (upward merge)", func(t *testing.T) {
		t.Parallel()
		// Scenario: main -> branch1 -> branch2
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// 1. Setup stack
		s.CreateBranch("branch1").
			CommitChange("file1.txt", "feat: branch1").
			TrackBranch("branch1", "main")

		s.CreateBranch("branch2").
			CommitChange("file2.txt", "feat: branch2").
			TrackBranch("branch2", "branch1")

		// 2. Fold branch1 into branch2 (upward merge)
		s.Checkout("branch2")
		s.RunGit("merge", "branch1", "--no-ff", "-m", "Merge branch1 into branch2")

		// 3. Reparent branch2 to main (branch1 will be deleted in a real fold)
		err := s.Engine.SetParent(context.Background(), s.Engine.GetBranch("branch2"), s.Engine.GetBranch("main"), engine.DivergenceRecompute)
		require.NoError(t, err)

		// VERIFY: ParentBranchRevision should be updated to main's tip
		// because branch1 was NOT merged into main; it was merged into branch2.
		// If we kept the old divergence point (before branch1), a restack would
		// try to re-apply branch1's changes which are already in branch2.
		mainSHA, _ := s.Engine.Trunk().GetRevision()
		meta, _ := s.Engine.Git().ReadMetadata("branch2")
		require.Equal(t, mainSHA, *meta.GetParentBranchRevision(), "Divergence point should be updated to new parent when folding upward")
	})

	t.Run("updates divergence point after manual rebase onto same parent", func(t *testing.T) {
		t.Parallel()
		// Scenario: main -> branch1
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		s.CreateBranch("branch1").
			CommitChange("file1.txt", "feat: branch1").
			TrackBranch("branch1", "main")

		originalMeta, _ := s.Engine.Git().ReadMetadata("branch1")

		// 1. Move main forward
		s.Checkout("main").
			CommitChange("main.txt", "feat: main")
		mainNewSHA, _ := s.Engine.Trunk().GetRevision()

		// 2. Manually rebase branch1 onto main
		s.Checkout("branch1")
		s.RunGit("rebase", "main")

		// 3. Call SetParent with the same parent (main)
		err := s.Engine.SetParent(context.Background(), s.Engine.GetBranch("branch1"), s.Engine.GetBranch("main"), engine.DivergenceRecompute)
		require.NoError(t, err)

		// VERIFY: ParentBranchRevision should be updated to mainNewSHA
		// because the branch has moved forward relative to its parent.
		meta, _ := s.Engine.Git().ReadMetadata("branch1")
		require.Equal(t, mainNewSHA, *meta.GetParentBranchRevision())
		require.NotEqual(t, *originalMeta.GetParentBranchRevision(), *meta.GetParentBranchRevision())
	})

	t.Run("reparent fails when divergence point cannot be determined", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		s.CreateBranch("branch1").
			CommitChange("file1.txt", "feat: branch1").
			TrackBranch("branch1", "main")
		s.CreateBranch("branch2").
			CommitChange("file2.txt", "feat: branch2").
			TrackBranch("branch2", "branch1")

		missingParent := "missing-parent"
		meta, err := s.Engine.Git().ReadMetadata("branch2")
		require.NoError(t, err)
		meta = meta.WithParentBranchName(&missingParent).WithParentBranchRevision(nil)
		require.NoError(t, s.Engine.Git().WriteMetadata("branch2", meta))
		require.NoError(t, s.Engine.Rebuild("main"))

		err = s.Engine.ReparentBranch(context.Background(), s.Engine.GetBranch("branch2"), s.Engine.GetBranch("main"))
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to determine divergence point for branch2")

		meta, err = s.Engine.Git().ReadMetadata("branch2")
		require.NoError(t, err)
		require.Equal(t, missingParent, *meta.GetParentBranchName())
		require.Nil(t, meta.GetParentBranchRevision())
	})
}

func TestFrozenBranches(t *testing.T) {
	t.Parallel()
	t.Run("set and check frozen status", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		s.CreateBranch("feature").
			Commit("feature change").
			Checkout("main")

		err := s.Engine.TrackBranch(context.Background(), "feature", "main")
		require.NoError(t, err)

		branch := s.Engine.GetBranch("feature")
		require.False(t, branch.IsFrozen())

		_, err = s.Engine.SetFrozen(context.Background(), engine.BranchesOf(branch), true)
		require.NoError(t, err)
		require.True(t, s.Engine.GetBranch("feature").IsFrozen())

		_, err = s.Engine.SetFrozen(context.Background(), engine.BranchesOf(branch), false)
		require.NoError(t, err)
		require.False(t, s.Engine.GetBranch("feature").IsFrozen())
	})

	t.Run("canModify helper respects frozen status", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		s.CreateBranch("feature").
			Commit("feature change").
			Checkout("main")

		err := s.Engine.TrackBranch(context.Background(), "feature", "main")
		require.NoError(t, err)

		branch := s.Engine.GetBranch("feature")
		require.True(t, branch.CanModify())

		// Test frozen
		_, err = s.Engine.SetFrozen(context.Background(), engine.BranchesOf(branch), true)
		require.NoError(t, err)
		require.False(t, s.Engine.GetBranch("feature").CanModify())

		// Test locked
		_, err = s.Engine.SetFrozen(context.Background(), engine.BranchesOf(branch), false)
		require.NoError(t, err)
		_, err = s.Engine.SetLocked(context.Background(), engine.BranchesOf(branch), engine.LockReasonUser)
		require.NoError(t, err)
		require.False(t, s.Engine.GetBranch("feature").CanModify())
	})

	t.Run("EnsureCanModify returns proper error type for frozen branch", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		s.CreateBranch("feature").
			Commit("feature change").
			Checkout("main")

		err := s.Engine.TrackBranch(context.Background(), "feature", "main")
		require.NoError(t, err)

		branch := s.Engine.GetBranch("feature")

		// Unmodified branch should not error
		err = branch.EnsureCanModify()
		require.NoError(t, err)

		// Freeze the branch
		_, err = s.Engine.SetFrozen(context.Background(), engine.BranchesOf(branch), true)
		require.NoError(t, err)

		// Now EnsureCanModify should return an error
		branch = s.Engine.GetBranch("feature")
		err = branch.EnsureCanModify()
		require.Error(t, err)

		// Error should match sentinel
		require.ErrorIs(t, err, errors.ErrBranchModificationRestricted)

		// Error should be extractable as BranchModificationError
		var modErr *errors.BranchModificationError
		require.ErrorAs(t, err, &modErr)
		require.Equal(t, "feature", modErr.BranchName)
		require.False(t, modErr.IsLocked())
		require.True(t, modErr.IsFrozen)
	})

	t.Run("EnsureCanModify returns proper error type for locked branch", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		s.CreateBranch("feature").
			Commit("feature change").
			Checkout("main")

		err := s.Engine.TrackBranch(context.Background(), "feature", "main")
		require.NoError(t, err)

		branch := s.Engine.GetBranch("feature")

		// Lock the branch
		_, err = s.Engine.SetLocked(context.Background(), engine.BranchesOf(branch), engine.LockReasonUser)
		require.NoError(t, err)

		// Now EnsureCanModify should return an error
		branch = s.Engine.GetBranch("feature")
		err = branch.EnsureCanModify()
		require.Error(t, err)

		// Error should match sentinel
		require.ErrorIs(t, err, errors.ErrBranchModificationRestricted)

		// Error should be extractable as BranchModificationError
		var modErr *errors.BranchModificationError
		require.ErrorAs(t, err, &modErr)
		require.Equal(t, "feature", modErr.BranchName)
		require.True(t, modErr.IsLocked())
		require.False(t, modErr.IsFrozen)
	})

	t.Run("EnsureCanModify returns proper error type for locked and frozen branch", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		s.CreateBranch("feature").
			Commit("feature change").
			Checkout("main")

		err := s.Engine.TrackBranch(context.Background(), "feature", "main")
		require.NoError(t, err)

		branch := s.Engine.GetBranch("feature")

		// Lock and freeze the branch
		_, err = s.Engine.SetLocked(context.Background(), engine.BranchesOf(branch), engine.LockReasonUser)
		require.NoError(t, err)
		_, err = s.Engine.SetFrozen(context.Background(), engine.BranchesOf(branch), true)
		require.NoError(t, err)

		// Now EnsureCanModify should return an error
		branch = s.Engine.GetBranch("feature")
		err = branch.EnsureCanModify()
		require.Error(t, err)

		// Error should be extractable as BranchModificationError with both flags
		var modErr *errors.BranchModificationError
		require.ErrorAs(t, err, &modErr)
		require.Equal(t, "feature", modErr.BranchName)
		require.True(t, modErr.IsLocked())
		require.True(t, modErr.IsFrozen)

		// Error message should mention both states
		require.Contains(t, err.Error(), "locked (user) and frozen")
	})

	t.Run("frozen status persists after engine rebuild", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		s.CreateBranch("feature").
			Commit("feature change").
			Checkout("main")

		err := s.Engine.TrackBranch(context.Background(), "feature", "main")
		require.NoError(t, err)

		branch := s.Engine.GetBranch("feature")
		_, err = s.Engine.SetFrozen(context.Background(), engine.BranchesOf(branch), true)
		require.NoError(t, err)
		require.True(t, s.Engine.GetBranch("feature").IsFrozen())

		// Rebuild the engine
		err = s.Engine.Rebuild("main")
		require.NoError(t, err)

		// Frozen status should persist
		require.True(t, s.Engine.GetBranch("feature").IsFrozen())
	})
}
