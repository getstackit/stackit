package branch_test

import (
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/testhelpers"
)

func TestAbsorbCommand(t *testing.T) {
	t.Parallel()
	binaryPath := testhelpers.GetSharedBinaryPath()
	if binaryPath == "" {
		if err := testhelpers.GetBinaryError(); err != nil {
			t.Fatalf("failed to build stackit binary: %v", err)
		}
		t.Fatal("stackit binary not built")
	}

	t.Run("absorb with --dry-run", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			// Create initial commit
			if err := testhelpers.InitialCommitSceneSetup(s); err != nil {
				return err
			}
			// Initialize stackit
			cmd := exec.Command(binaryPath, "init")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}
			// Create branch with a commit
			if err := s.Repo.CreateChange("feature change 1", "test1", false); err != nil {
				return err
			}
			cmd = exec.Command(binaryPath, "create", "feature", "-m", "feature change 1")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}
			// Create staged change
			if err := s.Repo.CreateChange("fix for feature change 1", "test1", false); err != nil {
				return err
			}
			return nil
		})

		// Run absorb with --dry-run
		cmd := exec.Command(binaryPath, "absorb", "--dry-run")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()

		require.NoError(t, err, "absorb dry-run failed: %s", string(output))
		require.Contains(t, string(output), "Would absorb", "should mention would absorb")
		require.Contains(t, string(output), "feature", "should mention the branch")

		// Verify staged changes are still there (dry-run doesn't modify)
		cmd = exec.Command("git", "diff", "--cached", "--name-only")
		cmd.Dir = scene.Dir
		staged := testhelpers.Must(cmd.CombinedOutput())
		require.Contains(t, string(staged), "test1_test.txt", "should still have staged changes after dry-run")
	})

	t.Run("absorb with --all flag", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			// Create initial commit
			if err := testhelpers.InitialCommitSceneSetup(s); err != nil {
				return err
			}
			// Initialize stackit
			cmd := exec.Command(binaryPath, "init")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}
			// Create branch with a commit
			if err := s.Repo.CreateChange("feature change 1", "test1", false); err != nil {
				return err
			}
			cmd = exec.Command(binaryPath, "create", "feature", "-m", "feature change 1")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}
			// Create unstaged change
			if err := s.Repo.CreateChange("fix for feature change 1", "test1", true); err != nil {
				return err
			}
			return nil
		})

		// Verify we have unstaged changes
		cmd := exec.Command("git", "diff", "--name-only")
		cmd.Dir = scene.Dir
		output := testhelpers.Must(cmd.CombinedOutput())
		require.Contains(t, string(output), "test1_test.txt")

		// Run absorb with --all and --force
		cmd = exec.Command(binaryPath, "absorb", "--all", "--force")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()

		require.NoError(t, err, "absorb with --all failed: %s", string(output))
		require.Contains(t, string(output), "Absorbed changes", "should mention absorbing")
	})

	t.Run("absorb - no staged changes", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			// Create initial commit
			if err := testhelpers.InitialCommitSceneSetup(s); err != nil {
				return err
			}
			// Initialize stackit
			cmd := exec.Command(binaryPath, "init")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}
			// Create a branch so we're not on trunk
			if err := s.Repo.CreateChange("feature change 1", "test1", false); err != nil {
				return err
			}
			cmd = exec.Command(binaryPath, "create", "feature", "-m", "feature change 1")
			cmd.Dir = s.Dir
			return cmd.Run()
		})

		// Run absorb without any staged changes
		cmd := exec.Command(binaryPath, "absorb", "--force")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()

		require.NoError(t, err, "absorb should not fail with no staged changes")
		require.Contains(t, string(output), "Nothing to absorb", "should mention nothing to absorb")
	})

	t.Run("absorb - only unabsorbable changes", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			if err := testhelpers.InitialCommitSceneSetup(s); err != nil {
				return err
			}
			cmd := exec.Command(binaryPath, "init")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}
			// Create a branch with a commit
			if err := s.Repo.CreateChange("commit 1", "file1", false); err != nil {
				return err
			}
			cmd = exec.Command(binaryPath, "create", "branch1", "-m", "commit 1")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}
			// Create a staged change that doesn't belong to any commit (new file)
			if err := s.Repo.CreateChange("unrelated change", "file2", false); err != nil {
				return err
			}
			return nil
		})

		cmd := exec.Command(binaryPath, "absorb", "--force")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()

		require.NoError(t, err, "Should not error if there are changes but none can be absorbed")
		require.Contains(t, string(output), "The following hunks could not be absorbed")
		require.Contains(t, string(output), "file2")
	})

	t.Run("absorb error - not initialized", func(t *testing.T) {
		t.Parallel()
		// Create a temporary directory without initializing stackit
		tmpDir := t.TempDir()

		// Initialize git repo
		cmd := exec.Command("git", "init", "-b", "main", tmpDir)
		require.NoError(t, cmd.Run())

		// Configure git user
		cmd = exec.Command("git", "-C", tmpDir, "config", "user.name", "Test User")
		require.NoError(t, cmd.Run())
		cmd = exec.Command("git", "-C", tmpDir, "config", "user.email", "test@example.com")
		require.NoError(t, cmd.Run())

		// Create initial commit
		cmd = exec.Command("git", "-C", tmpDir, "commit", "--allow-empty", "-m", "initial")
		require.NoError(t, cmd.Run())

		// Create staged change
		testFile := tmpDir + "/test.txt"
		require.NoError(t, os.WriteFile(testFile, []byte("test"), 0644))
		cmd = exec.Command("git", "-C", tmpDir, "add", testFile)
		require.NoError(t, cmd.Run())

		// Run absorb without initializing stackit
		cmd = exec.Command(binaryPath, "absorb", "--force")
		cmd.Dir = tmpDir
		output, err := cmd.CombinedOutput()

		require.Error(t, err, "absorb should fail when stackit not initialized")
		require.Contains(t, string(output), "not initialized", "should mention not initialized")
	})

	t.Run("absorb error - detached HEAD", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			// Create initial commit
			if err := testhelpers.InitialCommitSceneSetup(s); err != nil {
				return err
			}
			// Initialize stackit
			cmd := exec.Command(binaryPath, "init")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}
			// Create a second commit so we have something to detach to
			if err := s.Repo.CreateChangeAndCommit("second commit", "second"); err != nil {
				return err
			}
			// Detach HEAD by checking out a specific commit
			cmd = exec.Command("git", "checkout", "HEAD~1")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}
			// Create staged change
			if err := s.Repo.CreateChange("detached change", "detached", false); err != nil {
				return err
			}
			return nil
		})

		// Run absorb in detached HEAD state
		cmd := exec.Command(binaryPath, "absorb", "--force")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()

		require.Error(t, err, "absorb should fail in detached HEAD state")
		require.Contains(t, string(output), "not on a branch", "should mention not on a branch")
	})

	t.Run("absorb error - rebase in progress", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			// Create initial commit
			if err := testhelpers.InitialCommitSceneSetup(s); err != nil {
				return err
			}
			// Initialize stackit
			cmd := exec.Command(binaryPath, "init")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}
			// Create branch with a commit
			if err := s.Repo.CreateChange("feature change 1", "test1", false); err != nil {
				return err
			}
			cmd = exec.Command(binaryPath, "create", "feature", "-m", "feature change 1")
			cmd.Dir = s.Dir
			if err := cmd.Run(); err != nil {
				return err
			}
			return nil
		})

		// Start an interactive rebase that will pause (using exec to create the pause)
		// We'll create a rebase-merge directory to simulate rebase in progress
		rebaseMergeDir := scene.Dir + "/.git/rebase-merge"
		require.NoError(t, os.MkdirAll(rebaseMergeDir, 0755))
		// Write a minimal file to make it look like a rebase is in progress
		require.NoError(t, os.WriteFile(rebaseMergeDir+"/head-name", []byte("refs/heads/feature"), 0644))

		// Create staged change
		require.NoError(t, scene.Repo.CreateChange("change during rebase", "test1", false))

		// Run absorb during rebase
		cmd := exec.Command(binaryPath, "absorb", "--force")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()

		require.Error(t, err, "absorb should fail during rebase")
		require.Contains(t, string(output), "rebase", "should mention rebase")
	})
}

func TestAbsorbComplex(t *testing.T) {
	t.Parallel()
	binaryPath := testhelpers.GetSharedBinaryPath()
	if binaryPath == "" {
		if err := testhelpers.GetBinaryError(); err != nil {
			t.Fatalf("failed to build stackit binary: %v", err)
		}
		t.Fatal("stackit binary not built")
	}

	t.Run("absorb failure during restack preserves work and reports error", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
			// Create branch A
			if err := testhelpers.InitialCommitSceneSetup(s); err != nil {
				return err
			}
			cmd := exec.Command(binaryPath, "init")
			cmd.Dir = s.Dir
			require.NoError(t, cmd.Run())

			err := os.WriteFile(s.Dir+"/conflict.txt", []byte("line 1\nline 2\nline 3\n"), 0644)
			require.NoError(t, err)
			cmd = exec.Command("git", "add", "conflict.txt")
			cmd.Dir = s.Dir
			require.NoError(t, cmd.Run())
			cmd = exec.Command(binaryPath, "create", "branchA", "-m", "add conflict.txt", "--all")
			cmd.Dir = s.Dir
			require.NoError(t, cmd.Run())

			// Branch B modifies line 2
			err = os.WriteFile(s.Dir+"/conflict.txt", []byte("line 1\nline 2 B\nline 3\n"), 0644)
			require.NoError(t, err)
			cmd = exec.Command(binaryPath, "create", "branchB", "-m", "modify line 2", "--all")
			cmd.Dir = s.Dir
			require.NoError(t, cmd.Run())

			// Go back to branchA and absorb a change that modifies line 2, which will cause a conflict when restacking branchB
			cmd = exec.Command("git", "checkout", "branchA")
			cmd.Dir = s.Dir
			require.NoError(t, cmd.Run())

			// Important: change line 1 so it absorbs into branchA, but change line 2 to something else to cause conflict with branchB
			err = os.WriteFile(s.Dir+"/conflict.txt", []byte("line 1 modified\nline 2 modified in A\nline 3\n"), 0644)
			require.NoError(t, err)
			cmd = exec.Command("git", "add", "conflict.txt")
			cmd.Dir = s.Dir
			require.NoError(t, cmd.Run())

			return nil
		})

		// Run absorb. It should successfully absorb into branchA, but then fail during restack of branchB
		cmd := exec.Command(binaryPath, "absorb", "--force")
		cmd.Dir = scene.Dir
		output, err := cmd.CombinedOutput()

		require.Error(t, err, "absorb should fail during restack. Output: %s", string(output))
		require.Contains(t, string(output), "failed to restack", "should report restack failure")

		// In case of restack conflict, stackit stays in rebase mode (detached HEAD)
		rebaseDir := scene.Dir + "/.git/rebase-merge"
		if _, err := os.Stat(rebaseDir); os.IsNotExist(err) {
			rebaseDir = scene.Dir + "/.git/rebase-apply"
		}
		_, err = os.Stat(rebaseDir)
		require.NoError(t, err, "should be in middle of rebase")

		// Clean up for next tests
		cmd = exec.Command("git", "rebase", "--abort")
		cmd.Dir = scene.Dir
		_ = cmd.Run()
	})
}
