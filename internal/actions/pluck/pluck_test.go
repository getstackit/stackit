package pluck

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/testhelpers"
	"stackit.dev/stackit/testhelpers/scenario"
)

func TestPluckAction(t *testing.T) {
	t.Run("plucks branch to new parent and reparents children to grandparent", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
				"branch3": "branch2", // Child of branch2
			})

		// Pluck branch2 from branch1 to main
		// branch3 should be reparented to branch1 (the grandparent)
		err := Action(s.Context, Options{
			Source: "branch2",
			Onto:   "main",
		}, nil)
		require.NoError(t, err)

		// Verify branch2 is now on main
		branch2 := s.Engine.GetBranch("branch2")
		parent2 := branch2.GetParent()
		require.NotNil(t, parent2)
		require.Equal(t, "main", parent2.GetName())

		// Verify branch3 was reparented to branch1 (not main!)
		branch3 := s.Engine.GetBranch("branch3")
		parent3 := branch3.GetParent()
		require.NotNil(t, parent3)
		require.Equal(t, "branch1", parent3.GetName())
	})

	t.Run("plucks branch with no children", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
			})

		// Pluck branch2 (leaf branch) to main
		err := Action(s.Context, Options{
			Source: "branch2",
			Onto:   "main",
		}, nil)
		require.NoError(t, err)

		// Verify branch2 is now on main
		branch2 := s.Engine.GetBranch("branch2")
		parent2 := branch2.GetParent()
		require.NotNil(t, parent2)
		require.Equal(t, "main", parent2.GetName())

		// branch1 should still be on main
		branch1 := s.Engine.GetBranch("branch1")
		parent1 := branch1.GetParent()
		require.NotNil(t, parent1)
		require.Equal(t, "main", parent1.GetName())
	})

	t.Run("plucks branch with multiple children", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
				"child2a": "branch2",
				"child2b": "branch2",
				"grandch": "child2a",
				"branchX": "main",
			})

		// Pluck branch2 from branch1 to branchX
		// child2a and child2b should be reparented to branch1 (grandparent)
		// grandch should stay on child2a
		err := Action(s.Context, Options{
			Source: "branch2",
			Onto:   "branchX",
		}, nil)
		require.NoError(t, err)

		// Verify branch2 is now on branchX
		branch2 := s.Engine.GetBranch("branch2")
		parent2 := branch2.GetParent()
		require.NotNil(t, parent2)
		require.Equal(t, "branchX", parent2.GetName())

		// Verify child2a is on branch1
		child2a := s.Engine.GetBranch("child2a")
		parentC2a := child2a.GetParent()
		require.NotNil(t, parentC2a)
		require.Equal(t, "branch1", parentC2a.GetName())

		// Verify child2b is on branch1
		child2b := s.Engine.GetBranch("child2b")
		parentC2b := child2b.GetParent()
		require.NotNil(t, parentC2b)
		require.Equal(t, "branch1", parentC2b.GetName())

		// Verify grandch is still on child2a (unchanged)
		grandch := s.Engine.GetBranch("grandch")
		parentGC := grandch.GetParent()
		require.NotNil(t, parentGC)
		require.Equal(t, "child2a", parentGC.GetName())
	})

	t.Run("defaults source to current branch", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
			})

		// Switch to branch2
		s.Checkout("branch2")

		// Pluck without specifying source
		err := Action(s.Context, Options{
			Source: "",
			Onto:   "main",
		}, nil)
		require.NoError(t, err)

		// Verify branch2 was plucked
		branch2 := s.Engine.GetBranch("branch2")
		parent2 := branch2.GetParent()
		require.NotNil(t, parent2)
		require.Equal(t, "main", parent2.GetName())
	})

	t.Run("prevents plucking trunk branch", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		err := Action(s.Context, Options{
			Source: "main",
			Onto:   "main",
		}, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot pluck trunk branch")
	})

	t.Run("prevents plucking onto itself", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
			})

		err := Action(s.Context, Options{
			Source: "branch1",
			Onto:   "branch1",
		}, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot pluck branch onto itself")
	})

	t.Run("prevents plucking onto descendant (cycle detection)", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
				"branch3": "branch2",
			})

		// Try to pluck branch1 onto branch3 (which is a descendant)
		err := Action(s.Context, Options{
			Source: "branch1",
			Onto:   "branch3",
		}, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot pluck")
		require.Contains(t, err.Error(), "onto its own descendant")
	})

	t.Run("fails when source branch is not tracked", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			CreateBranch("untracked").
			Checkout("main")

		err := Action(s.Context, Options{
			Source: "untracked",
			Onto:   "main",
		}, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "not tracked")
	})

	t.Run("fails when onto branch does not exist", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
			})

		err := Action(s.Context, Options{
			Source: "branch1",
			Onto:   "nonexistent",
		}, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "does not exist")
	})

	t.Run("fails when onto is not specified", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
			})

		err := Action(s.Context, Options{
			Source: "branch1",
			Onto:   "",
		}, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "onto branch must be specified")
	})

	t.Run("fails when not on branch and no source specified", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// Detach HEAD
		s.RunGit("checkout", "HEAD~0").Rebuild()

		err := Action(s.Context, Options{
			Source: "",
			Onto:   "main",
		}, nil)
		require.Error(t, err)
		errMsg := err.Error()
		require.True(t,
			strings.Contains(errMsg, "not on a branch") || strings.Contains(errMsg, "cannot pluck trunk branch"),
			"error should mention not on a branch or trunk: %s", errMsg)
	})

	t.Run("restacks all affected branches after pluck", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
				"branch3": "branch2",
			})

		// Add a commit to main
		s.Checkout("main").
			Commit("main update")

		// Pluck branch2 to main
		err := Action(s.Context, Options{
			Source: "branch2",
			Onto:   "main",
		}, nil)
		require.NoError(t, err)

		// Verify all affected branches are restacked (fixed)
		s.ExpectBranchFixed("branch2")
		s.ExpectBranchFixed("branch3")
	})

	t.Run("preserves commits on plucked branch", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// Create branch1 with a commit
		s.Checkout("main").CreateBranch("branch1").Commit("commit in branch1")
		s.TrackBranch("branch1", "main")

		// Create branch2 with its own commit
		s.Checkout("branch1").CreateBranch("branch2").Commit("commit in branch2")
		s.TrackBranch("branch2", "branch1")

		// Pluck branch2 to main
		err := Action(s.Context, Options{
			Source: "branch2",
			Onto:   "main",
		}, nil)
		require.NoError(t, err)

		// Verify branch2 only has its own commit
		branch2Commits, err := s.Engine.GetAllCommits(s.Engine.GetBranch("branch2"), engine.CommitFormatSubject)
		require.NoError(t, err)
		require.Equal(t, 1, len(branch2Commits))
		require.Equal(t, "commit in branch2", branch2Commits[0])
	})

	t.Run("preserves PR information after pluck", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
			})

		// Add PR info to branch2
		branch2 := s.Engine.GetBranch("branch2")
		prNumber := 456
		prInfo := engine.NewPrInfo(&prNumber, "Test Pluck PR", "Test Body", "OPEN", "branch1", "https://github.com/owner/repo/pull/456", false)
		err := s.Engine.UpsertPrInfo(branch2, prInfo)
		require.NoError(t, err)

		// Pluck branch2 to main
		err = Action(s.Context, Options{
			Source: "branch2",
			Onto:   "main",
		}, nil)
		require.NoError(t, err)

		// Verify PR info is preserved
		pluckedBranch2 := s.Engine.GetBranch("branch2")
		newPrInfo, err := pluckedBranch2.GetPrInfo()
		require.NoError(t, err)
		require.NotNil(t, newPrInfo)
		require.Equal(t, 456, *newPrInfo.Number())
		require.Equal(t, "Test Pluck PR", newPrInfo.Title())
	})

	t.Run("pluck differs from move by not bringing descendants", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
				"branch3": "branch2",
			})

		// Pluck branch2 to main
		err := Action(s.Context, Options{
			Source: "branch2",
			Onto:   "main",
		}, nil)
		require.NoError(t, err)

		// branch2 should be on main
		branch2 := s.Engine.GetBranch("branch2")
		parent2 := branch2.GetParent()
		require.Equal(t, "main", parent2.GetName())

		// branch3 should be on branch1 (grandparent), NOT branch2
		// This is what distinguishes pluck from move
		branch3 := s.Engine.GetBranch("branch3")
		parent3 := branch3.GetParent()
		require.Equal(t, "branch1", parent3.GetName())

		// Verify branch2 has no children
		graph := engine.BuildStackGraph(s.Engine, engine.SortStrategyAlphabetical, nil)
		branch2Children := graph.Children(branch2)
		require.Empty(t, branch2Children)
	})

	t.Run("allows plucking onto untracked branch", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
			}).
			CreateBranch("untracked").
			Checkout("main")

		// Pluck branch1 onto untracked branch
		err := Action(s.Context, Options{
			Source: "branch1",
			Onto:   "untracked",
		}, nil)
		require.NoError(t, err)

		// Verify branch1 is now on untracked
		branch1 := s.Engine.GetBranch("branch1")
		parent1 := branch1.GetParent()
		require.NotNil(t, parent1)
		require.Equal(t, "untracked", parent1.GetName())
	})

	t.Run("plucks from middle of stack correctly", func(t *testing.T) {
		// main -> A -> B -> C -> D
		// Pluck B to main
		// Result: B is on main, C is on A (not B!)
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"A": "main",
				"B": "A",
				"C": "B",
				"D": "C",
			})

		err := Action(s.Context, Options{
			Source: "B",
			Onto:   "main",
		}, nil)
		require.NoError(t, err)

		// B is on main
		branchB := s.Engine.GetBranch("B")
		parentB := branchB.GetParent()
		require.Equal(t, "main", parentB.GetName())

		// C is on A (B's old parent)
		branchC := s.Engine.GetBranch("C")
		parentC := branchC.GetParent()
		require.Equal(t, "A", parentC.GetName())

		// D is still on C
		branchD := s.Engine.GetBranch("D")
		parentD := branchD.GetParent()
		require.Equal(t, "C", parentD.GetName())
	})
}
