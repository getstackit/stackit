package create

import (
	"os"
	"testing"

	"github.com/getstackit/stackit/internal/app/actiondeps"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions/worktree"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func deps(s *scenario.Scenario) Deps {
	return Deps{
		Engine:       s.Context.Engine,
		MutationDeps: actiondeps.Mutation(s.Context),
		Log:          s.Context.Logger,
		Verify:       s.Context.Verify,
		Worktrees: worktree.BranchWorktrees{Deps: worktree.Deps{
			Engine:   s.Context.Engine,
			Out:      s.Context.Output,
			Log:      s.Context.Logger,
			RepoRoot: s.Context.RepoRoot,
			Here:     actiondeps.Here(s.Context),
			Config:   s.Context.Config,
			Prompts:  s.Context.Prompts(),
		}},
	}
}

func TestCreateAction_Stdin(t *testing.T) {
	// NOT parallel: this test swaps the process-global os.Stdin for a pipe. Under
	// t.Parallel(), a concurrent test's git subprocess inherits that pipe as stdin
	// and blocks forever (same global-state hazard class as t.Setenv).
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
			_, _ = w.WriteString(expectedMessage)
			_ = w.Close()
		}()

		// Scenario already calls term.SetInteractive(false)

		opts := Options{}
		_, err = Action(t.Context(), deps(s), opts, nil)
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

func TestCreateAction_RefusesTrunkInManagedWorktree(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
	s.WithInitialCommit()
	s.Context.InManagedWorktree = true
	s.Context.WorktreeInfo = &engine.WorktreeInfo{
		Name:         "feature-wt",
		AnchorBranch: "feature-wt-anchor",
	}

	_, err := Action(t.Context(), deps(s), Options{BranchName: "new-branch", Message: "test"}, nil)
	require.Error(t, err)
	require.ErrorContains(t, err, "cannot create a branch from trunk inside managed worktree feature-wt")
}

func TestCreateAction_Insert(t *testing.T) {
	t.Parallel()
	t.Run("inserts branch between parent and children", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		// 1. Create child1 on main
		err := s.Scene.Repo.CreateChange("child1 content", "file1", false)
		require.NoError(t, err)
		opts1 := Options{
			BranchName: "child1",
			Message:    "Add child1",
		}
		_, err = Action(t.Context(), deps(s), opts1, nil)
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
		_, err = Action(t.Context(), deps(s), opts2, nil)
		require.NoError(t, err)

		// 4. Verify metadata relationships
		eng := s.Context.Engine
		branchparentInserted := eng.GetBranch("inserted")
		parentInserted := branchparentInserted.Parent()
		require.NotNil(t, parentInserted)
		require.Equal(t, "main", parentInserted.Name())
		branchparentChild1 := eng.GetBranch("child1")
		parentChild1 := branchparentChild1.Parent()
		require.NotNil(t, parentChild1)
		require.Equal(t, "inserted", parentChild1.Name())

		// 5. Verify physical relationship (child1 should have been restacked onto inserted)
		isAncestor, err := s.Scene.Repo.IsAncestor("inserted", "child1")
		require.NoError(t, err)
		require.True(t, isAncestor, "inserted should be an ancestor of child1 in git history")
	})

	t.Run("inserts branch in the middle of a stack", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		// 1. Create stack: main -> child1 -> child2
		err := s.Scene.Repo.CreateChange("child1 content", "file1", false)
		require.NoError(t, err)
		_, err = Action(t.Context(), deps(s), Options{BranchName: "child1", Message: "Add child1"}, nil)
		require.NoError(t, err)

		err = s.Scene.Repo.CreateChange("child2 content", "file2", false)
		require.NoError(t, err)
		_, err = Action(t.Context(), deps(s), Options{BranchName: "child2", Message: "Add child2"}, nil)
		require.NoError(t, err)

		// 2. Go to child1
		err = s.Scene.Repo.CheckoutBranch("child1")
		require.NoError(t, err)

		// Rebuild engine to ensure it knows we're on child1
		err = s.Context.Engine.Rebuild(t.Context(), s.Context.Engine.Trunk().Name())
		require.NoError(t, err)

		// 3. Insert 'inserted' after child1
		err = s.Scene.Repo.CreateChange("inserted content", "file3", false)
		require.NoError(t, err)
		_, err = Action(t.Context(), deps(s), Options{
			BranchName: "inserted",
			Message:    "Add inserted",
			Insert:     true,
		}, nil)
		require.NoError(t, err)

		// 4. Verify relationships
		eng := s.Context.Engine
		branchparentInserted := eng.GetBranch("inserted")
		parentInserted := branchparentInserted.Parent()
		require.NotNil(t, parentInserted)
		require.Equal(t, "child1", parentInserted.Name())
		branchparentChild2 := eng.GetBranch("child2")
		parentChild2 := branchparentChild2.Parent()
		require.NotNil(t, parentChild2)
		require.Equal(t, "inserted", parentChild2.Name())

		// 5. Verify physical relationship
		isAncestor, err := s.Scene.Repo.IsAncestor("inserted", "child2")
		require.NoError(t, err)
		require.True(t, isAncestor, "inserted should be an ancestor of child2")
	})

	t.Run("inserts branch into a branching stack (multiple children)", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		// 1. Create two children from main: main -> child1, main -> child2
		err := s.Scene.Repo.CreateChange("child1 content", "file1", false)
		require.NoError(t, err)
		_, err = Action(t.Context(), deps(s), Options{BranchName: "child1", Message: "Add child1"}, nil)
		require.NoError(t, err)

		err = s.Scene.Repo.CheckoutBranch("main")
		require.NoError(t, err)

		err = s.Scene.Repo.CreateChange("child2 content", "file2", false)
		require.NoError(t, err)
		_, err = Action(t.Context(), deps(s), Options{BranchName: "child2", Message: "Add child2"}, nil)
		require.NoError(t, err)

		// 2. Go back to main
		err = s.Scene.Repo.CheckoutBranch("main")
		require.NoError(t, err)

		// 3. Insert 'inserted' after main
		err = s.Scene.Repo.CreateChange("inserted content", "file3", false)
		require.NoError(t, err)
		// Non-interactive mode should move all children by default
		_, err = Action(t.Context(), deps(s), Options{
			BranchName: "inserted",
			Message:    "Add inserted",
			Insert:     true,
		}, nil)
		require.NoError(t, err)

		// 4. Verify relationships
		eng := s.Context.Engine
		branchparentInserted := eng.GetBranch("inserted")
		parentInserted := branchparentInserted.Parent()
		require.NotNil(t, parentInserted)
		require.Equal(t, "main", parentInserted.Name())
		branchparentChild1 := eng.GetBranch("child1")
		parentChild1 := branchparentChild1.Parent()
		require.NotNil(t, parentChild1)
		require.Equal(t, "inserted", parentChild1.Name())
		branchparentChild2 := eng.GetBranch("child2")
		parentChild2 := branchparentChild2.Parent()
		require.NotNil(t, parentChild2)
		require.Equal(t, "inserted", parentChild2.Name())

		// 5. Verify physical relationships
		isAncestor, err := s.Scene.Repo.IsAncestor("inserted", "child1")
		require.NoError(t, err)
		require.True(t, isAncestor, "inserted should be an ancestor of child1")

		isAncestor, err = s.Scene.Repo.IsAncestor("inserted", "child2")
		require.NoError(t, err)
		require.True(t, isAncestor, "inserted should be an ancestor of child2")
	})

	t.Run("restores original branch after insert", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		// 1. Create child1 on main
		err := s.Scene.Repo.CreateChange("child1 content", "file1", false)
		require.NoError(t, err)
		_, err = Action(t.Context(), deps(s), Options{BranchName: "child1", Message: "Add child1"}, nil)
		require.NoError(t, err)

		// 2. Go back to main
		err = s.Scene.Repo.CheckoutBranch("main")
		require.NoError(t, err)

		// 3. Insert 'inserted' branch with --insert
		err = s.Scene.Repo.CreateChange("inserted content", "file2", false)
		require.NoError(t, err)
		_, err = Action(t.Context(), deps(s), Options{
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
	t.Parallel()
	t.Run("restacks descendants deep into the stack", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		// 1. Create stack: main -> child1 -> grandchild
		err := s.Scene.Repo.CreateChange("child1 content", "file1", false)
		require.NoError(t, err)
		_, err = Action(t.Context(), deps(s), Options{BranchName: "child1", Message: "Add child1"}, nil)
		require.NoError(t, err)

		err = s.Scene.Repo.CreateChange("grandchild content", "file2", false)
		require.NoError(t, err)
		_, err = Action(t.Context(), deps(s), Options{BranchName: "grandchild", Message: "Add grandchild"}, nil)
		require.NoError(t, err)

		// 2. Go back to main
		err = s.Scene.Repo.CheckoutBranch("main")
		require.NoError(t, err)
		err = s.Context.Engine.Rebuild(t.Context(), "main")
		require.NoError(t, err)

		// 3. Insert 'inserted' after main
		err = s.Scene.Repo.CreateChange("inserted content", "file3", false)
		require.NoError(t, err)
		_, err = Action(t.Context(), deps(s), Options{
			BranchName: "inserted",
			Message:    "Add inserted",
			Insert:     true,
		}, nil)
		require.NoError(t, err)

		// 4. Verify relationships
		eng := s.Context.Engine
		require.Equal(t, "main", eng.GetBranch("inserted").Parent().Name())
		require.Equal(t, "inserted", eng.GetBranch("child1").Parent().Name())
		require.Equal(t, "child1", eng.GetBranch("grandchild").Parent().Name())

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
	t.Parallel()
	t.Run("creates worktree when -w flag is used from trunk", func(t *testing.T) {
		t.Parallel()
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
		_, err = Action(t.Context(), deps(s), opts, nil)
		require.NoError(t, err)

		// Verify we're back on trunk (main repo stays on trunk, worktree has the branch)
		currentBranch, err := s.Scene.Repo.CurrentBranchName()
		require.NoError(t, err)
		require.Equal(t, "main", currentBranch)

		// Verify worktree was registered
		eng := s.Context.Engine
		stackRoot := eng.GetStackRootForBranch(eng.GetBranch("feature-branch"))
		require.NotEmpty(t, stackRoot)
		require.NotEqual(t, "feature-branch", stackRoot)

		worktreeInfo, err := eng.GetWorktreeForStack(t.Context(), stackRoot)
		require.NoError(t, err)
		require.NotNil(t, worktreeInfo)
		require.Equal(t, stackRoot, worktreeInfo.AnchorBranch)
		require.Contains(t, worktreeInfo.Path.String(), "feature-branch")

		require.True(t, eng.GetBranch(stackRoot).IsWorktreeAnchor())
		require.Equal(t, stackRoot, eng.GetBranch("feature-branch").Parent().Name())

		// Clean up worktree
		_ = eng.RemoveWorktree(t.Context(), worktreeInfo.Path)
		_ = eng.UnregisterWorktree(t.Context(), stackRoot)
	})

	t.Run("fails with -w flag when not on trunk", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		// Create a child branch first (without worktree)
		err := s.Scene.Repo.CreateChange("child1 content", "file1", false)
		require.NoError(t, err)
		_, err = Action(t.Context(), deps(s), Options{BranchName: "child1", Message: "Add child1"}, nil)
		require.NoError(t, err)

		// Now try to create another branch with -w from child1 (not trunk)
		err = s.Scene.Repo.CreateChange("child2 content", "file2", false)
		require.NoError(t, err)
		opts := Options{
			BranchName: "child2",
			Message:    "Add child2",
			Worktree:   true,
		}
		_, err = Action(t.Context(), deps(s), opts, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "only valid when creating a new stack from trunk")
	})
}
