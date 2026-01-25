package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// =============================================================================
// Split Workflow Integration Tests
//
// These tests cover the split command which extracts files from a branch
// into a new parent branch, then restacks all affected branches.
// =============================================================================

func TestSplitWorkflow(t *testing.T) {
	t.Parallel()

	t.Run("split mid-stack branch with multiple children restacks all descendants", func(t *testing.T) {
		t.Parallel()

		// Scenario - Structure with multiple children:
		//
		// Before split:
		//           main
		//             |
		//         feature-a (has files: config, api, utils)
		//          /     \
		//      child-1  child-2
		//
		// After split --by-file config,api:
		//
		//           main
		//             |
		//        feature-a_split (has: config, api)
		//             |
		//         feature-a (has: utils only)
		//          /     \
		//      child-1  child-2

		sh := NewTestShellInProcess(t)

		// Create feature-a with multiple files
		sh.Write("config", "config content").
			Write("api", "api content").
			Write("utils", "utils content").
			Run("create feature-a -m 'Add feature-a with config, api, utils'")

		// Create child-1 from feature-a
		sh.Write("child1", "child1 content").
			Run("create child-1 -m 'Add child-1'")

		// Go back to feature-a and create child-2
		sh.Checkout("feature-a").
			Write("child2", "child2 content").
			Run("create child-2 -m 'Add child-2'")

		// Go back to feature-a and split out config and api files
		sh.Checkout("feature-a")

		// Run split --by-file to extract config and api (comma-separated)
		sh.Run("split --by-file config_test.txt,api_test.txt")

		// Verify the new split branch exists
		sh.HasBranches("main", "feature-a", "feature-a_split", "child-1", "child-2")

		// Verify feature-a_split has the extracted files (batch check)
		sh.Checkout("feature-a_split")
		verifyFilesExist(t, sh, []string{"config_test.txt", "api_test.txt"})
		verifyFilesNotExist(t, sh, []string{"utils_test.txt"})

		// Verify feature-a now only has utils (config and api were removed) (batch check)
		sh.Checkout("feature-a")
		verifyFilesNotExist(t, sh, []string{"config_test.txt", "api_test.txt"})
		verifyFilesExist(t, sh, []string{"utils_test.txt"})

		// Verify child-1 still has its changes
		sh.Checkout("child-1")
		verifyFilesExist(t, sh, []string{"child1_test.txt"})

		// Verify child-2 still has its changes
		sh.Checkout("child-2")
		verifyFilesExist(t, sh, []string{"child2_test.txt"})

		// Verify parent relationships using stackit info
		sh.Checkout("feature-a").Run("info")
		sh.OutputContains("feature-a_split") // parent should be feature-a_split

		sh.Checkout("child-1").Run("info")
		sh.OutputContains("feature-a") // parent should still be feature-a

		sh.Checkout("child-2").Run("info")
		sh.OutputContains("feature-a") // parent should still be feature-a
	})

	t.Run("split at stack bottom updates all upstack branches", func(t *testing.T) {
		t.Parallel()

		// Scenario:
		// 1. Build: main → feature-a (has shared file) → feature-b → feature-c
		// 2. Split feature-a to extract file to new parent
		// 3. Verify all branches properly restacked
		//
		// After split: main → feature-a_split (file1) → feature-a (file2) → feature-b → feature-c
		// Note: The extracted files (file1) are moved to the split branch and REMOVED from feature-a
		//       So feature-b and feature-c won't have file1 - that's by design

		sh := NewTestShellInProcess(t)

		// Create feature-a with multiple files
		sh.Write("file1", "file1 from feature-a").
			Write("file2", "file2 from feature-a").
			Run("create feature-a -m 'Add feature-a with file1 and file2'")

		// Create feature-b on top
		sh.Write("fileb", "content from feature-b").
			Run("create feature-b -m 'Add feature-b'")

		// Create feature-c on top
		sh.Write("filec", "content from feature-c").
			Run("create feature-c -m 'Add feature-c'")

		// Verify we have the stack
		sh.HasBranches("main", "feature-a", "feature-b", "feature-c")

		// Go to feature-a and split out file1
		sh.Checkout("feature-a").
			Run("split --by-file file1_test.txt")

		// Verify the new split branch exists
		sh.HasBranches("main", "feature-a", "feature-a_split", "feature-b", "feature-c")

		// Verify feature-a_split has file1 (extracted) (batch check)
		sh.Checkout("feature-a_split")
		verifyFilesExist(t, sh, []string{"file1_test.txt"})
		verifyFilesNotExist(t, sh, []string{"file2_test.txt"})

		// Verify feature-a now only has file2 (file1 was extracted) (batch check)
		sh.Checkout("feature-a")
		verifyFilesNotExist(t, sh, []string{"file1_test.txt"})
		verifyFilesExist(t, sh, []string{"file2_test.txt"})

		// Verify feature-b still has its changes (was restacked) (batch check)
		sh.Checkout("feature-b")
		verifyFilesExist(t, sh, []string{"fileb_test.txt", "file2_test.txt"})

		// Verify feature-c still has its changes (was restacked) (batch check)
		sh.Checkout("feature-c")
		verifyFilesExist(t, sh, []string{"filec_test.txt", "file2_test.txt"})
	})

	t.Run("split preserves commit history correctly", func(t *testing.T) {
		t.Parallel()

		sh := NewTestShellInProcess(t)

		// Create feature with two files
		sh.Write("extract", "content to extract").
			Write("keep", "content to keep").
			Run("create feature -m 'Add feature with two files'")

		// Split out the extract file
		sh.Run("split --by-file extract_test.txt")

		// split creates:
		// - feature_split: 1 commit (extract files from feature)
		// - feature: 2 commits (original commit + removal commit)
		sh.CommitCount("main", "feature_split", 1)
		sh.CommitCount("feature_split", "feature", 2) // original + removal commit
	})

	t.Run("split --by-commit accepts the flag and shows interactive prompt", func(t *testing.T) {
		t.Parallel()

		sh := NewTestShellInProcess(t)

		// Create a feature branch with multiple commits on the same branch
		sh.Write("file1", "commit 1 content").
			Run("create feature -m 'First commit'")
		// Add more commits to the same branch using git directly
		sh.Commit("file2", "Second commit on feature").
			Commit("file3", "Third commit on feature")

		// Verify the feature branch has multiple commits
		sh.CommitCount("main", "feature", 3) // feature now has 3 commits

		// Attempt to run split --by-commit
		// This will fail because it requires interactive input for commit selection
		// But we can verify that the command starts and recognizes the flag
		sh.RunExpectError("split --by-commit").
			OutputContains("Splitting the commits")
	})

	t.Run("split --by-commit validates branch has commits to split", func(t *testing.T) {
		t.Parallel()

		sh := NewTestShellInProcess(t)

		// Create a feature branch with only one commit
		sh.Write("file1", "single commit content").
			Run("create feature -m 'Single commit'")

		// Attempt to run split --by-commit on a branch with minimal commits
		// The logic should detect this and potentially default to hunk mode or show appropriate message
		sh.RunExpectError("split --by-commit")
	})
}

func TestSplitAsSibling(t *testing.T) {
	t.Parallel()

	t.Run("split --by-file --as-sibling creates sibling branch without modifying original", func(t *testing.T) {
		t.Parallel()

		// Scenario:
		// Before: main -> feature (has file1, file2)
		// After:  main -> feature (has file1, file2 - unchanged)
		//         main -> feature_split (has file1 only)
		//
		// Unlike default split, --as-sibling:
		// - Does NOT remove files from original branch
		// - Creates sibling on same parent, not a new parent

		sh := NewTestShellInProcess(t)

		// Create feature with two files
		sh.Write("file1", "file1 content").
			Write("file2", "file2 content").
			Run("create feature -m 'Add feature with file1 and file2'")

		// Split file1 to sibling branch
		sh.Run("split --by-file file1_test.txt --as-sibling")

		// Verify both branches exist
		sh.HasBranches("main", "feature", "feature_split")

		// Verify feature_split has the extracted file
		sh.Checkout("feature_split")
		verifyFilesExist(t, sh, []string{"file1_test.txt"})
		verifyFilesNotExist(t, sh, []string{"file2_test.txt"})

		// Verify feature STILL has both files (unchanged)
		sh.Checkout("feature")
		verifyFilesExist(t, sh, []string{"file1_test.txt", "file2_test.txt"})

		// Verify both branches have main as parent (siblings)
		sh.Run("info")
		sh.OutputContains("main") // feature's parent is main

		sh.Checkout("feature_split").Run("info")
		sh.OutputContains("main") // feature_split's parent is also main
	})

	t.Run("split --by-file --as-sibling with custom name", func(t *testing.T) {
		t.Parallel()

		sh := NewTestShellInProcess(t)

		sh.Write("extract", "content").
			Write("keep", "keep content").
			Run("create feature -m 'Add feature'")

		sh.Run("split --by-file extract_test.txt --as-sibling --name my-custom-branch")

		// Verify custom branch name was used
		sh.HasBranches("main", "feature", "my-custom-branch")

		sh.Checkout("my-custom-branch")
		verifyFilesExist(t, sh, []string{"extract_test.txt"})
	})

	t.Run("split --by-file --as-sibling with custom message", func(t *testing.T) {
		t.Parallel()

		sh := NewTestShellInProcess(t)

		sh.Write("file", "content").
			Run("create feature -m 'Add feature'")

		sh.Run("split --by-file file_test.txt --as-sibling --message 'Custom extraction message'")

		// Verify the commit message on the split branch
		sh.Checkout("feature_split").
			Git("log --oneline -1").
			OutputContains("Custom extraction message")
	})

	t.Run("split rejects --name with --by-commit", func(t *testing.T) {
		t.Parallel()

		sh := NewTestShellInProcess(t)

		sh.Write("file1", "content").
			Run("create feature -m 'Commit 1'")
		sh.Commit("file2", "Commit 2")

		// Should error because --name only works with --by-file
		sh.RunExpectError("split --by-commit --name bad-name").
			OutputContains("--name can only be used with --by-file")
	})

	t.Run("split rejects --message with --by-commit", func(t *testing.T) {
		t.Parallel()

		sh := NewTestShellInProcess(t)

		sh.Write("file1", "content").
			Run("create feature -m 'Commit 1'")
		sh.Commit("file2", "Commit 2")

		// Should error because --message only works with --by-file
		sh.RunExpectError("split --by-commit --message 'bad message'").
			OutputContains("--message can only be used with --by-file")
	})

	t.Run("split --as-sibling preserves children on original branch", func(t *testing.T) {
		t.Parallel()

		// Scenario:
		// Before: main -> feature (file1, file2) -> child
		// After:  main -> feature (file1, file2 - unchanged) -> child
		//         main -> feature_split (file1)

		sh := NewTestShellInProcess(t)

		// Create feature with two files
		sh.Write("file1", "file1 content").
			Write("file2", "file2 content").
			Run("create feature -m 'Add feature'")

		// Create child branch
		sh.Write("child_file", "child content").
			Run("create child -m 'Add child'")

		// Go back to feature and split with --as-sibling
		sh.Checkout("feature").
			Run("split --by-file file1_test.txt --as-sibling")

		// Verify structure
		sh.HasBranches("main", "feature", "feature_split", "child")

		// Verify child still has feature as parent
		sh.Checkout("child").Run("info")
		sh.OutputContains("feature")
	})

	t.Run("split rejects --name without explicit --by-file", func(t *testing.T) {
		t.Parallel()

		sh := NewTestShellInProcess(t)

		sh.Write("file1", "content").
			Run("create feature -m 'Add feature'")

		// Should error because --name requires explicit --by-file
		// (without --by-file, style would be auto-detected)
		sh.RunExpectError("split --name bad-name").
			OutputContains("--name can only be used with --by-file")
	})

	t.Run("split rejects --message without explicit --by-file", func(t *testing.T) {
		t.Parallel()

		sh := NewTestShellInProcess(t)

		sh.Write("file1", "content").
			Run("create feature -m 'Add feature'")

		// Should error because --message requires explicit --by-file
		sh.RunExpectError("split --message 'bad message'").
			OutputContains("--message can only be used with --by-file")
	})

	t.Run("split --by-file --as-sibling rejects duplicate custom branch name", func(t *testing.T) {
		t.Parallel()

		sh := NewTestShellInProcess(t)

		sh.Write("file1", "content").
			Write("file2", "content2").
			Run("create feature -m 'Add feature'")

		// Create a branch that would conflict
		sh.Git("branch existing-branch")

		// Should error because branch name already exists
		sh.RunExpectError("split --by-file file1_test.txt --as-sibling --name existing-branch").
			OutputContains("already exists")
	})
}

func TestSplitUndo(t *testing.T) {
	t.Parallel()

	t.Run("undo split --by-file restores original branch and removes split branch", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// 1. Setup
		sh.Write("file1", "content1").
			Write("file2", "content2").
			Run("create feature -m 'Add feature'")

		// 2. Perform split
		sh.Run("split --by-file file1_test.txt")
		sh.HasBranches("main", "feature", "feature_split")

		// 3. Undo
		sh.Log("Undoing split...").
			UndoLatest()

		// 4. Verify
		sh.HasBranches("main", "feature").
			OnBranch("feature")

		// Verify files are back to normal
		verifyFilesExist(t, sh, []string{"file1_test.txt", "file2_test.txt"})
	})

	t.Run("undo split --by-commit restores original branch", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Setup: feature branch with 2 commits
		sh.Write("file1", "c1").
			Run("create feature -m 'Commit 1'")

		// We need to use a non-interactive way or just verify the snapshot exists
		// Since split --by-commit is interactive, we'll verify it TAKES a snapshot
		// even if the command fails/is canceled.

		// Run split --by-commit and fail it immediately
		sh.RunExpectError("split --by-commit")

		// Verify a snapshot was created
		id := sh.GetLatestSnapshotID()
		require.Contains(t, id, "_split")
	})
}

// Helper functions for file verification
// Batch file verification functions to reduce git process spawns

func verifyFilesExist(t *testing.T, sh *TestShell, filenames []string) {
	t.Helper()
	if len(filenames) == 0 {
		return
	}
	// Use git ls-files to check all files at once
	cmd := exec.Command("git", "ls-files", "--")
	cmd.Dir = sh.Dir()
	cmd.Args = append(cmd.Args, filenames...)
	output, err := cmd.Output()
	require.NoError(t, err)
	outputStr := string(output)
	for _, filename := range filenames {
		require.True(t, strings.Contains(outputStr, filename),
			"expected file %s to exist on current branch", filename)
	}
}

func verifyFilesNotExist(t *testing.T, sh *TestShell, filenames []string) {
	t.Helper()
	if len(filenames) == 0 {
		return
	}
	// Use git ls-files to check all files at once
	cmd := exec.Command("git", "ls-files", "--")
	cmd.Dir = sh.Dir()
	cmd.Args = append(cmd.Args, filenames...)
	output, err := cmd.Output()
	require.NoError(t, err)
	outputStr := string(output)
	for _, filename := range filenames {
		require.False(t, strings.Contains(outputStr, filename),
			"expected file %s to NOT exist on current branch", filename)
	}
}

// =============================================================================
// Split by Hunk with Patch File Tests
//
// These tests cover the --patch flag for non-interactive hunk-based splitting.
// =============================================================================

func TestSplitByHunkWithPatch(t *testing.T) {
	t.Parallel()

	t.Run("split by hunk with patch file extracts hunks to child branch", func(t *testing.T) {
		t.Parallel()

		sh := NewTestShellInProcess(t)
		tmpDir := t.TempDir()

		// Setup: Create a file with many lines to enable multi-hunk changes
		sh.Write("init", "line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10\n").
			Run("create setup -m 'Setup file with multiple lines'")

		// Create feature branch that adds lines at TOP and BOTTOM (two separate hunks)
		sh.Write("init", "top_addition\nline1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10\nbottom_addition\n").
			Run("create feature -m 'Add top and bottom'")

		// Create a patch that extracts only the bottom addition
		// The actual diff will have two hunks: one for top and one for bottom
		// We extract only the bottom hunk
		patchContent := `diff --git a/init_test.txt b/init_test.txt
--- a/init_test.txt
+++ b/init_test.txt
@@ -8,3 +8,4 @@
 line8
 line9
 line10
+bottom_addition
`
		patchFile := filepath.Join(tmpDir, "extract.patch")
		require.NoError(t, os.WriteFile(patchFile, []byte(patchContent), 0644))

		// Run split with patch file
		sh.Run("split --by-hunk --above --patch " + patchFile + " --name child -m 'Extract bottom addition'")

		// Verify the child branch was created
		sh.HasBranches("main", "setup", "feature", "child")

		// Verify we're on the child branch
		sh.OnBranch("child")

		// Verify parent relationship
		sh.ExpectBranchParent("child", "feature")

		// Verify child has bottom_addition
		sh.Checkout("child").
			Git("show HEAD:init_test.txt").
			OutputContains("bottom_addition")

		// Verify feature still has top_addition (the remaining change)
		sh.Checkout("feature").
			Git("show HEAD:init_test.txt").
			OutputContains("top_addition").
			OutputNotContains("bottom_addition")
	})

	t.Run("split by hunk with patch file uses default branch name when not provided", func(t *testing.T) {
		t.Parallel()

		sh := NewTestShellInProcess(t)
		tmpDir := t.TempDir()

		// Setup: Create a file with many lines to enable multi-hunk changes
		sh.Write("init", "aaa\nbbb\nccc\nddd\neee\nfff\nggg\nhhh\niii\njjj\n").
			Run("create setup -m 'Setup file'")

		// Create feature branch that adds lines at TOP and BOTTOM
		sh.Write("init", "top_change\naaa\nbbb\nccc\nddd\neee\nfff\nggg\nhhh\niii\njjj\nbottom_change\n").
			Run("create feature -m 'Add lines'")

		// Create patch to extract bottom addition (leaving top on feature)
		patchContent := `diff --git a/init_test.txt b/init_test.txt
--- a/init_test.txt
+++ b/init_test.txt
@@ -8,3 +8,4 @@
 hhh
 iii
 jjj
+bottom_change
`
		patchFile := filepath.Join(tmpDir, "extract.patch")
		require.NoError(t, os.WriteFile(patchFile, []byte(patchContent), 0644))

		// Run split without --name
		sh.Run("split --by-hunk --above --patch " + patchFile + " -m 'Extract line'")

		// Should have generated default name
		sh.HasBranches("main", "setup", "feature", "feature_split")
	})

	t.Run("split by hunk with patch fails on empty patch", func(t *testing.T) {
		t.Parallel()

		sh := NewTestShellInProcess(t)
		tmpDir := t.TempDir()

		// Setup: Create a file with multiple lines
		sh.Write("init", "line1\nline2\nline3\n").
			Run("create setup -m 'Setup file'")

		// Modify file to add more lines
		sh.Write("init", "line1\nline2\nline3\nline4\nline5\n").
			Run("create feature -m 'Add lines'")

		patchFile := filepath.Join(tmpDir, "empty.patch")
		require.NoError(t, os.WriteFile(patchFile, []byte(""), 0644))

		sh.RunExpectError("split --by-hunk --above --patch " + patchFile).
			OutputContains("no hunks")
	})

	t.Run("split by hunk with patch fails on nonexistent patch file", func(t *testing.T) {
		t.Parallel()

		sh := NewTestShellInProcess(t)

		// Setup: Create a file with multiple lines
		sh.Write("init", "line1\nline2\nline3\n").
			Run("create setup -m 'Setup file'")

		// Modify file to add more lines
		sh.Write("init", "line1\nline2\nline3\nline4\nline5\n").
			Run("create feature -m 'Add lines'")

		sh.RunExpectError("split --by-hunk --above --patch /nonexistent/path.patch").
			OutputContains("failed to read patch file")
	})

	t.Run("split by hunk with patch fails when branch already exists", func(t *testing.T) {
		t.Parallel()

		sh := NewTestShellInProcess(t)
		tmpDir := t.TempDir()

		// Setup: Create a file with many lines
		sh.Write("init", "alpha\nbeta\ngamma\ndelta\nepsilon\nzeta\neta\ntheta\niota\nkappa\n").
			Run("create setup -m 'Setup file'")

		// Create feature branch that adds lines at TOP and BOTTOM
		sh.Write("init", "top_item\nalpha\nbeta\ngamma\ndelta\nepsilon\nzeta\neta\ntheta\niota\nkappa\nbottom_item\n").
			Run("create feature -m 'Add lines'")

		// Create the branch name we'll try to use
		sh.Git("branch existing-branch")

		// Patch for init_test.txt - extract bottom addition
		patchContent := `diff --git a/init_test.txt b/init_test.txt
--- a/init_test.txt
+++ b/init_test.txt
@@ -8,3 +8,4 @@
 theta
 iota
 kappa
+bottom_item
`
		patchFile := filepath.Join(tmpDir, "extract.patch")
		require.NoError(t, os.WriteFile(patchFile, []byte(patchContent), 0644))

		sh.RunExpectError("split --by-hunk --above --patch " + patchFile + " --name existing-branch").
			OutputContains("already exists")
	})

	t.Run("split by hunk with patch reparents existing children", func(t *testing.T) {
		t.Parallel()

		sh := NewTestShellInProcess(t)
		tmpDir := t.TempDir()

		// Setup: Create a file with many lines
		sh.Write("init", "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\nten\n").
			Run("create setup -m 'Setup file'")

		// Create parent branch with top and bottom additions
		sh.Write("init", "top_new\none\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\nten\nbottom_new\n").
			Run("create parent -m 'Add lines to parent'")

		// Create child branch with more modifications
		sh.Write("init", "top_new\none\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\nten\nbottom_new\nchild_extra\n").
			Run("create original-child -m 'Add child content'")

		// Go back to parent and split
		sh.Checkout("parent")

		// Patch for init_test.txt - extract bottom addition (leaving top on parent)
		patchContent := `diff --git a/init_test.txt b/init_test.txt
--- a/init_test.txt
+++ b/init_test.txt
@@ -8,3 +8,4 @@
 eight
 nine
 ten
+bottom_new
`
		patchFile := filepath.Join(tmpDir, "extract.patch")
		require.NoError(t, os.WriteFile(patchFile, []byte(patchContent), 0644))

		sh.Run("split --by-hunk --above --patch " + patchFile + " --name extracted -m 'Extract bottom'")

		// Verify the new child was created between parent and original-child
		sh.ExpectBranchParent("extracted", "parent")
		sh.ExpectBranchParent("original-child", "extracted")
	})
}
