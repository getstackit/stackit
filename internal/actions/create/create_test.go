package create

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/config"
	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/testhelpers"
	"stackit.dev/stackit/testhelpers/scenario"
)

// createTestScenario creates a test scenario with engine and context
func createTestScenario(t *testing.T, setup testhelpers.SceneSetup) (*testhelpers.Scene, *app.Context) {
	t.Helper()

	// Force non-interactive mode for tests
	t.Setenv("STACKIT_NON_INTERACTIVE", "true")

	// Create scene manually
	scene := testhelpers.NewScene(t, setup)
	cfg, _ := config.LoadConfig(scene.Dir)
	trunk := cfg.Trunk()
	if trunk == "" {
		trunk = "main"
	}
	maxUndoDepth := cfg.UndoStackDepth()
	if maxUndoDepth <= 0 {
		maxUndoDepth = engine.DefaultMaxUndoStackDepth
	}
	eng, err := engine.NewEngine(engine.Options{
		RepoRoot:          scene.Dir,
		Trunk:             trunk,
		MaxUndoStackDepth: maxUndoDepth,
	})
	require.NoError(t, err)

	ctx := app.NewContext(eng)
	ctx.RepoRoot = scene.Dir

	return scene, ctx
}

func TestCreateAction_Stdin(t *testing.T) {
	t.Run("reads commit message from stdin in non-interactive mode", func(t *testing.T) {
		// Create scene manually
		scene, ctx := createTestScenario(t, testhelpers.BasicSceneSetup)
		err := scene.Repo.CreateChangeAndCommit("initial", "init")
		require.NoError(t, err)

		// Create a change to stage
		err = scene.Repo.CreateChange("staged content", "test-file", false)
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

		opts := Options{}
		err = Action(ctx, opts)
		require.NoError(t, err)

		// Verify branch was created with name generated from stdin message
		currentBranch, err := scene.Repo.CurrentBranchName()
		require.NoError(t, err)
		require.Contains(t, currentBranch, "commit-message-from-stdin")

		// Verify commit message
		commits, err := scene.Repo.ListCurrentBranchCommitMessages()
		require.NoError(t, err)
		require.Contains(t, commits, expectedMessage)
	})
}

func TestCreateAction_Insert(t *testing.T) {
	t.Run("inserts branch between parent and children", func(t *testing.T) {
		s, ctx := createTestScenario(t, testhelpers.BasicSceneSetup)
		err := s.Repo.CreateChangeAndCommit("initial", "init")
		require.NoError(t, err)

		// 1. Create child1 on main
		err = s.Repo.CreateChange("child1 content", "file1", false)
		require.NoError(t, err)
		opts1 := Options{
			BranchName: "child1",
			Message:    "Add child1",
		}
		err = Action(ctx, opts1)
		require.NoError(t, err)

		// 2. Go back to main
		err = s.Repo.CheckoutBranch("main")
		require.NoError(t, err)

		// 3. Create 'inserted' branch with --insert
		err = s.Repo.CreateChange("inserted content", "file2", false)
		require.NoError(t, err)
		opts2 := Options{
			BranchName: "inserted",
			Message:    "Add inserted",
			Insert:     true,
		}
		err = Action(ctx, opts2)
		require.NoError(t, err)

		// 4. Verify metadata relationships
		eng := ctx.Engine
		branchparentInserted := eng.GetBranch("inserted")
		parentInserted := branchparentInserted.GetParent()
		require.NotNil(t, parentInserted)
		require.Equal(t, "main", parentInserted.GetName())
		branchparentChild1 := eng.GetBranch("child1")
		parentChild1 := branchparentChild1.GetParent()
		require.NotNil(t, parentChild1)
		require.Equal(t, "inserted", parentChild1.GetName())

		// 5. Verify physical relationship (child1 should have been restacked onto inserted)
		isAncestor, err := s.Repo.IsAncestor("inserted", "child1")
		require.NoError(t, err)
		require.True(t, isAncestor, "inserted should be an ancestor of child1 in git history")
	})

	t.Run("inserts branch in the middle of a stack", func(t *testing.T) {
		s, ctx := createTestScenario(t, testhelpers.BasicSceneSetup)
		err := s.Repo.CreateChangeAndCommit("initial", "init")
		require.NoError(t, err)

		// 1. Create stack: main -> child1 -> child2
		err = s.Repo.CreateChange("child1 content", "file1", false)
		require.NoError(t, err)
		err = Action(ctx, Options{BranchName: "child1", Message: "Add child1"})
		require.NoError(t, err)

		err = s.Repo.CreateChange("child2 content", "file2", false)
		require.NoError(t, err)
		err = Action(ctx, Options{BranchName: "child2", Message: "Add child2"})
		require.NoError(t, err)

		// 2. Go to child1
		err = s.Repo.CheckoutBranch("child1")
		require.NoError(t, err)

		// Rebuild engine to ensure it knows we're on child1
		err = ctx.Engine.Rebuild(ctx.Engine.Trunk().GetName())
		require.NoError(t, err)

		// 3. Insert 'inserted' after child1
		err = s.Repo.CreateChange("inserted content", "file3", false)
		require.NoError(t, err)
		err = Action(ctx, Options{
			BranchName: "inserted",
			Message:    "Add inserted",
			Insert:     true,
		})
		require.NoError(t, err)

		// 4. Verify relationships
		eng := ctx.Engine
		branchparentInserted := eng.GetBranch("inserted")
		parentInserted := branchparentInserted.GetParent()
		require.NotNil(t, parentInserted)
		require.Equal(t, "child1", parentInserted.GetName())
		branchparentChild2 := eng.GetBranch("child2")
		parentChild2 := branchparentChild2.GetParent()
		require.NotNil(t, parentChild2)
		require.Equal(t, "inserted", parentChild2.GetName())

		// 5. Verify physical relationship
		isAncestor, err := s.Repo.IsAncestor("inserted", "child2")
		require.NoError(t, err)
		require.True(t, isAncestor, "inserted should be an ancestor of child2")
	})

	t.Run("inserts branch into a branching stack (multiple children)", func(t *testing.T) {
		s, ctx := createTestScenario(t, testhelpers.BasicSceneSetup)
		err := s.Repo.CreateChangeAndCommit("initial", "init")
		require.NoError(t, err)

		// 1. Create two children from main: main -> child1, main -> child2
		err = s.Repo.CreateChange("child1 content", "file1", false)
		require.NoError(t, err)
		err = Action(ctx, Options{BranchName: "child1", Message: "Add child1"})
		require.NoError(t, err)

		err = s.Repo.CheckoutBranch("main")
		require.NoError(t, err)

		err = s.Repo.CreateChange("child2 content", "file2", false)
		require.NoError(t, err)
		err = Action(ctx, Options{BranchName: "child2", Message: "Add child2"})
		require.NoError(t, err)

		// 2. Go back to main
		err = s.Repo.CheckoutBranch("main")
		require.NoError(t, err)

		// 3. Insert 'inserted' after main
		err = s.Repo.CreateChange("inserted content", "file3", false)
		require.NoError(t, err)
		// Non-interactive mode should move all children by default
		err = Action(ctx, Options{
			BranchName: "inserted",
			Message:    "Add inserted",
			Insert:     true,
		})
		require.NoError(t, err)

		// 4. Verify relationships
		eng := ctx.Engine
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
		isAncestor, err := s.Repo.IsAncestor("inserted", "child1")
		require.NoError(t, err)
		require.True(t, isAncestor, "inserted should be an ancestor of child1")

		isAncestor, err = s.Repo.IsAncestor("inserted", "child2")
		require.NoError(t, err)
		require.True(t, isAncestor, "inserted should be an ancestor of child2")
	})

	t.Run("restores original branch after insert", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		// 1. Create child1 on main
		err := s.Scene.Repo.CreateChange("child1 content", "file1", false)
		require.NoError(t, err)
		err = Action(s.Context, Options{BranchName: "child1", Message: "Add child1"})
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
		})
		require.NoError(t, err)

		// 4. Verify we are back on main
		current, err := s.Scene.Repo.CurrentBranchName()
		require.NoError(t, err)
		require.Equal(t, "main", current)
	})

	t.Run("inserts branch into a branching stack selecting only one child", func(t *testing.T) {
		s, ctx := createTestScenario(t, testhelpers.BasicSceneSetup)
		err := s.Repo.CreateChangeAndCommit("initial", "init")
		require.NoError(t, err)

		// 1. Create two children from main: main -> child1, main -> child2
		err = s.Repo.CreateChange("child1 content", "file1", false)
		require.NoError(t, err)
		err = Action(ctx, Options{BranchName: "child1", Message: "Add child1"})
		require.NoError(t, err)

		err = s.Repo.CheckoutBranch("main")
		require.NoError(t, err)

		err = s.Repo.CreateChange("child2 content", "file2", false)
		require.NoError(t, err)
		err = Action(ctx, Options{BranchName: "child2", Message: "Add child2"})
		require.NoError(t, err)

		// 2. Go back to main
		err = s.Repo.CheckoutBranch("main")
		require.NoError(t, err)

		// 3. Insert 'inserted' after main, but only move 'child1'
		err = s.Repo.CreateChange("inserted content", "file3", false)
		require.NoError(t, err)
		err = Action(ctx, Options{
			BranchName:       "inserted",
			Message:          "Add inserted",
			Insert:           true,
			SelectedChildren: []string{"child1"},
		})
		require.NoError(t, err)

		// 4. Verify relationships
		eng := ctx.Engine
		branchparentInserted := eng.GetBranch("inserted")
		parentInserted := branchparentInserted.GetParent()
		require.NotNil(t, parentInserted)
		require.Equal(t, "main", parentInserted.GetName())
		branchparentChild1 := eng.GetBranch("child1")
		parentChild1 := branchparentChild1.GetParent()
		require.NotNil(t, parentChild1)
		require.Equal(t, "inserted", parentChild1.GetName(), "child1 should have been moved to inserted")
		branchparentChild2 := eng.GetBranch("child2")
		parentChild2 := branchparentChild2.GetParent()
		require.NotNil(t, parentChild2)
		require.Equal(t, "main", parentChild2.GetName(), "child2 should have remained a child of main")

		// 5. Verify physical relationships
		isAncestor, err := s.Repo.IsAncestor("inserted", "child1")
		require.NoError(t, err)
		require.True(t, isAncestor, "inserted should be an ancestor of child1")

		isAncestor, err = s.Repo.IsAncestor("inserted", "child2")
		require.NoError(t, err)
		require.False(t, isAncestor, "inserted should NOT be an ancestor of child2")
	})
}

func TestCreateAction_Insert_Deep(t *testing.T) {
	t.Run("restacks descendants deep into the stack", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		// 1. Create stack: main -> child1 -> grandchild
		err := s.Scene.Repo.CreateChange("child1 content", "file1", false)
		require.NoError(t, err)
		err = Action(s.Context, Options{BranchName: "child1", Message: "Add child1"})
		require.NoError(t, err)

		err = s.Scene.Repo.CreateChange("grandchild content", "file2", false)
		require.NoError(t, err)
		err = Action(s.Context, Options{BranchName: "grandchild", Message: "Add grandchild"})
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
		})
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
