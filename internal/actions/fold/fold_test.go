package fold

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/testhelpers"
	"stackit.dev/stackit/testhelpers/scenario"
)

func TestFoldAction(t *testing.T) {
	t.Run("folds branch into parent", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
			})

		// Switch to branch2
		s.Checkout("branch2")

		// Fold branch2 into branch1
		err := Action(s.Context, Options{Keep: false}, nil)
		require.NoError(t, err)

		// Verify branch2 is deleted
		branches, err := s.Engine.Git().GetAllBranchNames()
		require.NoError(t, err)
		require.NotContains(t, branches, "branch2")

		// Verify we're on branch1
		currentBranch, err := s.Scene.Repo.CurrentBranchName()
		require.NoError(t, err)
		require.Equal(t, "branch1", currentBranch)

		// Verify branch1 contains both commits by checking log
		logOutput, err := s.Scene.Repo.RunGitCommandAndGetOutput("log", "--oneline", "main..branch1")
		require.NoError(t, err)
		require.Contains(t, logOutput, "change on branch1")
		require.Contains(t, logOutput, "change on branch2")
	})

	t.Run("reparents children when folding branch", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
				"branch3": "branch2",
			})

		// Switch to branch2
		s.Checkout("branch2")

		// Fold branch2 into branch1
		err := Action(s.Context, Options{Keep: false}, nil)
		require.NoError(t, err)

		// Verify branch3's parent is now branch1
		branchparent := s.Engine.GetBranch("branch3")
		parent := branchparent.GetParent()
		require.NotNil(t, parent)
		require.Equal(t, "branch1", parent.GetName())

		// Verify branch2 is deleted
		branches, err := s.Engine.Git().GetAllBranchNames()
		require.NoError(t, err)
		require.NotContains(t, branches, "branch2")
	})

	t.Run("folds with --keep flag", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
			})

		// Switch to branch2
		s.Checkout("branch2")

		// Fold branch1 into branch2 with --keep
		err := Action(s.Context, Options{Keep: true}, nil)
		require.NoError(t, err)

		// Verify branch1 is deleted
		branches, err := s.Engine.Git().GetAllBranchNames()
		require.NoError(t, err)
		require.NotContains(t, branches, "branch1")

		// Verify we're on branch2
		currentBranch, err := s.Scene.Repo.CurrentBranchName()
		require.NoError(t, err)
		require.Equal(t, "branch2", currentBranch)

		// Verify branch2's parent is now main
		branchparent := s.Engine.GetBranch("branch2")
		parent := branchparent.GetParent()
		require.NotNil(t, parent)
		require.Equal(t, "main", parent.GetName())

		// Verify branch2 contains both commits by checking log
		logOutput, err := s.Scene.Repo.RunGitCommandAndGetOutput("log", "--oneline", "main..branch2")
		require.NoError(t, err)
		require.Contains(t, logOutput, "change on branch1")
		require.Contains(t, logOutput, "change on branch2")
	})

	t.Run("folds with --keep and reparents siblings", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
				"branch3": "branch1",
			})

		// Switch to branch2
		s.Checkout("branch2")

		// Fold branch1 into branch2 with --keep
		err := Action(s.Context, Options{Keep: true}, nil)
		require.NoError(t, err)

		// Verify branch1 is deleted
		branches, err := s.Engine.Git().GetAllBranchNames()
		require.NoError(t, err)
		require.NotContains(t, branches, "branch1")

		// Verify branch3's parent is now branch2
		branchparent := s.Engine.GetBranch("branch3")
		parent := branchparent.GetParent()
		require.NotNil(t, parent)
		require.Equal(t, "branch2", parent.GetName())

		// Verify branch2's parent is now main
		branchparent2 := s.Engine.GetBranch("branch2")
		parent2 := branchparent2.GetParent()
		require.NotNil(t, parent2)
		require.Equal(t, "main", parent2.GetName())
	})

	t.Run("fails when trying to fold trunk", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// Try to fold trunk (main)
		err := Action(s.Context, Options{Keep: false}, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot fold trunk branch")
	})

	t.Run("fails when trying to fold untracked branch", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			CreateBranch("untracked")

		// Try to fold untracked branch
		err := Action(s.Context, Options{Keep: false}, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot fold untracked branch")
	})

	t.Run("fails when trying to fold into trunk with --keep", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
			})

		// Try to fold into trunk with --keep
		err := Action(s.Context, Options{Keep: true}, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot fold into trunk with --keep")
	})

	t.Run("fails when there are uncommitted changes", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
			}).
			WithUncommittedChange("dirty")

		// Try to fold with dirty tree
		err := Action(s.Context, Options{Keep: false}, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "uncommitted changes")
	})

	t.Run("returns clear error message on merge conflict", func(t *testing.T) {
		s := scenario.NewScenario(t, nil)

		// Create conflicting changes manually
		s.RunGit("commit", "--allow-empty", "-m", "init")
		err := s.Scene.Repo.CreateChangeAndCommit("initial content\n", "conflict.txt")
		require.NoError(t, err)

		s.CreateBranch("branch1")
		s.CreateBranch("branch2")

		// parent (branch1) adds a conflicting change
		s.Checkout("branch1")
		err = s.Scene.Repo.CreateChangeAndCommit("branch1 content\n", "conflict.txt")
		require.NoError(t, err)
		s.TrackBranch("branch1", "main")

		// child (branch2) adds a conflicting change
		s.Checkout("branch2")
		err = s.Scene.Repo.CreateChangeAndCommit("branch2 content\n", "conflict.txt")
		require.NoError(t, err)
		s.TrackBranch("branch2", "branch1")

		// Fold branch2 into branch1 - should conflict
		err = Action(s.Context, Options{Keep: false}, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "due to conflicts. Please resolve the conflicts and run 'git commit'")
	})

	t.Run("restacks descendants after folding", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
				"branch3": "branch2",
			})

		// Make branch1 and branch2 both have commits
		s.Checkout("branch1")
		s.Scene.Repo.CreateChangeAndCommit("c1", "f1")
		s.Checkout("branch2")
		s.Scene.Repo.CreateChangeAndCommit("c2", "f2")
		s.Checkout("branch3")
		s.Scene.Repo.CreateChangeAndCommit("c3", "f3")

		// Fold branch2 into branch1
		s.Checkout("branch2")
		err := Action(s.Context, Options{Keep: false}, nil)
		require.NoError(t, err)

		// Verify branch3's parent is now branch1
		branchparent3 := s.Engine.GetBranch("branch3")
		parent3 := branchparent3.GetParent()
		require.NotNil(t, parent3)
		require.Equal(t, "branch1", parent3.GetName())

		// Verify branch3 contains all commits (c1, c2, c3)
		logOutput, err := s.Scene.Repo.RunGitCommandAndGetOutput("log", "--oneline", "main..branch3")
		require.NoError(t, err)
		require.Contains(t, logOutput, "c1")
		require.Contains(t, logOutput, "c2")
		require.Contains(t, logOutput, "c3")
	})

	t.Run("fails when rebase is in progress", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
			})

		// Start a rebase manually
		s.Checkout("branch2")
		// We can simulate a rebase by creating the .git/rebase-merge directory
		rebasePath := filepath.Join(s.Scene.Dir, ".git", "rebase-merge")
		err := os.MkdirAll(rebasePath, 0755)
		require.NoError(t, err)

		err = Action(s.Context, Options{Keep: false}, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "rebase is already in progress")
	})

	t.Run("folds branch with no unique commits", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
			})

		// branch2 has no unique commits (it's at the same position as branch1)
		s.Checkout("branch2")
		err := Action(s.Context, Options{Keep: false}, nil)
		require.NoError(t, err)

		// Verify branch2 is deleted and we're on branch1
		branches, _ := s.Engine.Git().GetAllBranchNames()
		require.NotContains(t, branches, "branch2")
		current, _ := s.Scene.Repo.CurrentBranchName()
		require.Equal(t, "branch1", current)
	})

	t.Run("takes snapshot before folding", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
			})

		s.Checkout("branch2")
		err := Action(s.Context, Options{Keep: false}, nil)
		require.NoError(t, err)

		// Verify snapshot exists
		snapshots, err := s.Engine.GetSnapshots()
		require.NoError(t, err)
		require.NotEmpty(t, snapshots)
		require.Equal(t, "fold", snapshots[0].Command)
	})

	t.Run("takes snapshot with --keep flag", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
			})

		s.Checkout("branch2")
		err := Action(s.Context, Options{Keep: true}, nil)
		require.NoError(t, err)

		// Verify snapshot exists with --keep arg
		snapshots, err := s.Engine.GetSnapshots()
		require.NoError(t, err)
		require.NotEmpty(t, snapshots)
		require.Equal(t, "fold", snapshots[0].Command)
		require.Contains(t, snapshots[0].Args, "--keep")
	})

	t.Run("fails when folding into trunk without --allow-trunk", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
			})

		s.Checkout("branch1")
		err := Action(s.Context, Options{Keep: false, AllowTrunk: false}, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "without --allow-trunk")
	})

	t.Run("folds bottom branch into trunk with --allow-trunk", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
			})

		s.Checkout("branch1")
		err := Action(s.Context, Options{Keep: false, AllowTrunk: true}, nil)
		require.NoError(t, err)

		// Verify branch1 is deleted
		branches, _ := s.Engine.Git().GetAllBranchNames()
		require.NotContains(t, branches, "branch1")

		// Verify we're on main
		current, _ := s.Scene.Repo.CurrentBranchName()
		require.Equal(t, "main", current)

		// Verify main contains branch1's commit
		logOutput, _ := s.Scene.Repo.RunGitCommandAndGetOutput("log", "--oneline", "-n", "1")
		require.Contains(t, logOutput, "change on branch1")
	})

	t.Run("fails when folding branches with different scopes", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
			})

		// Set different scopes on the branches
		branch1 := s.Engine.GetBranch("branch1")
		branch2 := s.Engine.GetBranch("branch2")
		err := s.Engine.SetScope(branch1, engine.NewScope("PROJ-123"))
		require.NoError(t, err)
		err = s.Engine.SetScope(branch2, engine.NewScope("PROJ-456"))
		require.NoError(t, err)

		// Switch to branch2 and try to fold
		s.Checkout("branch2")
		err = Action(s.Context, Options{Keep: false}, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot fold branches with different scopes")
		require.Contains(t, err.Error(), "[PROJ-456]")
		require.Contains(t, err.Error(), "[PROJ-123]")
	})

	t.Run("fails when current branch is locked", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
			})

		s.Checkout("branch2")
		branch2 := s.Engine.GetBranch("branch2")
		_, err := s.Engine.SetLocked([]engine.Branch{branch2}, engine.LockReasonUser)
		require.NoError(t, err)

		err = Action(s.Context, Options{Keep: false}, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "branch branch2 is locked")
	})

	t.Run("fails when parent branch is locked", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
			})

		s.Checkout("branch2")
		branch1 := s.Engine.GetBranch("branch1")
		_, err := s.Engine.SetLocked([]engine.Branch{branch1}, engine.LockReasonUser)
		require.NoError(t, err)

		err = Action(s.Context, Options{Keep: false}, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "branch branch1 is locked")
	})

	t.Run("fails when branch is frozen", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
			})

		s.Checkout("branch2")
		branch2 := s.Engine.GetBranch("branch2")
		_, err := s.Engine.SetFrozen([]engine.Branch{branch2}, true)
		require.NoError(t, err)

		err = Action(s.Context, Options{Keep: false}, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "branch branch2 is frozen")
	})

	t.Run("dry-run does not modify repository", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
			})

		s.Checkout("branch2")

		// Perform dry-run fold
		err := Action(s.Context, Options{DryRun: true}, nil)
		require.NoError(t, err)

		// Verify branch2 still exists
		branches, err := s.Engine.Git().GetAllBranchNames()
		require.NoError(t, err)
		require.Contains(t, branches, "branch2")

		// Verify we're still on branch2
		currentBranch, err := s.Scene.Repo.CurrentBranchName()
		require.NoError(t, err)
		require.Equal(t, "branch2", currentBranch)

		// Verify branch1 parent is still main
		branch1 := s.Engine.GetBranch("branch1")
		require.Equal(t, "main", branch1.GetParent().GetName())
	})

	t.Run("dry-run fails if branch is locked", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
			})

		s.Checkout("branch2")
		branch2 := s.Engine.GetBranch("branch2")
		_, err := s.Engine.SetLocked([]engine.Branch{branch2}, engine.LockReasonUser)
		require.NoError(t, err)

		// Dry-run should fail because branch is locked
		err = Action(s.Context, Options{DryRun: true}, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "branch branch2 is locked")
	})
}
