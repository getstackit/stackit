package create

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/testhelpers"
	"stackit.dev/stackit/testhelpers/scenario"
)

func TestCreateAction_Stdin(t *testing.T) {
	t.Run("reads commit message from stdin in non-interactive mode", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		// Create a change to stage
		err := s.Scene.Repo.CreateChange("staged content", "test-file", false)
		require.NoError(t, err)

		// Mock stdin
		oldStdin := os.Stdin
		defer func() { os.Stdin = oldStdin }()
		r, w, _ := os.Pipe()
		os.Stdin = r

		expectedMessage := "feat: commit message from stdin"
		go func() {
			_, _ = w.Write([]byte(expectedMessage))
			_ = w.Close()
		}()

		// Scenario already calls tui.SetInteractive(false)

		opts := Options{}
		err = Action(s.Context, opts, nil)
		require.NoError(t, err)

		// Verify branch was created with name generated from stdin message
		currentBranch, err := s.Scene.Repo.CurrentBranchName()
		require.NoError(t, err)
		require.Contains(t, currentBranch, "commit-message-from-stdin")

		// Verify commit message
		commits, err := s.Scene.Repo.ListCurrentBranchCommitMessages()
		require.NoError(t, err)
		require.Contains(t, commits, expectedMessage)
	})
}

func TestCreateAction_Insert(t *testing.T) {
	t.Run("inserts branch between parent and children", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		// 1. Create child1 on main
		err := s.Scene.Repo.CreateChange("child1 content", "file1", false)
		require.NoError(t, err)
		opts1 := Options{
			BranchName: "child1",
			Message:    "Add child1",
		}
		err = Action(s.Context, opts1, nil)
		require.NoError(t, err)

		// 2. Go back to main
		err = s.Scene.Repo.CheckoutBranch("main")
		require.NoError(t, err)

		// 3. Create 'inserted' branch with --insert
		err = s.Scene.Repo.CreateChange("inserted content", "file2", false)
		require.NoError(t, err)
		opts2 := Options{
			BranchName: "inserted",
			Message:    "Add inserted",
			Insert:     true,
		}
		err = Action(s.Context, opts2, nil)
		require.NoError(t, err)

		// 4. Verify metadata relationships
		eng := s.Context.Engine
		branchparentInserted := eng.GetBranch("inserted")
		parentInserted := branchparentInserted.GetParent()
		require.NotNil(t, parentInserted)
		require.Equal(t, "main", parentInserted.GetName())
		branchparentChild1 := eng.GetBranch("child1")
		parentChild1 := branchparentChild1.GetParent()
		require.NotNil(t, parentChild1)
		require.Equal(t, "inserted", parentChild1.GetName())

		// 5. Verify physical relationship (child1 should have been restacked onto inserted)
		isAncestor, err := s.Scene.Repo.IsAncestor("inserted", "child1")
		require.NoError(t, err)
		require.True(t, isAncestor, "inserted should be an ancestor of child1 in git history")
	})

	t.Run("inserts branch in the middle of a stack", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		// 1. Create stack: main -> child1 -> child2
		err := s.Scene.Repo.CreateChange("child1 content", "file1", false)
		require.NoError(t, err)
		err = Action(s.Context, Options{BranchName: "child1", Message: "Add child1"}, nil)
		require.NoError(t, err)

		err = s.Scene.Repo.CreateChange("child2 content", "file2", false)
		require.NoError(t, err)
		err = Action(s.Context, Options{BranchName: "child2", Message: "Add child2"}, nil)
		require.NoError(t, err)

		// 2. Go to child1
		err = s.Scene.Repo.CheckoutBranch("child1")
		require.NoError(t, err)

		// Rebuild engine to ensure it knows we're on child1
		err = s.Context.Engine.Rebuild(s.Context.Engine.Trunk().GetName())
		require.NoError(t, err)

		// 3. Insert 'inserted' after child1
		err = s.Scene.Repo.CreateChange("inserted content", "file3", false)
		require.NoError(t, err)
		err = Action(s.Context, Options{
			BranchName: "inserted",
			Message:    "Add inserted",
			Insert:     true,
		}, nil)
		require.NoError(t, err)

		// 4. Verify relationships
		eng := s.Context.Engine
		branchparentInserted := eng.GetBranch("inserted")
		parentInserted := branchparentInserted.GetParent()
		require.NotNil(t, parentInserted)
		require.Equal(t, "child1", parentInserted.GetName())
		branchparentChild2 := eng.GetBranch("child2")
		parentChild2 := branchparentChild2.GetParent()
		require.NotNil(t, parentChild2)
		require.Equal(t, "inserted", parentChild2.GetName())

		// 5. Verify physical relationship
		isAncestor, err := s.Scene.Repo.IsAncestor("inserted", "child2")
		require.NoError(t, err)
		require.True(t, isAncestor, "inserted should be an ancestor of child2")
	})

	t.Run("inserts branch into a branching stack (multiple children)", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		// 1. Create two children from main: main -> child1, main -> child2
		err := s.Scene.Repo.CreateChange("child1 content", "file1", false)
		require.NoError(t, err)
		err = Action(s.Context, Options{BranchName: "child1", Message: "Add child1"}, nil)
		require.NoError(t, err)

		err = s.Scene.Repo.CheckoutBranch("main")
		require.NoError(t, err)

		err = s.Scene.Repo.CreateChange("child2 content", "file2", false)
		require.NoError(t, err)
		err = Action(s.Context, Options{BranchName: "child2", Message: "Add child2"}, nil)
		require.NoError(t, err)

		// 2. Go back to main
		err = s.Scene.Repo.CheckoutBranch("main")
		require.NoError(t, err)

		// 3. Insert 'inserted' after main
		err = s.Scene.Repo.CreateChange("inserted content", "file3", false)
		require.NoError(t, err)
		// Non-interactive mode should move all children by default
		err = Action(s.Context, Options{
			BranchName: "inserted",
			Message:    "Add inserted",
			Insert:     true,
		}, nil)
		require.NoError(t, err)

		// 4. Verify relationships
		eng := s.Context.Engine
		branchparentInserted := eng.GetBranch("inserted")
		parentInserted := branchparentInserted.GetParent()
		require.NotNil(t, parentInserted)
		require.Equal(t, "main", parentInserted.GetName())
		branchparentChild1 := eng.GetBranch("child1")
		parentChild1 := branchparentChild1.GetParent()
		require.NotNil(t, parentChild1)
		require.Equal(t, "inserted", parentChild1.GetName())
		branchparentChild2 := eng.GetBranch("child2")
		parentChild2 := branchparentChild2.GetParent()
		require.NotNil(t, parentChild2)
		require.Equal(t, "inserted", parentChild2.GetName())

		// 5. Verify physical relationships
		isAncestor, err := s.Scene.Repo.IsAncestor("inserted", "child1")
		require.NoError(t, err)
		require.True(t, isAncestor, "inserted should be an ancestor of child1")

		isAncestor, err = s.Scene.Repo.IsAncestor("inserted", "child2")
		require.NoError(t, err)
		require.True(t, isAncestor, "inserted should be an ancestor of child2")
	})

	t.Run("restores original branch after insert", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		// 1. Create child1 on main
		err := s.Scene.Repo.CreateChange("child1 content", "file1", false)
		require.NoError(t, err)
		err = Action(s.Context, Options{BranchName: "child1", Message: "Add child1"}, nil)
		require.NoError(t, err)

		// 2. Go back to main
		err = s.Scene.Repo.CheckoutBranch("main")
		require.NoError(t, err)

		// 3. Insert 'inserted' branch with --insert
		err = s.Scene.Repo.CreateChange("inserted content", "file2", false)
		require.NoError(t, err)
		err = Action(s.Context, Options{
			BranchName: "inserted",
			Message:    "Add inserted",
			Insert:     true,
		}, nil)
		require.NoError(t, err)

		// 4. Verify we are back on main
		current, err := s.Scene.Repo.CurrentBranchName()
		require.NoError(t, err)
		require.Equal(t, "main", current)
	})
}

func TestCreateAction_Insert_Deep(t *testing.T) {
	t.Run("restacks descendants deep into the stack", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		// 1. Create stack: main -> child1 -> grandchild
		err := s.Scene.Repo.CreateChange("child1 content", "file1", false)
		require.NoError(t, err)
		err = Action(s.Context, Options{BranchName: "child1", Message: "Add child1"}, nil)
		require.NoError(t, err)

		err = s.Scene.Repo.CreateChange("grandchild content", "file2", false)
		require.NoError(t, err)
		err = Action(s.Context, Options{BranchName: "grandchild", Message: "Add grandchild"}, nil)
		require.NoError(t, err)

		// 2. Go back to main
		err = s.Scene.Repo.CheckoutBranch("main")
		require.NoError(t, err)
		err = s.Context.Engine.Rebuild("main")
		require.NoError(t, err)

		// 3. Insert 'inserted' after main
		err = s.Scene.Repo.CreateChange("inserted content", "file3", false)
		require.NoError(t, err)
		err = Action(s.Context, Options{
			BranchName: "inserted",
			Message:    "Add inserted",
			Insert:     true,
		}, nil)
		require.NoError(t, err)

		// 4. Verify relationships
		eng := s.Context.Engine
		require.Equal(t, "main", eng.GetBranch("inserted").GetParent().GetName())
		require.Equal(t, "inserted", eng.GetBranch("child1").GetParent().GetName())
		require.Equal(t, "child1", eng.GetBranch("grandchild").GetParent().GetName())

		// 5. Verify physical relationships (deep restack)
		// grandchild should have been restacked and thus should have 'inserted' in its ancestry
		isAncestor, err := s.Scene.Repo.IsAncestor("inserted", "grandchild")
		require.NoError(t, err)
		require.True(t, isAncestor, "inserted should be an ancestor of grandchild after deep restack")

		isAncestor, err = s.Scene.Repo.IsAncestor("child1", "grandchild")
		require.NoError(t, err)
		require.True(t, isAncestor, "child1 should still be an ancestor of grandchild")
	})
}

func TestCreateAction_Worktree(t *testing.T) {
	t.Run("creates worktree when -w flag is used from trunk", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		// Create a change to stage
		err := s.Scene.Repo.CreateChange("feature content", "feature.txt", false)
		require.NoError(t, err)

		// Create branch with worktree from trunk
		opts := Options{
			BranchName: "feature-branch",
			Message:    "Add feature",
			Worktree:   true,
		}
		err = Action(s.Context, opts, nil)
		require.NoError(t, err)

		// Verify we're back on trunk (main repo stays on trunk, worktree has the branch)
		currentBranch, err := s.Scene.Repo.CurrentBranchName()
		require.NoError(t, err)
		require.Equal(t, "main", currentBranch)

		// Verify worktree was registered
		eng := s.Context.Engine
		worktreeInfo, err := eng.GetWorktreeForStack("feature-branch")
		require.NoError(t, err)
		require.NotNil(t, worktreeInfo)
		require.Equal(t, "feature-branch", worktreeInfo.AnchorBranch)
		require.Contains(t, worktreeInfo.Path, "feature-branch")

		// Clean up worktree
		_ = eng.RemoveWorktree(s.Context.Context, worktreeInfo.Path)
		_ = eng.UnregisterWorktree("feature-branch")
	})

	t.Run("fails with -w flag when not on trunk", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		// Create a child branch first (without worktree)
		err := s.Scene.Repo.CreateChange("child1 content", "file1", false)
		require.NoError(t, err)
		err = Action(s.Context, Options{BranchName: "child1", Message: "Add child1"}, nil)
		require.NoError(t, err)

		// Now try to create another branch with -w from child1 (not trunk)
		err = s.Scene.Repo.CreateChange("child2 content", "file2", false)
		require.NoError(t, err)
		opts := Options{
			BranchName: "child2",
			Message:    "Add child2",
			Worktree:   true,
		}
		err = Action(s.Context, opts, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "only valid when creating a new stack from trunk")
	})
}
