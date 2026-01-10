package move

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/testhelpers"
	"stackit.dev/stackit/testhelpers/scenario"
)

func TestMoveAction(t *testing.T) {
	t.Run("moves branch downstack and restacks descendants", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
				"branch3": "branch2",
			})

		// Verify initial state
		branchparent2Initial := s.Engine.GetBranch("branch2")
		parent2Initial := branchparent2Initial.GetParent()
		require.NotNil(t, parent2Initial)
		require.Equal(t, "branch1", parent2Initial.GetName())
		branchparent3Initial := s.Engine.GetBranch("branch3")
		parent3Initial := branchparent3Initial.GetParent()
		require.NotNil(t, parent3Initial)
		require.Equal(t, "branch2", parent3Initial.GetName())

		// Move branch2 from branch1 to main (downstack)
		err := Action(s.Context, Options{
			Source: "branch2",
			Onto:   "main",
		}, nil)
		require.NoError(t, err)

		// Verify new parent relationship
		branchparent2After := s.Engine.GetBranch("branch2")
		parent2After := branchparent2After.GetParent()
		require.NotNil(t, parent2After)
		require.Equal(t, "main", parent2After.GetName())
		graph := engine.BuildStackGraph(s.Engine, engine.SortStrategyAlphabetical, nil)
		mainChildren := graph.Children(s.Engine.Trunk())
		require.Contains(t, mainChildren, "branch2")
		branch1Children := graph.Children(s.Engine.GetBranch("branch1"))
		require.NotContains(t, branch1Children, "branch2")

		// Verify branch3 still has branch2 as parent (descendant relationship preserved)
		branchparent3After := s.Engine.GetBranch("branch3")
		parent3After := branchparent3After.GetParent()
		require.NotNil(t, parent3After)
		require.Equal(t, "branch2", parent3After.GetName())
	})

	t.Run("moves branch upstack and restacks descendants", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branchA":  "main",
				"branchA2": "branchA",
				"branchB":  "main",
			})

		// Move branchA from main to branchB (upstack - moving to a sibling branch)
		err := Action(s.Context, Options{
			Source: "branchA",
			Onto:   "branchB",
		}, nil)
		require.NoError(t, err)

		// Verify new parent relationship
		branchparentA := s.Engine.GetBranch("branchA")
		parentA := branchparentA.GetParent()
		require.NotNil(t, parentA)
		require.Equal(t, "branchB", parentA.GetName())
		graph := engine.BuildStackGraph(s.Engine, engine.SortStrategyAlphabetical, nil)
		branchBChildren := graph.Children(s.Engine.GetBranch("branchB"))
		require.Contains(t, branchBChildren, "branchA")
		mainChildren := graph.Children(s.Engine.Trunk())
		require.NotContains(t, mainChildren, "branchA")

		// Verify branchA2 still has branchA as parent (descendant relationship preserved)
		branchparentA2 := s.Engine.GetBranch("branchA2")
		parentA2 := branchparentA2.GetParent()
		require.NotNil(t, parentA2)
		require.Equal(t, "branchA", parentA2.GetName())
	})

	t.Run("moves branch across different stack trees", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branchA1": "main",
				"branchA2": "branchA1",
				"branchB1": "main",
				"branchB2": "branchB1",
			})

		// Move branchA2 from branchA1 to branchB1 (across stacks)
		err := Action(s.Context, Options{
			Source: "branchA2",
			Onto:   "branchB1",
		}, nil)
		require.NoError(t, err)

		// Verify new parent relationship
		branchparentA2 := s.Engine.GetBranch("branchA2")
		parentA2 := branchparentA2.GetParent()
		require.NotNil(t, parentA2)
		require.Equal(t, "branchB1", parentA2.GetName())
		graph := engine.BuildStackGraph(s.Engine, engine.SortStrategyAlphabetical, nil)
		branchB1Children := graph.Children(s.Engine.GetBranch("branchB1"))
		require.Contains(t, branchB1Children, "branchA2")
		branchA1Children := graph.Children(s.Engine.GetBranch("branchA1"))
		require.NotContains(t, branchA1Children, "branchA2")
	})

	t.Run("defaults source to current branch", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
			})

		// Switch to branch2
		s.Checkout("branch2")

		// Move without specifying source (should use current branch)
		err := Action(s.Context, Options{
			Source: "", // Empty means use current branch
			Onto:   "main",
		}, nil)
		require.NoError(t, err)

		// Verify branch2 was moved
		branchparent2 := s.Engine.GetBranch("branch2")
		parent2 := branchparent2.GetParent()
		require.NotNil(t, parent2)
		require.Equal(t, "main", parent2.GetName())
	})

	t.Run("prevents moving trunk branch", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		err := Action(s.Context, Options{
			Source: "main",
			Onto:   "main", // Even if onto is same, should fail earlier
		}, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot move trunk branch")
	})

	t.Run("prevents moving onto itself", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
			})

		err := Action(s.Context, Options{
			Source: "branch1",
			Onto:   "branch1",
		}, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot move branch onto itself")
	})

	t.Run("prevents moving onto descendant (cycle detection)", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
				"branch3": "branch2",
			})

		// Try to move branch1 onto branch3 (which is a descendant of branch1)
		err := Action(s.Context, Options{
			Source: "branch1",
			Onto:   "branch3",
		}, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot move")
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

	t.Run("fails when not on branch and no source specified", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// Detach HEAD
		s.RunGit("checkout", "HEAD~0").Rebuild()

		// Try to move without specifying source - should fail because we're in detached HEAD
		err := Action(s.Context, Options{
			Source: "",
			Onto:   "main",
		}, nil)
		require.Error(t, err)
		// The error should be either "not on a branch" or "cannot move trunk branch"
		errMsg := err.Error()
		require.True(t,
			strings.Contains(errMsg, "not on a branch") || strings.Contains(errMsg, "cannot move trunk branch"),
			"error should mention not on a branch or trunk: %s", errMsg)
	})

	t.Run("allows moving onto untracked branch", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
			}).
			CreateBranch("untracked").
			Checkout("main")

		// Move branch1 onto untracked branch (should work)
		err := Action(s.Context, Options{
			Source: "branch1",
			Onto:   "untracked",
		}, nil)
		require.NoError(t, err)

		// Verify new parent relationship
		branchparent1 := s.Engine.GetBranch("branch1")
		parent1 := branchparent1.GetParent()
		require.NotNil(t, parent1)
		require.Equal(t, "untracked", parent1.GetName())
	})

	t.Run("restacks all descendants after move", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
				"branch3": "branch2",
			})

		// Get initial revisions
		branch1RevBefore, _ := s.Scene.Repo.GetRevision("branch1")
		branch2RevBefore, _ := s.Scene.Repo.GetRevision("branch2")
		branch3RevBefore, _ := s.Scene.Repo.GetRevision("branch3")

		// Make a change to main to force restacking
		s.Checkout("main").
			Commit("new main change")

		// Move branch1 to main (which now has new commits)
		err := Action(s.Context, Options{
			Source: "branch1",
			Onto:   "main",
		}, nil)
		require.NoError(t, err)

		// Verify branches were restacked (revisions should have changed)
		branch1RevAfter, _ := s.Scene.Repo.GetRevision("branch1")
		branch2RevAfter, _ := s.Scene.Repo.GetRevision("branch2")
		branch3RevAfter, _ := s.Scene.Repo.GetRevision("branch3")

		// Revisions should be different (branches were rebased)
		require.NotEqual(t, branch1RevBefore, branch1RevAfter)
		require.NotEqual(t, branch2RevBefore, branch2RevAfter)
		require.NotEqual(t, branch3RevBefore, branch3RevAfter)

		// Verify all branches are still fixed (properly restacked)
		s.ExpectBranchFixed("branch1").
			ExpectBranchFixed("branch2").
			ExpectBranchFixed("branch3")
	})

	t.Run("fails when onto is not specified", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
			})

		err := Action(s.Context, Options{
			Source: "branch1",
			Onto:   "", // Empty onto
		}, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "onto branch must be specified")
	})

	t.Run("moves branch downstack without pulling along intermediate commits", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// Create a clean stack: main -> branch1 -> branch2
		s.Checkout("main").CreateBranch("branch1").Commit("commit in branch1")
		branch1Commit, _ := s.Scene.Repo.GetRevision("HEAD")
		s.TrackBranch("branch1", "main")

		s.Checkout("branch1").CreateBranch("branch2").Commit("commit in branch2")
		s.TrackBranch("branch2", "branch1")

		// Move branch2 from branch1 to main
		err := Action(s.Context, Options{
			Source: "branch2",
			Onto:   "main",
		}, nil)
		require.NoError(t, err)

		// Verify branch2 only has its own commit relative to main
		// branch1's commit should NOT be in branch2's history anymore
		branch2Commits, err := s.Engine.GetAllCommits(s.Engine.GetBranch("branch2"), engine.CommitFormatSHA)
		require.NoError(t, err)

		// branch2 should only have 1 commit (the one we added to it)
		require.Equal(t, 1, len(branch2Commits), "branch2 should only have 1 commit after move")
		require.NotEqual(t, branch1Commit, branch2Commits[0], "branch1's commit should not be in branch2")

		// Verify the commit message matches
		branch2Messages, err := s.Engine.GetAllCommits(s.Engine.GetBranch("branch2"), engine.CommitFormatSubject)
		require.NoError(t, err)
		require.Equal(t, "commit in branch2", branch2Messages[0])
	})

	t.Run("preserves PR information after move", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
			})

		// Add PR info to branch2
		branch2 := s.Engine.GetBranch("branch2")
		prNumber := 123
		prInfo := engine.NewPrInfo(&prNumber, "Test PR", "Test Body", "OPEN", "branch1", "https://github.com/owner/repo/pull/123", false)
		err := s.Engine.UpsertPrInfo(branch2, prInfo)
		require.NoError(t, err)

		// Move branch2 to main
		err = Action(s.Context, Options{
			Source: "branch2",
			Onto:   "main",
		}, nil)
		require.NoError(t, err)

		// Verify PR info is preserved
		movedBranch2 := s.Engine.GetBranch("branch2")
		newPrInfo, err := movedBranch2.GetPrInfo()
		require.NoError(t, err)
		require.NotNil(t, newPrInfo)
		require.Equal(t, 123, *newPrInfo.Number())
		require.Equal(t, "Test PR", newPrInfo.Title())
	})

	t.Run("moves branch after it has been amended", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
			})

		// Amend branch2
		s.Checkout("branch2").RunGit("commit", "--amend", "--no-edit", "-m", "amended commit")

		// Move branch2 to main
		err := Action(s.Context, Options{
			Source: "branch2",
			Onto:   "main",
		}, nil)
		require.NoError(t, err)

		// Verify branch2 still has 1 commit relative to main (the amended one)
		branch2Commits, err := s.Engine.GetAllCommits(s.Engine.GetBranch("branch2"), engine.CommitFormatSubject)
		require.NoError(t, err)
		require.Equal(t, 1, len(branch2Commits))
		require.Equal(t, "amended commit", branch2Commits[0])
	})

	t.Run("moves branch across stacks without pulling old parent's commits", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// Stack 1: main -> branchA1 -> branchA2
		s.Checkout("main").CreateBranch("branchA1").Commit("commit A1")
		branchA1Commit, _ := s.Scene.Repo.GetRevision("HEAD")
		s.TrackBranch("branchA1", "main")

		s.Checkout("branchA1").CreateBranch("branchA2").Commit("commit A2")
		s.TrackBranch("branchA2", "branchA1")

		// Stack 2: main -> branchB1
		s.Checkout("main").CreateBranch("branchB1").Commit("commit B1")
		s.TrackBranch("branchB1", "main")

		// Move branchA2 from branchA1 to branchB1
		err := Action(s.Context, Options{
			Source: "branchA2",
			Onto:   "branchB1",
		}, nil)
		require.NoError(t, err)

		// Verify branchA2 only has its own commit relative to branchB1
		// branchA1's commit should NOT be in branchA2's history relative to branchB1
		branchA2Commits, err := s.Engine.GetAllCommits(s.Engine.GetBranch("branchA2"), engine.CommitFormatSHA)
		require.NoError(t, err)

		require.Equal(t, 1, len(branchA2Commits), "branchA2 should only have 1 commit after move")
		require.NotEqual(t, branchA1Commit, branchA2Commits[0], "branchA1's commit should not be in branchA2")

		// Verify the commit message matches
		branchA2Messages, err := s.Engine.GetAllCommits(s.Engine.GetBranch("branchA2"), engine.CommitFormatSubject)
		require.NoError(t, err)
		require.Equal(t, "commit A2", branchA2Messages[0])
	})
}
