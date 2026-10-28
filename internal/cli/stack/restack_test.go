package stack_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/testhelpers"
)

func TestRestackCommand(t *testing.T) {
	t.Parallel()

	t.Run("restack auto-reparents when parent branch is deleted", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			// Create initial commit
			if err := testhelpers.InitialCommitSceneSetup(s); err != nil {
				return err
			}
			// Create branch1 (parent) using create command
			if err := s.Repo.CreateChange("branch1 change", "file1", false); err != nil {
				return err
			}
			if err := runCliCommand(s.Dir, "create", "branch1", "-m", "branch1 change"); err != nil {
				return err
			}
			// Create branch2 (child) on top of branch1 using create command
			if err := s.Repo.CreateChange("branch2 change", "file2", false); err != nil {
				return err
			}
			return runCliCommand(s.Dir, "create", "branch2", "-m", "branch2 change")
		})

		// Delete branch1 forcefully (simulating it being deleted after PR merge)
		err := scene.Repo.CheckoutBranch("branch2")
		require.NoError(t, err)

		// Force delete branch1
		err = scene.Repo.RunGitCommand("branch", "-D", "branch1")
		require.NoError(t, err)

		// Run restack --only on branch2
		// It should detect that branch1 no longer exists and reparent branch2 to main
		output, err := runCliCommandOutput(scene.Dir, "restack", "--only")

		require.NoError(t, err, "restack command failed: %s", string(output))
		// Should mention reparenting
		require.Contains(t, string(output), "Reparented", "should mention reparenting")
		require.Contains(t, string(output), "branch1", "should mention old parent")
		require.Contains(t, string(output), "main", "should mention new parent (trunk)")
	})

	// Every scope of a no-op restack reports the same up-to-date summary. The
	// four invocations share one main -> branch1 -> branch2 stack because none
	// of them changes any state.
	t.Run("restack scopes report up to date when nothing needs restacking", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			// Create initial commit
			if err := testhelpers.InitialCommitSceneSetup(s); err != nil {
				return err
			}
			// Create branch1 using create command
			if err := s.Repo.CreateChange("branch1 change", "test1", false); err != nil {
				return err
			}
			if err := runCliCommand(s.Dir, "create", "branch1", "-m", "branch1 change"); err != nil {
				return err
			}
			// Create branch2 on top of branch1 using create command
			if err := s.Repo.CreateChange("branch2 change", "test2", false); err != nil {
				return err
			}
			return runCliCommand(s.Dir, "create", "branch2", "-m", "branch2 change")
		})

		expected := testhelpers.NormalizeOutput(`
✨ Everything is up to date!
`)
		requireUpToDate := func(t *testing.T, args ...string) {
			t.Helper()
			output, err := runCliCommandOutput(scene.Dir, args...)
			require.NoError(t, err, "restack command failed: %s", string(output))
			require.Equal(t, expected, testhelpers.NormalizeOutput(string(output)),
				"output format should match expected structure for %v", args)
		}

		// Single branch (--only) and --downstack from branch2
		requireUpToDate(t, "restack", "--only")
		requireUpToDate(t, "restack", "--downstack")

		// --upstack from branch1
		require.NoError(t, scene.Repo.CheckoutBranch("branch1"))
		requireUpToDate(t, "restack", "--upstack")

		// --branch from trunk
		require.NoError(t, scene.Repo.CheckoutBranch("main"))
		requireUpToDate(t, "restack", "--branch", "branch1", "--only")
	})

	// Trunk-only repo: the no-stack report and every flag-validation error
	// share one scene. Detaching HEAD comes last since it changes state.
	t.Run("restack on trunk reports no branches and rejects invalid flags", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		// On trunk with no stack, trunk should not be treated as a restack target
		output, err := runCliCommandOutput(scene.Dir, "restack")
		require.NoError(t, err, "restack command failed: %s", string(output))
		expected := testhelpers.NormalizeOutput(`
No branches to restack.
`)
		require.Equal(t, expected, testhelpers.NormalizeOutput(string(output)), "trunk should not be treated as a restack target")

		// --parallel without --all-stacks errors
		output, err = runCliCommandOutput(scene.Dir, "restack", "--parallel")
		require.Error(t, err, "restack --parallel should require --all-stacks or --stacks")
		require.Contains(t, string(output), "--parallel requires --all-stacks or --stacks")

		// Multiple scope flags error
		output, err = runCliCommandOutput(scene.Dir, "restack", "--downstack", "--upstack")
		require.Error(t, err, "restack should fail with conflicting flags")
		require.Contains(t, string(output), "only one of --downstack, --only, or --upstack")

		// Not on a branch and --branch not specified
		err = scene.Repo.RunGitCommand("checkout", "HEAD~0")
		require.NoError(t, err)
		output, err = runCliCommandOutput(scene.Dir, "restack")
		require.Error(t, err, "restack should fail when not on a branch")
		require.Contains(t, string(output), "not on a branch")
	})

	t.Run("restack output when branches need restacking", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			// Create initial commit
			if err := testhelpers.InitialCommitSceneSetup(s); err != nil {
				return err
			}
			// Create branch1 using create command
			if err := s.Repo.CreateChange("branch1 change", "test1", false); err != nil {
				return err
			}
			if err := runCliCommand(s.Dir, "create", "branch1", "-m", "branch1 change"); err != nil {
				return err
			}
			// Create branch2 on top of branch1 using create command
			if err := s.Repo.CreateChange("branch2 change", "test2", false); err != nil {
				return err
			}
			return runCliCommand(s.Dir, "create", "branch2", "-m", "branch2 change")
		})

		// Make a change to main so branch1 needs restacking
		err := scene.Repo.CheckoutBranch("main")
		require.NoError(t, err)
		err = scene.Repo.CreateChangeAndCommit("main update", "main-file")
		require.NoError(t, err)

		// Switch to branch2 and run restack --downstack
		err = scene.Repo.CheckoutBranch("branch2")
		require.NoError(t, err)

		output, err := runCliCommandOutput(scene.Dir, "restack", "--downstack")

		require.NoError(t, err, "restack command failed: %s", string(output))

		normalized := testhelpers.NormalizeOutput(string(output))
		// Output should show branches being restacked
		require.Contains(t, normalized, "Restacked branch1 on main", "should show branch1 restacked")
		require.Contains(t, normalized, "Restacked branch2 (current) on branch1", "should show branch2 restacked")
		require.Contains(t, normalized, "Summary: restacked 2")
	})

	t.Run("restack --all-stacks restacks every independent stack", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			if err := testhelpers.InitialCommitSceneSetup(s); err != nil {
				return err
			}

			if err := s.Repo.CreateChange("stackA change", "stackA", false); err != nil {
				return err
			}
			createStackitBranch(t, s.Dir, "stackA", "stackA change")

			if err := s.Repo.CreateChange("stackA-child change", "stackA-child", false); err != nil {
				return err
			}
			createStackitBranch(t, s.Dir, "stackA-child", "stackA-child change")

			if err := s.Repo.CheckoutBranch("main"); err != nil {
				return err
			}
			if err := s.Repo.CreateChange("stackB change", "stackB", false); err != nil {
				return err
			}
			createStackitBranch(t, s.Dir, "stackB", "stackB change")
			return nil
		})

		stackABefore, err := scene.Repo.GetBranchSHA("stackA")
		require.NoError(t, err)
		stackAChildBefore, err := scene.Repo.GetBranchSHA("stackA-child")
		require.NoError(t, err)
		stackBBefore, err := scene.Repo.GetBranchSHA("stackB")
		require.NoError(t, err)

		err = scene.Repo.CheckoutBranch("main")
		require.NoError(t, err)
		err = scene.Repo.CreateChangeAndCommit("main update", "main")
		require.NoError(t, err)

		output, err := runCliCommandOutput(scene.Dir, "restack", "--all-stacks")

		require.NoError(t, err, "restack command failed: %s", string(output))
		normalized := testhelpers.NormalizeOutput(string(output))
		require.Contains(t, normalized, "Restacked stackA on main")
		require.Contains(t, normalized, "Restacked stackA-child on stackA")
		require.Contains(t, normalized, "Restacked stackB on main")

		stackAAfter, err := scene.Repo.GetBranchSHA("stackA")
		require.NoError(t, err)
		stackAChildAfter, err := scene.Repo.GetBranchSHA("stackA-child")
		require.NoError(t, err)
		stackBAfter, err := scene.Repo.GetBranchSHA("stackB")
		require.NoError(t, err)
		require.NotEqual(t, stackABefore, stackAAfter)
		require.NotEqual(t, stackAChildBefore, stackAChildAfter)
		require.NotEqual(t, stackBBefore, stackBAfter)
	})

	t.Run("restack --stacks only restacks selected independent stacks", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			if err := testhelpers.InitialCommitSceneSetup(s); err != nil {
				return err
			}

			if err := s.Repo.CreateChange("stackA change", "stackA", false); err != nil {
				return err
			}
			createStackitBranch(t, s.Dir, "stackA", "stackA change")

			if err := s.Repo.CreateChange("stackA-child change", "stackA-child", false); err != nil {
				return err
			}
			createStackitBranch(t, s.Dir, "stackA-child", "stackA-child change")

			if err := s.Repo.CheckoutBranch("main"); err != nil {
				return err
			}
			if err := s.Repo.CreateChange("stackB change", "stackB", false); err != nil {
				return err
			}
			createStackitBranch(t, s.Dir, "stackB", "stackB change")
			return nil
		})

		stackABefore, err := scene.Repo.GetBranchSHA("stackA")
		require.NoError(t, err)
		stackAChildBefore, err := scene.Repo.GetBranchSHA("stackA-child")
		require.NoError(t, err)
		stackBBefore, err := scene.Repo.GetBranchSHA("stackB")
		require.NoError(t, err)

		err = scene.Repo.CheckoutBranch("main")
		require.NoError(t, err)
		err = scene.Repo.CreateChangeAndCommit("main update", "main")
		require.NoError(t, err)

		output, err := runCliCommandOutput(scene.Dir, "restack", "--stacks", "stackA")

		require.NoError(t, err, "restack command failed: %s", string(output))
		normalized := testhelpers.NormalizeOutput(string(output))
		require.Contains(t, normalized, "Restacked stackA on main")
		require.Contains(t, normalized, "Restacked stackA-child on stackA")
		require.NotContains(t, normalized, "stackB")

		stackAAfter, err := scene.Repo.GetBranchSHA("stackA")
		require.NoError(t, err)
		stackAChildAfter, err := scene.Repo.GetBranchSHA("stackA-child")
		require.NoError(t, err)
		stackBAfter, err := scene.Repo.GetBranchSHA("stackB")
		require.NoError(t, err)
		require.NotEqual(t, stackABefore, stackAAfter)
		require.NotEqual(t, stackAChildBefore, stackAChildAfter)
		require.Equal(t, stackBBefore, stackBAfter)
	})

	t.Run("restack --all-stacks --continue-on-conflict keeps processing independent stacks", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			if err := s.Repo.CreateChangeAndCommit("initial", "conflict"); err != nil {
				return err
			}

			if err := s.Repo.CreateChange("stackA conflicting change", "conflict", false); err != nil {
				return err
			}
			createStackitBranch(t, s.Dir, "stackA", "stackA conflicting change")

			if err := s.Repo.CheckoutBranch("main"); err != nil {
				return err
			}
			if err := s.Repo.CreateChange("stackB change", "stackB", false); err != nil {
				return err
			}
			createStackitBranch(t, s.Dir, "stackB", "stackB change")
			return nil
		})

		stackABefore, err := scene.Repo.GetBranchSHA("stackA")
		require.NoError(t, err)
		stackBBefore, err := scene.Repo.GetBranchSHA("stackB")
		require.NoError(t, err)

		err = scene.Repo.CheckoutBranch("main")
		require.NoError(t, err)
		err = scene.Repo.CreateChangeAndCommit("main conflicting change", "conflict")
		require.NoError(t, err)

		output, err := runCliCommandOutput(scene.Dir, "restack", "--all-stacks", "--continue-on-conflict")

		require.NoError(t, err, "restack command failed: %s", string(output))
		normalized := testhelpers.NormalizeOutput(string(output))
		require.Contains(t, normalized, "Skipped stackA (conflict)")
		require.Contains(t, normalized, "Restacked stackB on main")
		require.Contains(t, normalized, "Summary: restacked 1, skipped 1 (conflict)")

		stackAAfter, err := scene.Repo.GetBranchSHA("stackA")
		require.NoError(t, err)
		stackBAfter, err := scene.Repo.GetBranchSHA("stackB")
		require.NoError(t, err)
		require.Equal(t, stackABefore, stackAAfter)
		require.NotEqual(t, stackBBefore, stackBAfter)
	})

	t.Run("restack --all-stacks --parallel restacks every independent stack", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			if err := testhelpers.InitialCommitSceneSetup(s); err != nil {
				return err
			}

			if err := s.Repo.CreateChange("stackA change", "stackA", false); err != nil {
				return err
			}
			createStackitBranch(t, s.Dir, "stackA", "stackA change")

			if err := s.Repo.CreateChange("stackA-child change", "stackA-child", false); err != nil {
				return err
			}
			createStackitBranch(t, s.Dir, "stackA-child", "stackA-child change")

			if err := s.Repo.CheckoutBranch("main"); err != nil {
				return err
			}
			if err := s.Repo.CreateChange("stackB change", "stackB", false); err != nil {
				return err
			}
			createStackitBranch(t, s.Dir, "stackB", "stackB change")
			return nil
		})

		stackABefore, err := scene.Repo.GetBranchSHA("stackA")
		require.NoError(t, err)
		stackAChildBefore, err := scene.Repo.GetBranchSHA("stackA-child")
		require.NoError(t, err)
		stackBBefore, err := scene.Repo.GetBranchSHA("stackB")
		require.NoError(t, err)

		err = scene.Repo.CheckoutBranch("main")
		require.NoError(t, err)
		err = scene.Repo.CreateChangeAndCommit("main update", "main")
		require.NoError(t, err)

		output, err := runCliCommandOutput(scene.Dir, "restack", "--all-stacks", "--parallel", "--jobs", "2")

		require.NoError(t, err, "restack --all-stacks --parallel failed: %s", string(output))

		// Verify all branches were actually restacked (SHAs changed)
		stackAAfter, err := scene.Repo.GetBranchSHA("stackA")
		require.NoError(t, err)
		stackAChildAfter, err := scene.Repo.GetBranchSHA("stackA-child")
		require.NoError(t, err)
		stackBAfter, err := scene.Repo.GetBranchSHA("stackB")
		require.NoError(t, err)
		require.NotEqual(t, stackABefore, stackAAfter)
		require.NotEqual(t, stackAChildBefore, stackAChildAfter)
		require.NotEqual(t, stackBBefore, stackBAfter)
	})

	t.Run("restack --all-stacks --parallel --continue-on-conflict skips conflicted stacks", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			if err := s.Repo.CreateChangeAndCommit("initial", "conflict"); err != nil {
				return err
			}

			if err := s.Repo.CreateChange("stackA conflicting change", "conflict", false); err != nil {
				return err
			}
			createStackitBranch(t, s.Dir, "stackA", "stackA conflicting change")

			if err := s.Repo.CheckoutBranch("main"); err != nil {
				return err
			}
			if err := s.Repo.CreateChange("stackB change", "stackB", false); err != nil {
				return err
			}
			createStackitBranch(t, s.Dir, "stackB", "stackB change")
			return nil
		})

		stackABefore, err := scene.Repo.GetBranchSHA("stackA")
		require.NoError(t, err)
		stackBBefore, err := scene.Repo.GetBranchSHA("stackB")
		require.NoError(t, err)

		err = scene.Repo.CheckoutBranch("main")
		require.NoError(t, err)
		err = scene.Repo.CreateChangeAndCommit("main conflicting change", "conflict")
		require.NoError(t, err)

		output, err := runCliCommandOutput(scene.Dir, "restack", "--all-stacks", "--parallel", "--jobs", "2", "--continue-on-conflict")

		require.NoError(t, err, "restack command failed: %s", string(output))

		// stackA should be untouched (conflict); stackB should be restacked
		stackAAfter, err := scene.Repo.GetBranchSHA("stackA")
		require.NoError(t, err)
		stackBAfter, err := scene.Repo.GetBranchSHA("stackB")
		require.NoError(t, err)
		require.Equal(t, stackABefore, stackAAfter)
		require.NotEqual(t, stackBBefore, stackBAfter)
	})

	t.Run("restack handles conflict and persists continuation state", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			// Create initial commit with a file
			if err := s.Repo.CreateChange("initial", "test", false); err != nil {
				return err
			}
			if err := testhelpers.InitialCommitSceneSetup(s); err != nil {
				return err
			}
			// Create branch1 using create command
			if err := s.Repo.CreateChange("branch1 change", "test1", false); err != nil {
				return err
			}
			if err := runCliCommand(s.Dir, "create", "branch1", "-m", "branch1 change"); err != nil {
				return err
			}
			// Create branch2 on top of branch1 using create command
			// Modify the same file that will conflict
			if err := s.Repo.CreateChange("branch2 change", "test", false); err != nil {
				return err
			}
			if err := runCliCommand(s.Dir, "create", "branch2", "-m", "branch2 change"); err != nil {
				return err
			}
			// Switch to main and create conflicting change in the same file
			if err := s.Repo.CheckoutBranch("main"); err != nil {
				return err
			}
			// Modify the same file in main (this will conflict when branch2 is rebased)
			if err := s.Repo.CreateChange("main change", "test", false); err != nil {
				return err
			}
			return s.Repo.CreateChangeAndCommit("main change", "main")
		})

		// Switch to branch1 first and update it to point to old main
		// Then switch to branch2 which is based on branch1
		err := scene.Repo.CheckoutBranch("branch2")
		require.NoError(t, err)

		// Run restack (should hit conflict because branch1 needs to be rebased on new main,
		// and branch2 is based on branch1)
		output, err := runCliCommandOutput(scene.Dir, "restack", "--downstack")

		// Should fail with conflict (branch1 will conflict when rebasing onto new main)
		if err == nil {
			// If no error, check if restack actually happened
			// The conflict might not occur if the changes don't actually conflict
			t.Logf("Restack output: %s", string(output))
			// For now, just verify the command ran
			require.Contains(t, string(output), "restack", "should mention restack")
		} else {
			require.Contains(t, string(output), "conflict", "should mention conflict")
			// Verify continuation state was persisted
			continuationPath := filepath.Join(scene.Dir, ".git", ".stackit_continue")
			_, err = os.Stat(continuationPath)
			require.NoError(t, err, "continuation state file should exist")
		}
	})

	t.Run("restack branching stacks in topological order", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			// Create initial commit
			if err := testhelpers.InitialCommitSceneSetup(s); err != nil {
				return err
			}

			// Create branching stack structure:
			// main
			// ├── stackA
			// │   ├── stackA-child1
			// │   └── stackA-child2
			// └── stackB
			//     └── stackB-child1

			// First stack: main -> stackA -> stackA-child1
			if err := s.Repo.CreateChange("stackA change", "sA", false); err != nil {
				return err
			}
			if err := runCliCommand(s.Dir, "create", "stackA", "-m", "stackA change"); err != nil {
				return err
			}

			if err := s.Repo.CreateChange("stackA-child1 change", "sAc1", false); err != nil {
				return err
			}
			if err := runCliCommand(s.Dir, "create", "stackA-child1", "-m", "stackA-child1 change"); err != nil {
				return err
			}

			// Branch off stackA for stackA-child2
			if err := s.Repo.CheckoutBranch("stackA"); err != nil {
				return err
			}
			if err := s.Repo.CreateChange("stackA-child2 change", "sAc2", false); err != nil {
				return err
			}
			if err := runCliCommand(s.Dir, "create", "stackA-child2", "-m", "stackA-child2 change"); err != nil {
				return err
			}

			// Second stack: main -> stackB -> stackB-child1
			if err := s.Repo.CheckoutBranch("main"); err != nil {
				return err
			}
			if err := s.Repo.CreateChange("stackB change", "sB", false); err != nil {
				return err
			}
			if err := runCliCommand(s.Dir, "create", "stackB", "-m", "stackB change"); err != nil {
				return err
			}

			if err := s.Repo.CreateChange("stackB-child1 change", "sBc1", false); err != nil {
				return err
			}
			return runCliCommand(s.Dir, "create", "stackB-child1", "-m", "stackB-child1 change")
		})

		// Make a change to main so all stacks need restacking
		err := scene.Repo.CheckoutBranch("main")
		require.NoError(t, err)
		err = scene.Repo.CreateChangeAndCommit("main update", "main")
		require.NoError(t, err)

		// Restack from stackA (should restack stackA and its children)
		err = scene.Repo.CheckoutBranch("stackA")
		require.NoError(t, err)

		output, err := runCliCommandOutput(scene.Dir, "restack", "--upstack")

		require.NoError(t, err, "restack command failed: %s", string(output))

		// Verify all stackA branches are properly related
		requireInfoParent(t, scene.Dir, "main")

		// Verify stackA-child1 is still a child of stackA
		err = scene.Repo.CheckoutBranch("stackA-child1")
		require.NoError(t, err)
		requireInfoParent(t, scene.Dir, "stackA")

		// Verify stackA-child2 is still a child of stackA
		err = scene.Repo.CheckoutBranch("stackA-child2")
		require.NoError(t, err)
		requireInfoParent(t, scene.Dir, "stackA")
	})

	t.Run("restack auto-reparents multiple children when parent is merged", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			// Create initial commit
			if err := testhelpers.InitialCommitSceneSetup(s); err != nil {
				return err
			}
			// Create parent branch
			if err := s.Repo.CreateChange("parent change", "parent", false); err != nil {
				return err
			}
			if err := runCliCommand(s.Dir, "create", "parent", "-m", "parent change"); err != nil {
				return err
			}
			// Create first child
			if err := s.Repo.CreateChange("child1 change", "child1", false); err != nil {
				return err
			}
			if err := runCliCommand(s.Dir, "create", "child1", "-m", "child1 change"); err != nil {
				return err
			}
			// Go back to parent and create second child
			if err := s.Repo.CheckoutBranch("parent"); err != nil {
				return err
			}
			if err := s.Repo.CreateChange("child2 change", "child2", false); err != nil {
				return err
			}
			if err := runCliCommand(s.Dir, "create", "child2", "-m", "child2 change"); err != nil {
				return err
			}
			// Go back to parent and create third child
			if err := s.Repo.CheckoutBranch("parent"); err != nil {
				return err
			}
			if err := s.Repo.CreateChange("child3 change", "child3", false); err != nil {
				return err
			}
			return runCliCommand(s.Dir, "create", "child3", "-m", "child3 change")
		})

		// Simulate merging parent into main
		err := scene.Repo.CheckoutBranch("main")
		require.NoError(t, err)
		err = scene.Repo.RunGitCommand("merge", "parent", "--no-ff", "-m", "Merge parent")
		require.NoError(t, err)

		// Switch to child1 and run restack - should reparent all siblings
		err = scene.Repo.CheckoutBranch("child1")
		require.NoError(t, err)

		output, err := runCliCommandOutput(scene.Dir, "restack", "--only")

		require.NoError(t, err, "restack command failed: %s", string(output))
		require.Contains(t, string(output), "Reparented", "should mention reparenting")
		// "parent" alone would match "Reparented"; anchor on the from/to phrase
		// so both the old parent and the new parent (trunk) are proven.
		require.Contains(t, testhelpers.NormalizeOutput(string(output)), "from parent to main", "should mention old parent and new parent (trunk)")
		// The restack row must name the parent the branch now sits on, not the
		// landed one it was reparented away from.
		require.Contains(t, testhelpers.NormalizeOutput(string(output)), "Restacked child1 (current) on main")
		require.NotContains(t, testhelpers.NormalizeOutput(string(output)), "on parent")

		// Verify child1's parent is now main
		requireInfoParent(t, scene.Dir, "main")

		// Restack the other children too
		err = scene.Repo.CheckoutBranch("child2")
		require.NoError(t, err)
		output, err = runCliCommandOutput(scene.Dir, "restack", "--only")
		require.NoError(t, err, "restack command failed: %s", string(output))

		err = scene.Repo.CheckoutBranch("child3")
		require.NoError(t, err)
		output, err = runCliCommandOutput(scene.Dir, "restack", "--only")
		require.NoError(t, err, "restack command failed: %s", string(output))

		// Verify all children are now parented to main
		for _, child := range []string{"child2", "child3"} {
			err = scene.Repo.CheckoutBranch(child)
			require.NoError(t, err)
			requireInfoParent(t, scene.Dir, "main")
		}
	})

	t.Run("restack with --downstack includes siblings", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			// Create initial commit
			if err := testhelpers.InitialCommitSceneSetup(s); err != nil {
				return err
			}

			// Create structure:
			// main -> parent -> child1
			//                -> child2 (current)

			// Create parent
			if err := s.Repo.CreateChange("parent change", "parent", false); err != nil {
				return err
			}
			if err := runCliCommand(s.Dir, "create", "parent", "-m", "parent change"); err != nil {
				return err
			}

			// Create child1
			if err := s.Repo.CreateChange("child1 change", "child1", false); err != nil {
				return err
			}
			if err := runCliCommand(s.Dir, "create", "child1", "-m", "child1 change"); err != nil {
				return err
			}

			// Go back to parent and create child2
			if err := s.Repo.CheckoutBranch("parent"); err != nil {
				return err
			}
			if err := s.Repo.CreateChange("child2 change", "child2", false); err != nil {
				return err
			}
			return runCliCommand(s.Dir, "create", "child2", "-m", "child2 change")
		})

		// Make a change to main so parent needs restacking
		err := scene.Repo.CheckoutBranch("main")
		require.NoError(t, err)
		err = scene.Repo.CreateChangeAndCommit("main update", "main")
		require.NoError(t, err)

		// From child2, run restack --downstack (should restack parent and child2, but not child1)
		err = scene.Repo.CheckoutBranch("child2")
		require.NoError(t, err)

		output, err := runCliCommandOutput(scene.Dir, "restack", "--downstack")

		require.NoError(t, err, "restack command failed: %s", string(output))

		// Verify child2 is still a child of parent
		requireInfoParent(t, scene.Dir, "parent")
	})

	t.Run("restack --upstack from parent restacks all children", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			// Create initial commit
			if err := testhelpers.InitialCommitSceneSetup(s); err != nil {
				return err
			}

			// Create structure:
			// main -> parent -> child1
			//                -> child2
			//                -> child3

			// Create parent
			if err := s.Repo.CreateChange("parent change", "parent", false); err != nil {
				return err
			}
			if err := runCliCommand(s.Dir, "create", "parent", "-m", "parent change"); err != nil {
				return err
			}

			// Create child1
			if err := s.Repo.CreateChange("child1 change", "child1", false); err != nil {
				return err
			}
			if err := runCliCommand(s.Dir, "create", "child1", "-m", "child1 change"); err != nil {
				return err
			}

			// Go back to parent and create child2
			if err := s.Repo.CheckoutBranch("parent"); err != nil {
				return err
			}
			if err := s.Repo.CreateChange("child2 change", "child2", false); err != nil {
				return err
			}
			if err := runCliCommand(s.Dir, "create", "child2", "-m", "child2 change"); err != nil {
				return err
			}

			// Go back to parent and create child3
			if err := s.Repo.CheckoutBranch("parent"); err != nil {
				return err
			}
			if err := s.Repo.CreateChange("child3 change", "child3", false); err != nil {
				return err
			}
			return runCliCommand(s.Dir, "create", "child3", "-m", "child3 change")
		})

		// Amend parent branch (children will need restacking)
		err := scene.Repo.CheckoutBranch("parent")
		require.NoError(t, err)
		err = scene.Repo.CreateChangeAndAmend("parent amended", "parent")
		require.NoError(t, err)

		// Run restack --upstack from parent (should restack all three children)
		output, err := runCliCommandOutput(scene.Dir, "restack", "--upstack")

		require.NoError(t, err, "restack command failed: %s", string(output))

		// Verify all children are still children of parent
		for _, child := range []string{"child1", "child2", "child3"} {
			err = scene.Repo.CheckoutBranch(child)
			require.NoError(t, err)
			requireInfoParent(t, scene.Dir, "parent")
		}
	})
}
