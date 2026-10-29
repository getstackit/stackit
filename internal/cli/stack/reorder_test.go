package stack_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/testhelpers"
)

func TestReorderCommand(t *testing.T) {
	t.Parallel()
	// `reorder` reads its editor from the process environment (GIT_EDITOR /
	// EDITOR), which is process-global and cannot vary between parallel
	// in-process runs. Only the editor-driven `reorder` invocations spawn the
	// binary; all setup and verification commands run in-process.
	binaryPath := testhelpers.GetSharedBinaryPath()
	if binaryPath == "" {
		if err := testhelpers.GetBinaryError(); err != nil {
			t.Fatalf("failed to build stackit binary: %v", err)
		}
		t.Fatal("stackit binary not built")
	}

	// runReorder spawns `stackit reorder` with a scripted editor that writes
	// the given order.
	runReorder := func(t *testing.T, dir, order string) ([]byte, error) {
		t.Helper()
		editorScript := createEditorScript(t, order)
		cmd := exec.Command(binaryPath, "reorder")
		cmd.Dir = dir
		// Set both EDITOR and GIT_EDITOR to ensure test editor is used
		cmd.Env = append(os.Environ(), "EDITOR="+editorScript, "GIT_EDITOR="+editorScript)
		return cmd.CombinedOutput()
	}

	// stackSetup initializes stackit and creates a linear stack of the given
	// branches, each with one commit touching its own file.
	stackSetup := func(branches ...string) testhelpers.SceneSetup {
		return func(s *testhelpers.Scene) error {
			// Create initial commit
			if err := testhelpers.InitialCommitSceneSetup(s); err != nil {
				return err
			}
			// Initialize stackit
			if err := runCliCommand(s.Dir, "init"); err != nil {
				return err
			}
			for _, branch := range branches {
				if err := s.Repo.CreateChange(branch+" change", "file-"+branch, false); err != nil {
					return err
				}
				if err := runCliCommand(s.Dir, "create", branch, "-m", branch+" change"); err != nil {
					return err
				}
			}
			return nil
		}
	}

	t.Run("simple reorder: A -> B to B -> A", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, stackSetup("branch1", "branch2"))

		// Verify initial state: branch2 should have branch1 as parent
		output, err := runCliCommandOutput(scene.Dir, "parent")
		require.NoError(t, err, "parent command failed: %s", string(output))
		require.Contains(t, string(output), "branch1", "branch2 should initially have branch1 as parent")

		// Reorder: branch2, branch1 (reversed)
		output, err = runReorder(t, scene.Dir, "branch2\nbranch1\n")
		require.NoError(t, err, "reorder command failed: %s", string(output))
		require.Contains(t, string(output), "Reordered", "should mention reordering")

		// Verify new parent relationship: branch2 should now have main as parent
		output, err = runCliCommandOutput(scene.Dir, "parent")
		require.NoError(t, err, "parent command failed: %s", string(output))
		require.Contains(t, string(output), "main", "branch2 should now have main as parent")

		// Checkout branch1 and verify it has branch2 as parent
		require.NoError(t, scene.Repo.CheckoutBranch("branch1"))
		output, err = runCliCommandOutput(scene.Dir, "parent")
		require.NoError(t, err, "parent command failed: %s", string(output))
		require.Contains(t, string(output), "branch2", "branch1 should now have branch2 as parent")
	})

	t.Run("reorder preserves commit counts per branch", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, stackSetup("branch-a", "branch-b", "branch-c"))

		// Helper to get commit count between two refs
		commitCount := func(t *testing.T, dir, from, to string) int {
			t.Helper()
			cmd := exec.Command("git", "rev-list", "--count", from+".."+to)
			cmd.Dir = dir
			out, err := cmd.CombinedOutput()
			require.NoError(t, err, "rev-list failed: %s", string(out))
			var count int
			_, err = fmt.Sscanf(strings.TrimSpace(string(out)), "%d", &count)
			require.NoError(t, err)
			return count
		}

		// Verify initial commit counts: each branch has exactly 1 commit relative to parent
		require.Equal(t, 1, commitCount(t, scene.Dir, "main", "branch-a"), "branch-a should have 1 commit above main")
		require.Equal(t, 1, commitCount(t, scene.Dir, "branch-a", "branch-b"), "branch-b should have 1 commit above branch-a")
		require.Equal(t, 1, commitCount(t, scene.Dir, "branch-b", "branch-c"), "branch-c should have 1 commit above branch-b")

		// Reorder: branch-b, branch-a, branch-c (swap first two)
		output, err := runReorder(t, scene.Dir, "branch-b\nbranch-a\nbranch-c\n")
		require.NoError(t, err, "reorder command failed: %s", string(output))

		// Run restack to rebase branches onto their new parents
		output, err = runCliCommandOutput(scene.Dir, "restack")
		require.NoError(t, err, "restack command failed: %s", string(output))

		// After reorder + restack: main -> branch-b -> branch-a -> branch-c
		// Each branch should still have exactly 1 commit relative to its new parent
		require.Equal(t, 1, commitCount(t, scene.Dir, "main", "branch-b"),
			"branch-b should have 1 commit above main after reorder")
		require.Equal(t, 1, commitCount(t, scene.Dir, "branch-b", "branch-a"),
			"branch-a should have 1 commit above branch-b after reorder")
		require.Equal(t, 1, commitCount(t, scene.Dir, "branch-a", "branch-c"),
			"branch-c should have 1 commit above branch-a after reorder")
	})

	t.Run("reorder with descendants", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, stackSetup("branch1", "branch2", "branch3"))

		// Verify initial state: branch3 should have branch2 as parent
		output, err := runCliCommandOutput(scene.Dir, "parent")
		require.NoError(t, err, "parent command failed: %s", string(output))
		require.Contains(t, string(output), "branch2", "branch3 should initially have branch2 as parent")

		// Reorder: branch2, branch1, branch3
		// This reorders branch1 and branch2 (reversed), keeping branch3 at the end
		// After reordering: trunk -> branch2 -> branch1 -> branch3
		// branch3's parent becomes branch1 (the branch before it in the new order)
		output, err = runReorder(t, scene.Dir, "branch2\nbranch1\nbranch3\n")
		require.NoError(t, err, "reorder command failed: %s", string(output))

		// Verify branch3 now has branch1 as parent (new order: branch2 -> branch1 -> branch3)
		output, err = runCliCommandOutput(scene.Dir, "parent")
		require.NoError(t, err, "parent command failed: %s", string(output))
		require.Contains(t, string(output), "branch1", "branch3 should now have branch1 as parent after reorder")
	})

	// A rejected edit fails while parsing the editor output, before any
	// mutation, so the unchanged-order case can reuse the same stack.
	t.Run("error when branch is removed, then no changes when order unchanged", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, stackSetup("branch1", "branch2"))

		// Editor removes branch1 - should fail
		output, err := runReorder(t, scene.Dir, "branch2\n")
		require.Error(t, err, "reorder should fail when branch is removed")
		require.Contains(t, string(output), "was removed", "error should mention branch removal")
		require.Contains(t, string(output), "stackit delete", "error should suggest using stackit delete")

		// Editor keeps the original order (branch1, branch2)
		output, err = runReorder(t, scene.Dir, "branch1\nbranch2\n")
		require.NoError(t, err, "reorder command failed: %s", string(output))
		require.Contains(t, string(output), "unchanged", "should mention order unchanged")
	})

	// Neither error opens an editor or mutates anything, so both run
	// in-process against one single-branch stack.
	t.Run("error when less than 2 branches, then when on trunk", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, stackSetup("branch1"))

		// On branch1 with only one branch in the stack - should fail
		output, err := runCliCommandOutput(scene.Dir, "reorder")
		require.Error(t, err, "reorder should fail with less than 2 branches")
		require.Contains(t, string(output), "at least 2 branches", "error should mention need for 2+ branches")

		// On trunk - should fail
		require.NoError(t, scene.Repo.CheckoutBranch("main"))
		output, err = runCliCommandOutput(scene.Dir, "reorder")
		require.Error(t, err, "reorder should fail when on trunk")
		require.Contains(t, string(output), "cannot reorder trunk", "error should mention trunk")
	})
}

// createEditorScript creates a shell script in a temp directory that writes the given content to the file
// passed as the first argument, simulating an editor
func createEditorScript(t *testing.T, content string) string {
	t.Helper()

	// Create temp directory outside the repo to avoid git seeing it as uncommitted changes
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "editor.sh")
	// Use a heredoc to safely write content
	scriptContent := "#!/bin/sh\n"
	scriptContent += "cat > \"$1\" << 'EOF'\n"
	scriptContent += content
	scriptContent += "EOF\n"

	err := os.WriteFile(scriptPath, []byte(scriptContent), 0755)
	require.NoError(t, err)

	// Return absolute path for EDITOR environment variable
	absPath, err := filepath.Abs(scriptPath)
	require.NoError(t, err)
	return absPath
}
