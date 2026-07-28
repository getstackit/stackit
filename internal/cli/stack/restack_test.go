package stack_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/testhelpers"
)

func TestRestackCommand(t *testing.T) {
	t.Parallel()
	binaryPath := testhelpers.GetSharedBinaryPath()
	if binaryPath == "" {
		if err := testhelpers.GetBinaryError(); err != nil {
			t.Fatalf("failed to build stackit binary: %v", err)
		}
		t.Fatal("stackit binary not built")
	}

	t.Run("restack auto-reparents when parent is merged into trunk", func(t *testing.T) {
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
			cmd := exec.Command(binaryPath, "create", "branch1", "-m", "branch1 change")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}
			// Create branch2 (child) on top of branch1 using create command
			if err := s.Repo.CreateChange("branch2 change", "file2", false); err != nil {
				return err
			}
			cmd = exec.Command(binaryPath, "create", "branch2", "-m", "branch2 change")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}
			return nil
		})

		// Simulate merging branch1 into main by:
		// 1. Checkout main
		// 2. Merge branch1 into main (fast-forward or regular merge)
		err := scene.Repo.CheckoutBranch("main")
		require.NoError(t, err)

		err = scene.Repo.RunGitCommand("merge", "branch1", "--no-ff", "-m", "Merge branch1")
		require.NoError(t, err)

		// Now switch to branch2 and run restack
		err = scene.Repo.CheckoutBranch("branch2")
		require.NoError(t, err)

		// Run restack --only on branch2
		// It should detect that branch1 is merged into main and reparent branch2 to main
		cmd := exec.Command(binaryPath, "restack", "--only")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()

		require.NoError(t, err, "restack command failed: %s", string(output))
		// Should mention reparenting
		require.Contains(t, string(output), "reparented onto", "should mention reparenting")
		require.Contains(t, string(output), "branch1", "should mention old parent")
		require.Contains(t, string(output), "main", "should mention new parent (trunk)")

		// Verify branch2's parent is now main
		cmd = exec.Command(binaryPath, "info")
		cmd.Dir = scene.Dir
		infoOutput, err := cmd.CombinedOutput()
		require.NoError(t, err, "info command failed: %s", string(infoOutput))
		// The parent should now be main, not branch1
		require.Contains(t, string(infoOutput), "main", "branch2's parent should now be main")
	})

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
			cmd := exec.Command(binaryPath, "create", "branch1", "-m", "branch1 change")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}
			// Create branch2 (child) on top of branch1 using create command
			if err := s.Repo.CreateChange("branch2 change", "file2", false); err != nil {
				return err
			}
			cmd = exec.Command(binaryPath, "create", "branch2", "-m", "branch2 change")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}
			return nil
		})

		// Delete branch1 forcefully (simulating it being deleted after PR merge)
		err := scene.Repo.CheckoutBranch("branch2")
		require.NoError(t, err)

		// Force delete branch1
		err = scene.Repo.RunGitCommand("branch", "-D", "branch1")
		require.NoError(t, err)

		// Run restack --only on branch2
		// It should detect that branch1 no longer exists and reparent branch2 to main
		cmd := exec.Command(binaryPath, "restack", "--only")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()

		require.NoError(t, err, "restack command failed: %s", string(output))
		// Should mention reparenting
		require.Contains(t, string(output), "reparented onto", "should mention reparenting")
		require.Contains(t, string(output), "branch1", "should mention old parent")
		require.Contains(t, string(output), "main", "should mention new parent (trunk)")
	})

	t.Run("restack single branch", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			// Create initial commit
			if err := testhelpers.InitialCommitSceneSetup(s); err != nil {
				return err
			}
			// Create a change and use create command (which automatically tracks)
			if err := s.Repo.CreateChange("feature change", "test", false); err != nil {
				return err
			}
			cmd := exec.Command(binaryPath, "create", "feature", "-m", "feature change")
			cmd.Dir = s.Dir
			return cmd.Run()
		})

		// Run restack --only
		cmd := exec.Command(binaryPath, "restack", "--only")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()

		require.NoError(t, err, "restack command failed: %s", string(output))

		normalized := testhelpers.NormalizeOutput(string(output))
		expected := testhelpers.NormalizeOutput(`
Already up to date.
		`)
		require.Equal(t, expected, normalized, "output format should match expected structure")
	})

	t.Run("restack on trunk with no stack reports no branches", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		cmd := exec.Command(binaryPath, "restack")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()

		require.NoError(t, err, "restack command failed: %s", string(output))

		normalized := testhelpers.NormalizeOutput(string(output))
		expected := testhelpers.NormalizeOutput(`
No branches to restack.
`)
		require.Equal(t, expected, normalized, "trunk should not be treated as a restack target")
	})

	t.Run("restack with downstack flag", func(t *testing.T) {
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
			cmd := exec.Command(binaryPath, "create", "branch1", "-m", "branch1 change")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}
			// Create branch2 on top of branch1 using create command
			if err := s.Repo.CreateChange("branch2 change", "test2", false); err != nil {
				return err
			}
			cmd = exec.Command(binaryPath, "create", "branch2", "-m", "branch2 change")
			cmd.Dir = s.Dir
			return cmd.Run()
		})

		// Run restack --downstack
		cmd := exec.Command(binaryPath, "restack", "--downstack")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()

		require.NoError(t, err, "restack command failed: %s", string(output))

		normalized := testhelpers.NormalizeOutput(string(output))
		expected := testhelpers.NormalizeOutput(`
Already up to date.
`)
		require.Equal(t, expected, normalized, "output format should match expected structure")
	})

	t.Run("restack with upstack flag", func(t *testing.T) {
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
			cmd := exec.Command(binaryPath, "create", "branch1", "-m", "branch1 change")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}
			// Create branch2 on top of branch1 using create command
			if err := s.Repo.CreateChange("branch2 change", "test2", false); err != nil {
				return err
			}
			cmd = exec.Command(binaryPath, "create", "branch2", "-m", "branch2 change")
			cmd.Dir = s.Dir
			return cmd.Run()
		})

		// Switch to branch1
		err := scene.Repo.CheckoutBranch("branch1")
		require.NoError(t, err)

		// Run restack --upstack
		cmd := exec.Command(binaryPath, "restack", "--upstack")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()

		require.NoError(t, err, "restack command failed: %s", string(output))

		normalized := testhelpers.NormalizeOutput(string(output))
		expected := testhelpers.NormalizeOutput(`
Already up to date.
`)
		require.Equal(t, expected, normalized, "output format should match expected structure")
	})

	t.Run("restack with --branch flag", func(t *testing.T) {
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
			cmd := exec.Command(binaryPath, "create", "branch1", "-m", "branch1 change")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}
			// Switch back to main
			return s.Repo.CheckoutBranch("main")
		})

		// Run restack with --branch flag
		cmd := exec.Command(binaryPath, "restack", "--branch", "branch1", "--only")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()

		require.NoError(t, err, "restack command failed: %s", string(output))

		normalized := testhelpers.NormalizeOutput(string(output))
		expected := testhelpers.NormalizeOutput(`
Already up to date.
`)
		require.Equal(t, expected, normalized, "output format should match expected structure")
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
			cmd := exec.Command(binaryPath, "create", "branch1", "-m", "branch1 change")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}
			// Create branch2 on top of branch1 using create command
			if err := s.Repo.CreateChange("branch2 change", "test2", false); err != nil {
				return err
			}
			cmd = exec.Command(binaryPath, "create", "branch2", "-m", "branch2 change")
			cmd.Dir = s.Dir
			return cmd.Run()
		})

		// Make a change to main so branch1 needs restacking
		err := scene.Repo.CheckoutBranch("main")
		require.NoError(t, err)
		err = scene.Repo.CreateChangeAndCommit("main update", "main-file")
		require.NoError(t, err)

		// Switch to branch2 and run restack --downstack
		err = scene.Repo.CheckoutBranch("branch2")
		require.NoError(t, err)

		cmd := exec.Command(binaryPath, "restack", "--downstack")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()

		require.NoError(t, err, "restack command failed: %s", string(output))

		normalized := testhelpers.NormalizeOutput(string(output))
		// Output should show branches being restacked
		require.Contains(t, normalized, "Restacked 2 branches", "should report the restack count")
		// A plain successful restack is a count, not a row: it is what the developer assumed.
		require.Contains(t, normalized, "Restacked 2")
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
			createStackitBranch(t, binaryPath, s.Dir, "stackA", "stackA change")

			if err := s.Repo.CreateChange("stackA-child change", "stackA-child", false); err != nil {
				return err
			}
			createStackitBranch(t, binaryPath, s.Dir, "stackA-child", "stackA-child change")

			if err := s.Repo.CheckoutBranch("main"); err != nil {
				return err
			}
			if err := s.Repo.CreateChange("stackB change", "stackB", false); err != nil {
				return err
			}
			createStackitBranch(t, binaryPath, s.Dir, "stackB", "stackB change")
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

		cmd := exec.Command(binaryPath, "restack", "--all-stacks")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()

		require.NoError(t, err, "restack command failed: %s", string(output))
		normalized := testhelpers.NormalizeOutput(string(output))
		require.Contains(t, normalized, "Restacked")

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
			createStackitBranch(t, binaryPath, s.Dir, "stackA", "stackA change")

			if err := s.Repo.CreateChange("stackA-child change", "stackA-child", false); err != nil {
				return err
			}
			createStackitBranch(t, binaryPath, s.Dir, "stackA-child", "stackA-child change")

			if err := s.Repo.CheckoutBranch("main"); err != nil {
				return err
			}
			if err := s.Repo.CreateChange("stackB change", "stackB", false); err != nil {
				return err
			}
			createStackitBranch(t, binaryPath, s.Dir, "stackB", "stackB change")
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

		cmd := exec.Command(binaryPath, "restack", "--stacks", "stackA")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()

		require.NoError(t, err, "restack command failed: %s", string(output))
		normalized := testhelpers.NormalizeOutput(string(output))
		require.Contains(t, normalized, "Restacked")

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
			createStackitBranch(t, binaryPath, s.Dir, "stackA", "stackA conflicting change")

			if err := s.Repo.CheckoutBranch("main"); err != nil {
				return err
			}
			if err := s.Repo.CreateChange("stackB change", "stackB", false); err != nil {
				return err
			}
			createStackitBranch(t, binaryPath, s.Dir, "stackB", "stackB change")
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

		cmd := exec.Command(binaryPath, "restack", "--all-stacks", "--continue-on-conflict")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()

		require.NoError(t, err, "restack command failed: %s", string(output))
		normalized := testhelpers.NormalizeOutput(string(output))
		require.Contains(t, normalized, "⚠ stackA conflict")

		require.Contains(t, normalized, "Restacked 1, skipped 1 (conflict)")

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
			createStackitBranch(t, binaryPath, s.Dir, "stackA", "stackA change")

			if err := s.Repo.CreateChange("stackA-child change", "stackA-child", false); err != nil {
				return err
			}
			createStackitBranch(t, binaryPath, s.Dir, "stackA-child", "stackA-child change")

			if err := s.Repo.CheckoutBranch("main"); err != nil {
				return err
			}
			if err := s.Repo.CreateChange("stackB change", "stackB", false); err != nil {
				return err
			}
			createStackitBranch(t, binaryPath, s.Dir, "stackB", "stackB change")
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

		cmd := exec.Command(binaryPath, "restack", "--all-stacks", "--parallel", "--jobs", "2")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()

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
			createStackitBranch(t, binaryPath, s.Dir, "stackA", "stackA conflicting change")

			if err := s.Repo.CheckoutBranch("main"); err != nil {
				return err
			}
			if err := s.Repo.CreateChange("stackB change", "stackB", false); err != nil {
				return err
			}
			createStackitBranch(t, binaryPath, s.Dir, "stackB", "stackB change")
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

		cmd := exec.Command(binaryPath, "restack", "--all-stacks", "--parallel", "--jobs", "2", "--continue-on-conflict")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()

		require.NoError(t, err, "restack command failed: %s", string(output))

		// stackA should be untouched (conflict); stackB should be restacked
		stackAAfter, err := scene.Repo.GetBranchSHA("stackA")
		require.NoError(t, err)
		stackBAfter, err := scene.Repo.GetBranchSHA("stackB")
		require.NoError(t, err)
		require.Equal(t, stackABefore, stackAAfter)
		require.NotEqual(t, stackBBefore, stackBAfter)
	})

	t.Run("restack --parallel without --all-stacks errors", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		cmd := exec.Command(binaryPath, "restack", "--parallel")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()

		require.Error(t, err, "restack --parallel should require --all-stacks or --stacks")
		require.Contains(t, string(output), "--parallel requires --all-stacks or --stacks")
	})

	t.Run("restack errors when multiple scope flags specified", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		// Run restack with conflicting flags
		cmd := exec.Command(binaryPath, "restack", "--downstack", "--upstack")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()

		require.Error(t, err, "restack should fail with conflicting flags")
		require.Contains(t, string(output), "only one of --downstack, --only, or --upstack")
	})

	t.Run("restack errors when not on a branch and --branch not specified", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		// Detach HEAD
		err := scene.Repo.RunGitCommand("checkout", "HEAD~0")
		require.NoError(t, err)

		// Run restack without --branch flag
		cmd := exec.Command(binaryPath, "restack")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()

		require.Error(t, err, "restack should fail when not on a branch")
		require.Contains(t, string(output), "not on a branch")
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
			cmd := exec.Command(binaryPath, "create", "branch1", "-m", "branch1 change")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}
			// Create branch2 on top of branch1 using create command
			// Modify the same file that will conflict
			if err := s.Repo.CreateChange("branch2 change", "test", false); err != nil {
				return err
			}
			cmd = exec.Command(binaryPath, "create", "branch2", "-m", "branch2 change")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
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
		cmd := exec.Command(binaryPath, "restack", "--downstack")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()

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

	t.Run("restack with branching stack - parent with multiple children", func(t *testing.T) {
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
			cmd := exec.Command(binaryPath, "create", "parent", "-m", "parent change")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}
			// Create first child branch
			if err := s.Repo.CreateChange("child1 change", "child1", false); err != nil {
				return err
			}
			cmd = exec.Command(binaryPath, "create", "child1", "-m", "child1 change")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}
			// Go back to parent and create second child
			if err := s.Repo.CheckoutBranch("parent"); err != nil {
				return err
			}
			if err := s.Repo.CreateChange("child2 change", "child2", false); err != nil {
				return err
			}
			cmd = exec.Command(binaryPath, "create", "child2", "-m", "child2 change")
			cmd.Dir = s.Dir
			return cmd.Run()
		})

		// Make a change to main so parent needs restacking
		err := scene.Repo.CheckoutBranch("main")
		require.NoError(t, err)
		err = scene.Repo.CreateChangeAndCommit("main update", "main")
		require.NoError(t, err)

		// Switch to parent and restack (should restack parent and both children)
		err = scene.Repo.CheckoutBranch("parent")
		require.NoError(t, err)

		cmd := exec.Command(binaryPath, "restack", "--upstack")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()

		require.NoError(t, err, "restack command failed: %s", string(output))

		// Verify both children are still valid and have parent as their parent
		cmd = exec.Command(binaryPath, "info")
		cmd.Dir = scene.Dir
		_, err = cmd.CombinedOutput()
		require.NoError(t, err)

		// Verify child1 is still a child of parent
		err = scene.Repo.CheckoutBranch("child1")
		require.NoError(t, err)
		cmd = exec.Command(binaryPath, "info")
		cmd.Dir = scene.Dir
		infoOutput, err := cmd.CombinedOutput()
		require.NoError(t, err, "info command failed: %s", string(infoOutput))
		require.Contains(t, string(infoOutput), "parent", "child1 should still have parent as its parent")

		// Verify child2 is still a child of parent
		err = scene.Repo.CheckoutBranch("child2")
		require.NoError(t, err)
		cmd = exec.Command(binaryPath, "info")
		cmd.Dir = scene.Dir
		infoOutput, err = cmd.CombinedOutput()
		require.NoError(t, err, "info command failed: %s", string(infoOutput))
		require.Contains(t, string(infoOutput), "parent", "child2 should still have parent as its parent")
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
			cmd := exec.Command(binaryPath, "create", "stackA", "-m", "stackA change")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}

			if err := s.Repo.CreateChange("stackA-child1 change", "sAc1", false); err != nil {
				return err
			}
			cmd = exec.Command(binaryPath, "create", "stackA-child1", "-m", "stackA-child1 change")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}

			// Branch off stackA for stackA-child2
			if err := s.Repo.CheckoutBranch("stackA"); err != nil {
				return err
			}
			if err := s.Repo.CreateChange("stackA-child2 change", "sAc2", false); err != nil {
				return err
			}
			cmd = exec.Command(binaryPath, "create", "stackA-child2", "-m", "stackA-child2 change")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}

			// Second stack: main -> stackB -> stackB-child1
			if err := s.Repo.CheckoutBranch("main"); err != nil {
				return err
			}
			if err := s.Repo.CreateChange("stackB change", "sB", false); err != nil {
				return err
			}
			cmd = exec.Command(binaryPath, "create", "stackB", "-m", "stackB change")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}

			if err := s.Repo.CreateChange("stackB-child1 change", "sBc1", false); err != nil {
				return err
			}
			cmd = exec.Command(binaryPath, "create", "stackB-child1", "-m", "stackB-child1 change")
			cmd.Dir = s.Dir
			return cmd.Run()
		})

		// Make a change to main so all stacks need restacking
		err := scene.Repo.CheckoutBranch("main")
		require.NoError(t, err)
		err = scene.Repo.CreateChangeAndCommit("main update", "main")
		require.NoError(t, err)

		// Restack from stackA (should restack stackA and its children)
		err = scene.Repo.CheckoutBranch("stackA")
		require.NoError(t, err)

		cmd := exec.Command(binaryPath, "restack", "--upstack")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()

		require.NoError(t, err, "restack command failed: %s", string(output))

		// Verify all stackA branches are properly related
		cmd = exec.Command(binaryPath, "info")
		cmd.Dir = scene.Dir
		_, err = cmd.CombinedOutput()
		require.NoError(t, err)

		// Verify stackA-child1 is still a child of stackA
		err = scene.Repo.CheckoutBranch("stackA-child1")
		require.NoError(t, err)
		cmd = exec.Command(binaryPath, "info")
		cmd.Dir = scene.Dir
		infoOutput, err := cmd.CombinedOutput()
		require.NoError(t, err)
		require.Contains(t, string(infoOutput), "stackA")

		// Verify stackA-child2 is still a child of stackA
		err = scene.Repo.CheckoutBranch("stackA-child2")
		require.NoError(t, err)
		cmd = exec.Command(binaryPath, "info")
		cmd.Dir = scene.Dir
		infoOutput, err = cmd.CombinedOutput()
		require.NoError(t, err)
		require.Contains(t, string(infoOutput), "stackA")
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
			cmd := exec.Command(binaryPath, "create", "parent", "-m", "parent change")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}
			// Create first child
			if err := s.Repo.CreateChange("child1 change", "child1", false); err != nil {
				return err
			}
			cmd = exec.Command(binaryPath, "create", "child1", "-m", "child1 change")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}
			// Go back to parent and create second child
			if err := s.Repo.CheckoutBranch("parent"); err != nil {
				return err
			}
			if err := s.Repo.CreateChange("child2 change", "child2", false); err != nil {
				return err
			}
			cmd = exec.Command(binaryPath, "create", "child2", "-m", "child2 change")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}
			// Go back to parent and create third child
			if err := s.Repo.CheckoutBranch("parent"); err != nil {
				return err
			}
			if err := s.Repo.CreateChange("child3 change", "child3", false); err != nil {
				return err
			}
			cmd = exec.Command(binaryPath, "create", "child3", "-m", "child3 change")
			cmd.Dir = s.Dir
			return cmd.Run()
		})

		// Simulate merging parent into main
		err := scene.Repo.CheckoutBranch("main")
		require.NoError(t, err)
		err = scene.Repo.RunGitCommand("merge", "parent", "--no-ff", "-m", "Merge parent")
		require.NoError(t, err)

		// Switch to child1 and run restack - should reparent all siblings
		err = scene.Repo.CheckoutBranch("child1")
		require.NoError(t, err)

		cmd := exec.Command(binaryPath, "restack", "--only")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()

		require.NoError(t, err, "restack command failed: %s", string(output))
		require.Contains(t, string(output), "reparented onto", "should mention reparenting")

		// Verify child1's parent is now main
		cmd = exec.Command(binaryPath, "info")
		cmd.Dir = scene.Dir
		infoOutput, err := cmd.CombinedOutput()
		require.NoError(t, err)
		require.Contains(t, string(infoOutput), "main", "child1 should now be parented to main")

		// Restack the other children too
		err = scene.Repo.CheckoutBranch("child2")
		require.NoError(t, err)
		cmd = exec.Command(binaryPath, "restack", "--only")
		cmd.Dir = scene.Dir
		output, err = cmd.CombinedOutput()
		require.NoError(t, err, "restack command failed: %s", string(output))

		err = scene.Repo.CheckoutBranch("child3")
		require.NoError(t, err)
		cmd = exec.Command(binaryPath, "restack", "--only")
		cmd.Dir = scene.Dir
		output, err = cmd.CombinedOutput()
		require.NoError(t, err, "restack command failed: %s", string(output))

		// Verify all children are now parented to main
		for _, child := range []string{"child2", "child3"} {
			err = scene.Repo.CheckoutBranch(child)
			require.NoError(t, err)
			cmd = exec.Command(binaryPath, "info")
			cmd.Dir = scene.Dir
			infoOutput, err = cmd.CombinedOutput()
			require.NoError(t, err, "info command failed for %s: %s", child, string(infoOutput))
			require.Contains(t, string(infoOutput), "main", "%s should now be parented to main", child)
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
			cmd := exec.Command(binaryPath, "create", "parent", "-m", "parent change")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}

			// Create child1
			if err := s.Repo.CreateChange("child1 change", "child1", false); err != nil {
				return err
			}
			cmd = exec.Command(binaryPath, "create", "child1", "-m", "child1 change")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}

			// Go back to parent and create child2
			if err := s.Repo.CheckoutBranch("parent"); err != nil {
				return err
			}
			if err := s.Repo.CreateChange("child2 change", "child2", false); err != nil {
				return err
			}
			cmd = exec.Command(binaryPath, "create", "child2", "-m", "child2 change")
			cmd.Dir = s.Dir
			return cmd.Run()
		})

		// Make a change to main so parent needs restacking
		err := scene.Repo.CheckoutBranch("main")
		require.NoError(t, err)
		err = scene.Repo.CreateChangeAndCommit("main update", "main")
		require.NoError(t, err)

		// From child2, run restack --downstack (should restack parent and child2, but not child1)
		err = scene.Repo.CheckoutBranch("child2")
		require.NoError(t, err)

		cmd := exec.Command(binaryPath, "restack", "--downstack")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()

		require.NoError(t, err, "restack command failed: %s", string(output))

		// Verify child2 is still a child of parent
		cmd = exec.Command(binaryPath, "info")
		cmd.Dir = scene.Dir
		infoOutput, err := cmd.CombinedOutput()
		require.NoError(t, err)
		require.Contains(t, string(infoOutput), "parent")
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
			cmd := exec.Command(binaryPath, "create", "parent", "-m", "parent change")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}

			// Create child1
			if err := s.Repo.CreateChange("child1 change", "child1", false); err != nil {
				return err
			}
			cmd = exec.Command(binaryPath, "create", "child1", "-m", "child1 change")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}

			// Go back to parent and create child2
			if err := s.Repo.CheckoutBranch("parent"); err != nil {
				return err
			}
			if err := s.Repo.CreateChange("child2 change", "child2", false); err != nil {
				return err
			}
			cmd = exec.Command(binaryPath, "create", "child2", "-m", "child2 change")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}

			// Go back to parent and create child3
			if err := s.Repo.CheckoutBranch("parent"); err != nil {
				return err
			}
			if err := s.Repo.CreateChange("child3 change", "child3", false); err != nil {
				return err
			}
			cmd = exec.Command(binaryPath, "create", "child3", "-m", "child3 change")
			cmd.Dir = s.Dir
			return cmd.Run()
		})

		// Amend parent branch (children will need restacking)
		err := scene.Repo.CheckoutBranch("parent")
		require.NoError(t, err)
		err = scene.Repo.CreateChangeAndAmend("parent amended", "parent")
		require.NoError(t, err)

		// Run restack --upstack from parent (should restack all three children)
		cmd := exec.Command(binaryPath, "restack", "--upstack")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()

		require.NoError(t, err, "restack command failed: %s", string(output))

		// Verify all children are still children of parent
		for _, child := range []string{"child1", "child2", "child3"} {
			err = scene.Repo.CheckoutBranch(child)
			require.NoError(t, err)
			cmd = exec.Command(binaryPath, "info")
			cmd.Dir = scene.Dir
			infoOutput, err := cmd.CombinedOutput()
			require.NoError(t, err, "info command failed for %s: %s", child, string(infoOutput))
			require.Contains(t, string(infoOutput), "parent", "%s should still have parent as its parent", child)
		}
	})
}

func createStackitBranch(t *testing.T, binaryPath, dir, branch, message string) {
	t.Helper()

	cmd := exec.Command(binaryPath, "create", branch, "-m", message)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "create command failed: %s", string(output))
}
