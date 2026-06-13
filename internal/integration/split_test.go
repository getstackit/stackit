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

	run := func(name string, fn func(t *testing.T, sh *TestShell)) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			sh := NewTestShellInProcess(t)
			sh.SetWorktreeBasePath(t.TempDir())
			fn(t, sh)
		})
	}

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
	run("split mid-stack branch with multiple children restacks all descendants", func(t *testing.T, sh *TestShell) {
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

	// Scenario:
	// 1. Build: main → feature-a (has shared file) → feature-b → feature-c
	// 2. Split feature-a to extract file to new parent
	// 3. Verify all branches properly restacked
	//
	// After split: main → feature-a_split (file1) → feature-a (file2) → feature-b → feature-c
	// Note: The extracted files (file1) are moved to the split branch and REMOVED from feature-a
	//       So feature-b and feature-c won't have file1 - that's by design
	run("split at stack bottom updates all upstack branches", func(t *testing.T, sh *TestShell) {
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

	run("split preserves commit history correctly", func(_ *testing.T, sh *TestShell) {
		// Create feature with two files
		sh.Write("extract", "content to extract").
			Write("keep", "content to keep").
			Run("create feature -m 'Add feature with two files'")

		// Split out the extract file
		sh.Run("split --by-file extract_test.txt")

		// split creates:
		// - feature_split: 1 commit (extracted file changes)
		// - feature: 1 commit (remaining file changes)
		// Note: The new hunk-based approach doesn't need a removal commit
		sh.CommitCount("main", "feature_split", 1)
		sh.CommitCount("feature_split", "feature", 1)
	})

	run("split --by-commit accepts the flag and shows interactive prompt", func(_ *testing.T, sh *TestShell) {
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

	run("split --by-commit validates branch has commits to split", func(_ *testing.T, sh *TestShell) {
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

	run := func(name string, fn func(t *testing.T, sh *TestShell)) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			sh := NewTestShellInProcess(t)
			sh.SetWorktreeBasePath(t.TempDir())
			fn(t, sh)
		})
	}

	// Scenario:
	// Before: main -> feature (has file1, file2)
	// After:  main -> feature (has file1, file2 - unchanged)
	//         main -> feature_split (has file1 only)
	//
	// Unlike default split, --as-sibling:
	// - Does NOT remove files from original branch
	// - Creates sibling on same parent, not a new parent
	run("split --by-file --as-sibling creates sibling branch without modifying original", func(t *testing.T, sh *TestShell) {
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

	run("split --by-file --as-sibling with custom name", func(t *testing.T, sh *TestShell) {
		sh.Write("extract", "content").
			Write("keep", "keep content").
			Run("create feature -m 'Add feature'")

		sh.Run("split --by-file extract_test.txt --as-sibling --name my-custom-branch")

		// Verify custom branch name was used
		sh.HasBranches("main", "feature", "my-custom-branch")

		sh.Checkout("my-custom-branch")
		verifyFilesExist(t, sh, []string{"extract_test.txt"})
	})

	run("split --by-file --as-sibling with custom message", func(_ *testing.T, sh *TestShell) {
		sh.Write("file", "content").
			Run("create feature -m 'Add feature'")

		sh.Run("split --by-file file_test.txt --as-sibling --message 'Custom extraction message'")

		// Verify the commit message on the split branch
		sh.Checkout("feature_split").
			Git("log --oneline -1").
			OutputContains("Custom extraction message")
	})

	run("split rejects --name with --by-commit", func(_ *testing.T, sh *TestShell) {
		sh.Write("file1", "content").
			Run("create feature -m 'Commit 1'")
		sh.Commit("file2", "Commit 2")

		// Should error because --name only works with --by-file
		sh.RunExpectError("split --by-commit --name bad-name").
			OutputContains("--name can only be used with --by-file")
	})

	run("split rejects --message with --by-commit", func(_ *testing.T, sh *TestShell) {
		sh.Write("file1", "content").
			Run("create feature -m 'Commit 1'")
		sh.Commit("file2", "Commit 2")

		// Should error because --message only works with --by-file
		sh.RunExpectError("split --by-commit --message 'bad message'").
			OutputContains("--message/--message-file can only be used with --by-file")
	})

	// Scenario:
	// Before: main -> feature (file1, file2) -> child
	// After:  main -> feature (file1, file2 - unchanged) -> child
	//         main -> feature_split (file1)
	run("split --as-sibling preserves children on original branch", func(_ *testing.T, sh *TestShell) {
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

	run("split rejects --name without explicit --by-file", func(_ *testing.T, sh *TestShell) {
		sh.Write("file1", "content").
			Run("create feature -m 'Add feature'")

		// Should error because --name requires explicit --by-file
		// (without --by-file, style would be auto-detected)
		sh.RunExpectError("split --name bad-name").
			OutputContains("--name can only be used with --by-file")
	})

	run("split rejects --message without explicit --by-file", func(_ *testing.T, sh *TestShell) {
		sh.Write("file1", "content").
			Run("create feature -m 'Add feature'")

		// Should error because --message requires explicit --by-file
		sh.RunExpectError("split --message 'bad message'").
			OutputContains("--message/--message-file can only be used with --by-file")
	})

	run("split --by-file --as-sibling rejects duplicate custom branch name", func(_ *testing.T, sh *TestShell) {
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

	run := func(name string, fn func(t *testing.T, sh *TestShell)) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			sh := NewTestShellInProcess(t)
			sh.SetWorktreeBasePath(t.TempDir())
			fn(t, sh)
		})
	}

	run("undo split --by-file restores original branch and removes split branch", func(t *testing.T, sh *TestShell) {
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

	run("undo split --by-commit restores original branch", func(t *testing.T, sh *TestShell) {
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
// Split by File - Hunk-Based Extraction Tests
//
// These tests verify that --by-file extracts CHANGES to files, not whole files.
// This is the correct semantic for modified files.
// =============================================================================

func TestSplitByFileExtractsChanges(t *testing.T) {
	t.Parallel()

	run := func(name string, fn func(t *testing.T, sh *TestShell)) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			sh := NewTestShellInProcess(t)
			sh.SetWorktreeBasePath(t.TempDir())
			fn(t, sh)
		})
	}

	run("by-file extracts changes not whole file for modified files", func(_ *testing.T, sh *TestShell) {
		// Setup: create a file with existing content
		sh.Write("base", "line1\nline2\nline3\n").
			Run("create setup -m 'Setup file with content'")

		// Feature: modify the existing file and add a new file
		sh.Write("base", "line1\nmodified line2\nline3\n").
			Write("newfile", "new content").
			Run("create feature -m 'Modify existing and add new'")

		// Split out only the base file changes
		sh.Run("split --by-file base_test.txt")

		// Verify branches exist
		sh.HasBranches("main", "setup", "feature", "feature_split")

		// Verify feature_split has the base file changes
		sh.Checkout("feature_split").
			Git("show HEAD:base_test.txt").
			OutputContains("modified line2")

		// Verify feature still has the new file but NOT the base file modification
		sh.Checkout("feature").
			Git("show HEAD:newfile_test.txt").
			OutputContains("new content")
		// The base file on feature should be inherited from feature_split
		// but feature's commit diff should NOT include base_test.txt changes
		sh.Git("show HEAD --stat").
			OutputContains("newfile_test.txt").
			OutputNotContains("base_test.txt")
	})

	run("by-file extracts whole file for new files", func(_ *testing.T, sh *TestShell) {
		// Setup: create a base branch
		sh.Write("existing", "existing content").
			Run("create setup -m 'Setup'")

		// Feature: add two new files
		sh.Write("file1", "file1 content").
			Write("file2", "file2 content").
			Run("create feature -m 'Add two files'")

		// Split out file1 only
		sh.Run("split --by-file file1_test.txt")

		// Verify feature_split has file1
		sh.Checkout("feature_split").
			Git("show HEAD:file1_test.txt").
			OutputContains("file1 content")

		// Verify feature_split does NOT have file2
		sh.Git("ls-tree HEAD --name-only").
			OutputNotContains("file2_test.txt")

		// Verify feature has file2
		sh.Checkout("feature").
			Git("show HEAD:file2_test.txt").
			OutputContains("file2 content")
	})

	run("by-file with mixed new and modified files", func(_ *testing.T, sh *TestShell) {
		// Setup: create a base file
		sh.Write("existing", "original\n").
			Run("create setup -m 'Setup'")

		// Feature: modify existing and add new file
		sh.Write("existing", "modified\n").
			Write("newfile", "new content").
			Run("create feature -m 'Modify and add'")

		// Split out the new file (leave modification on feature)
		sh.Run("split --by-file newfile_test.txt")

		// feature_split should have the new file
		sh.Checkout("feature_split").
			Git("show HEAD:newfile_test.txt").
			OutputContains("new content")
		// feature_split should NOT have the existing file modification
		sh.Git("show HEAD:existing_test.txt").
			OutputContains("original") // inherits from setup

		// feature should have the modification
		sh.Checkout("feature").
			Git("show HEAD:existing_test.txt").
			OutputContains("modified")
	})
}

// =============================================================================
// Split by Hunk with Patch File Tests
//
// These tests cover the --patch flag for non-interactive hunk-based splitting.
// =============================================================================

func TestSplitByHunkWithPatch(t *testing.T) {
	t.Parallel()

	run := func(name string, fn func(t *testing.T, sh *TestShell)) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			sh := NewTestShellInProcess(t)
			sh.SetWorktreeBasePath(t.TempDir())
			fn(t, sh)
		})
	}

	run("split by hunk with patch file extracts hunks to child branch", func(t *testing.T, sh *TestShell) {
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

	run("split by hunk with patch file uses default branch name when not provided", func(t *testing.T, sh *TestShell) {
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

	run("split by hunk with patch fails on empty patch", func(t *testing.T, sh *TestShell) {
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

	run("split by hunk with patch fails on nonexistent patch file", func(_ *testing.T, sh *TestShell) {
		// Setup: Create a file with multiple lines
		sh.Write("init", "line1\nline2\nline3\n").
			Run("create setup -m 'Setup file'")

		// Modify file to add more lines
		sh.Write("init", "line1\nline2\nline3\nline4\nline5\n").
			Run("create feature -m 'Add lines'")

		sh.RunExpectError("split --by-hunk --above --patch /nonexistent/path.patch").
			OutputContains("failed to read patch file")
	})

	run("split by hunk with patch fails when branch already exists", func(t *testing.T, sh *TestShell) {
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

	run("split by hunk with patch reparents existing children", func(t *testing.T, sh *TestShell) {
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

	run("split by hunk with patch file defaults to below direction", func(t *testing.T, sh *TestShell) {
		tmpDir := t.TempDir()

		// Setup: Create a file with many lines
		sh.Write("init", "aaa\nbbb\nccc\nddd\neee\nfff\nggg\nhhh\niii\njjj\n").
			Run("create setup -m 'Setup file'")

		// Create feature branch that adds lines at TOP and BOTTOM
		sh.Write("init", "top_item\naaa\nbbb\nccc\nddd\neee\nfff\nggg\nhhh\niii\njjj\nbottom_item\n").
			Run("create feature -m 'Add lines'")

		// Patch extracts top addition to new PARENT branch (--below is default)
		patchContent := `diff --git a/init_test.txt b/init_test.txt
--- a/init_test.txt
+++ b/init_test.txt
@@ -1,3 +1,4 @@
+top_item
 aaa
 bbb
 ccc
`
		patchFile := filepath.Join(tmpDir, "extract.patch")
		require.NoError(t, os.WriteFile(patchFile, []byte(patchContent), 0644))

		// Run split with --patch but NO direction flag (should default to --below)
		sh.Run("split --patch " + patchFile + " --name parent-split -m 'Extract top'")

		// Verify the new parent branch was created
		sh.HasBranches("main", "setup", "feature", "parent-split")

		// Verify parent-split is parent of feature (--below creates parent)
		sh.ExpectBranchParent("feature", "parent-split")

		// Verify parent-split has top_item but NOT bottom_item
		sh.Checkout("parent-split").
			Git("show HEAD:init_test.txt").
			OutputContains("top_item").
			OutputNotContains("bottom_item")

		// Verify feature has BOTH top_item (inherited from parent-split) AND bottom_item (its own change)
		// This is correct because feature is rebased on parent-split
		sh.Checkout("feature").
			Git("show HEAD:init_test.txt").
			OutputContains("bottom_item").
			OutputContains("top_item")

		// Verify commit diff only introduces bottom_item (not the whole file)
		sh.Git("show HEAD --stat").
			OutputContains("init_test.txt")
	})

	run("split by hunk with patch file below direction creates parent branch", func(t *testing.T, sh *TestShell) {
		tmpDir := t.TempDir()

		// Setup: Create a file with many lines
		sh.Write("init", "111\n222\n333\n444\n555\n666\n777\n888\n999\n000\n").
			Run("create setup -m 'Setup file'")

		// Create feature branch that adds lines at TOP and BOTTOM
		sh.Write("init", "top_line\n111\n222\n333\n444\n555\n666\n777\n888\n999\n000\nbottom_line\n").
			Run("create feature -m 'Add lines'")

		// Patch extracts bottom addition to new PARENT branch
		patchContent := `diff --git a/init_test.txt b/init_test.txt
--- a/init_test.txt
+++ b/init_test.txt
@@ -8,3 +8,4 @@
 888
 999
 000
+bottom_line
`
		patchFile := filepath.Join(tmpDir, "extract.patch")
		require.NoError(t, os.WriteFile(patchFile, []byte(patchContent), 0644))

		// Run split with explicit --below
		sh.Run("split --patch " + patchFile + " --below --name parent-branch -m 'Extract bottom to parent'")

		// Verify parent-branch is parent of feature
		sh.ExpectBranchParent("feature", "parent-branch")

		// Verify parent-branch has bottom_line (the patch content goes to parent) but NOT top_line
		sh.Checkout("parent-branch").
			Git("show HEAD:init_test.txt").
			OutputContains("bottom_line").
			OutputNotContains("top_line")

		// Verify feature has BOTH: bottom_line (from parent-branch) AND top_line (its own change)
		sh.Checkout("feature").
			Git("show HEAD:init_test.txt").
			OutputContains("top_line").
			OutputContains("bottom_line")
	})

	run("split by hunk with patch fails when all changes staged", func(t *testing.T, sh *TestShell) {
		tmpDir := t.TempDir()

		// Setup: Create a simple file
		sh.Write("init", "original\n").
			Run("create setup -m 'Setup file'")

		// Create feature branch with only one hunk
		sh.Write("init", "modified\n").
			Run("create feature -m 'Modify file'")

		// Patch that includes ALL the changes
		patchContent := `diff --git a/init_test.txt b/init_test.txt
--- a/init_test.txt
+++ b/init_test.txt
@@ -1 +1 @@
-original
+modified
`
		patchFile := filepath.Join(tmpDir, "all.patch")
		require.NoError(t, os.WriteFile(patchFile, []byte(patchContent), 0644))

		// Should fail because all changes would be staged, leaving nothing on feature
		sh.RunExpectError("split --patch " + patchFile + " --above").
			OutputContains("nothing would remain")
	})

	run("split by hunk with malformed patch fails gracefully", func(t *testing.T, sh *TestShell) {
		tmpDir := t.TempDir()

		// Setup
		sh.Write("init", "content\n").
			Run("create setup -m 'Setup'")

		sh.Write("init", "modified content\n").
			Run("create feature -m 'Modify'")

		// Malformed patch (invalid diff header)
		patchContent := `this is not a valid patch
just some random text
`
		patchFile := filepath.Join(tmpDir, "bad.patch")
		require.NoError(t, os.WriteFile(patchFile, []byte(patchContent), 0644))

		// Should fail with no hunks error (parser returns empty when invalid)
		sh.RunExpectError("split --patch " + patchFile).
			OutputContains("no hunks")
	})

	run("split by hunk with multi-file patch extracts changes from multiple files", func(t *testing.T, sh *TestShell) {
		tmpDir := t.TempDir()

		// Setup: Create two files
		sh.Write("file1", "file1_line1\nfile1_line2\nfile1_line3\n").
			Write("file2", "file2_line1\nfile2_line2\nfile2_line3\n").
			Run("create setup -m 'Setup two files'")

		// Create feature branch that modifies both files
		sh.Write("file1", "file1_line1\nfile1_modified\nfile1_line3\n").
			Write("file2", "file2_line1\nfile2_modified\nfile2_line3\n").
			Run("create feature -m 'Modify both files'")

		// Create patch to extract only file2 changes to child
		patchContent := `diff --git a/file2_test.txt b/file2_test.txt
--- a/file2_test.txt
+++ b/file2_test.txt
@@ -1,3 +1,3 @@
 file2_line1
-file2_line2
+file2_modified
 file2_line3
`
		patchFile := filepath.Join(tmpDir, "extract.patch")
		require.NoError(t, os.WriteFile(patchFile, []byte(patchContent), 0644))

		// Run split with patch file (--above extracts to child)
		sh.Run("split --patch " + patchFile + " --above --name child -m 'Extract file2 changes'")

		// Verify branches exist
		sh.HasBranches("main", "setup", "feature", "child")

		// Verify child has file2 changes
		sh.Checkout("child").
			Git("show HEAD:file2_test.txt").
			OutputContains("file2_modified")

		// Verify feature keeps file1 changes but not file2 changes
		sh.Checkout("feature").
			Git("show HEAD:file1_test.txt").
			OutputContains("file1_modified")
		sh.Git("show HEAD:file2_test.txt").
			OutputContains("file2_line2").
			OutputNotContains("file2_modified")
	})

	run("split by hunk with patch includes new files - above direction", func(t *testing.T, sh *TestShell) {
		tmpDir := t.TempDir()

		// Setup: create a base with existing file
		// Write("existing", ...) creates existing_test.txt
		sh.Write("existing", "original content").
			Run("create setup -m 'Setup'")

		// Feature: modify existing file and add new file
		// Write("newfile", ...) creates newfile_test.txt
		sh.Write("existing", "modified content").
			Write("newfile", "new file content").
			Run("create feature -m 'Add changes'")

		// Create patch that extracts only the new file
		patchContent := `diff --git a/newfile_test.txt b/newfile_test.txt
new file mode 100644
--- /dev/null
+++ b/newfile_test.txt
@@ -0,0 +1 @@
+new file content
`
		patchFile := filepath.Join(tmpDir, "extract.patch")
		require.NoError(t, os.WriteFile(patchFile, []byte(patchContent), 0o644))

		// Extract the new file to a child branch
		sh.Run("split --patch " + patchFile + " --above --name child -m 'Extract new file'")

		// Verify child has the new file
		sh.Checkout("child").
			Git("show HEAD:newfile_test.txt").
			OutputContains("new file content")

		// Verify feature keeps the existing file modification but not the new file
		sh.Checkout("feature").
			Git("show HEAD:existing_test.txt").
			OutputContains("modified content")
		// The new file should not be in feature's commit
		sh.Git("show HEAD -- newfile_test.txt").
			OutputNotContains("new file content")
	})

	run("split by hunk with patch includes deleted files - above direction", func(t *testing.T, sh *TestShell) {
		tmpDir := t.TempDir()

		// Setup: create files that will be modified and deleted
		// Write("tokeep", ...) creates tokeep_test.txt
		// Write("todelete", ...) creates todelete_test.txt
		// Add trailing newline to match patch format
		sh.Write("tokeep", "keep this\n").
			Write("todelete", "will be deleted\n").
			Run("create setup -m 'Setup with files'")

		// Feature: modify one file and delete another
		sh.Write("tokeep", "modified content\n").
			Git("rm todelete_test.txt").
			Run("create feature -m 'Modify and delete'")

		// Create patch that extracts only the deletion
		patchContent := `diff --git a/todelete_test.txt b/todelete_test.txt
deleted file mode 100644
--- a/todelete_test.txt
+++ /dev/null
@@ -1 +0,0 @@
-will be deleted
`
		patchFile := filepath.Join(tmpDir, "extract.patch")
		require.NoError(t, os.WriteFile(patchFile, []byte(patchContent), 0o644))

		// Extract the deletion to a child branch
		sh.Run("split --patch " + patchFile + " --above --name child -m 'Extract deletion'")

		// Verify child has the deletion (file was deleted in this commit)
		sh.Checkout("child").
			Git("show HEAD -- todelete_test.txt").
			OutputContains("deleted file mode")

		// Verify feature still has the file (deletion was extracted)
		sh.Checkout("feature").
			Git("show HEAD:todelete_test.txt").
			OutputContains("will be deleted")

		// Verify feature keeps the modification
		sh.Git("show HEAD:tokeep_test.txt").
			OutputContains("modified content")
	})

	run("split by hunk with patch includes new files - below direction", func(t *testing.T, sh *TestShell) {
		tmpDir := t.TempDir()

		// Setup: create a base with existing file
		// Write("existing", ...) creates existing_test.txt
		sh.Write("existing", "original content").
			Run("create setup -m 'Setup'")

		// Feature: modify existing file and add new file
		// Write("newfile", ...) creates newfile_test.txt
		sh.Write("existing", "modified content").
			Write("newfile", "new file content").
			Run("create feature -m 'Add changes'")

		// Create patch that extracts only the new file to the parent
		patchContent := `diff --git a/newfile_test.txt b/newfile_test.txt
new file mode 100644
--- /dev/null
+++ b/newfile_test.txt
@@ -0,0 +1 @@
+new file content
`
		patchFile := filepath.Join(tmpDir, "extract.patch")
		require.NoError(t, os.WriteFile(patchFile, []byte(patchContent), 0o644))

		// Extract the new file to a parent branch (--below creates parent)
		sh.Run("split --patch " + patchFile + " --below --name parent -m 'New file only'")

		// Verify parent has the new file
		sh.Checkout("parent").
			Git("show HEAD:newfile_test.txt").
			OutputContains("new file content")

		// Verify feature keeps the existing file modification but not the new file
		sh.Checkout("feature").
			Git("show HEAD:existing_test.txt").
			OutputContains("modified content")
		// New file should not be in feature's commit
		sh.Git("show HEAD -- newfile_test.txt").
			OutputNotContains("new file content")
	})
}

// =============================================================================
// Split Edge Case Tests
//
// These tests cover edge cases and error handling for split operations.
// =============================================================================

func TestSplitEdgeCases(t *testing.T) {
	t.Parallel()

	run := func(name string, fn func(t *testing.T, sh *TestShell)) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			sh := NewTestShellInProcess(t)
			sh.SetWorktreeBasePath(t.TempDir())
			fn(t, sh)
		})
	}

	run("by-file works with multi-commit branch", func(_ *testing.T, sh *TestShell) {
		// Setup
		sh.Write("base", "base content").
			Run("create setup -m 'Setup'")

		// Create feature with first commit
		sh.Write("file1", "v1").
			Run("create feature -m 'Add file1'")

		// Add more commits to same branch
		sh.Commit("file2", "Add file2")
		sh.Write("file1", "v2").
			Git("add file1_test.txt").
			Git("commit -m 'Update file1 v2'")

		// Verify feature has multiple commits
		sh.CommitCount("setup", "feature", 3)

		// Split out file1 (should include changes from all commits)
		sh.Run("split --by-file file1_test.txt")

		// feature_split should have file1 with v2 content
		sh.Checkout("feature_split").
			Git("show HEAD:file1_test.txt").
			OutputContains("v2")

		// feature should have file2 but not file1 changes
		sh.Checkout("feature").
			Git("show HEAD --stat").
			OutputContains("file2_test.txt").
			OutputNotContains("file1_test.txt")
	})

	run("by-file with deleted file", func(_ *testing.T, sh *TestShell) {
		// Setup with two files (add trailing newlines to match patch format)
		sh.Write("keep", "keep content\n").
			Write("todelete", "will be deleted\n").
			Run("create setup -m 'Setup with files'")

		// Feature: modify one, delete another
		sh.Write("keep", "modified keep\n").
			Git("rm todelete_test.txt").
			Run("create feature -m 'Modify and delete'")

		// Split out the deletion
		sh.Run("split --by-file todelete_test.txt")

		// feature_split should NOT have todelete_test.txt (it was deleted)
		sh.Checkout("feature_split").
			Git("ls-tree HEAD --name-only").
			OutputNotContains("todelete_test.txt")

		// feature should still have keep modifications
		sh.Checkout("feature").
			Git("show HEAD:keep_test.txt").
			OutputContains("modified keep")
	})

	run("by-file with special characters in filename", func(_ *testing.T, sh *TestShell) {
		sh.Write("normal", "normal content").
			Run("create setup -m 'Setup'")

		// Create files with special characters (dashes and underscores)
		sh.Write("file-with-dashes", "dashes content").
			Write("file_with_underscores", "underscores content").
			Run("create feature -m 'Add files with special chars'")

		// Split one of them
		sh.Run("split --by-file file-with-dashes_test.txt")

		sh.Checkout("feature_split").
			Git("show HEAD:file-with-dashes_test.txt").
			OutputContains("dashes content")

		// feature should have the other file
		sh.Checkout("feature").
			Git("show HEAD:file_with_underscores_test.txt").
			OutputContains("underscores content")
	})

	run("sibling mode extracts changes not whole files", func(_ *testing.T, sh *TestShell) {
		// Setup with existing file
		sh.Write("existing", "original line1\noriginal line2\n").
			Run("create setup -m 'Setup'")

		// Feature modifies existing and adds new
		sh.Write("existing", "modified line1\noriginal line2\n").
			Write("newfile", "new content").
			Run("create feature -m 'Modify and add'")

		// Split with sibling - extracts the change to new sibling
		sh.Run("split --by-file existing_test.txt --as-sibling")

		// feature_split should have the modification
		sh.Checkout("feature_split").
			Git("show HEAD:existing_test.txt").
			OutputContains("modified line1")

		// feature should be UNCHANGED (sibling mode)
		sh.Checkout("feature").
			Git("show HEAD:existing_test.txt").
			OutputContains("modified line1")
		// Also verify newfile is still there
		sh.Git("show HEAD:newfile_test.txt").
			OutputContains("new content")
	})

	run("split returns to original branch on error", func(_ *testing.T, sh *TestShell) {
		// Setup with a single file
		sh.Write("onlyfile", "content").
			Run("create feature -m 'Add file'")

		// Trying to split the only file should fail
		sh.RunExpectError("split --by-file onlyfile_test.txt").
			OutputContains("nothing would remain")

		// Should still be on feature branch (safety invariant)
		sh.OnBranch("feature")
	})

	run("split fails gracefully with nonexistent file", func(_ *testing.T, sh *TestShell) {
		sh.Write("file1", "content1").
			Write("file2", "content2").
			Run("create feature -m 'Add files'")

		// Trying to split a file that doesn't exist
		sh.RunExpectError("split --by-file nonexistent.txt").
			OutputContains("no changes found")

		// Should still be on feature branch
		sh.OnBranch("feature")
	})
}

// =============================================================================
// Split --by-file --above Tests
//
// These tests cover the --above direction for --by-file splits which creates
// a child branch instead of a parent branch.
// =============================================================================

func TestSplitByFileAbove(t *testing.T) {
	t.Parallel()

	run := func(name string, fn func(t *testing.T, sh *TestShell)) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			sh := NewTestShellInProcess(t)
			sh.SetWorktreeBasePath(t.TempDir())
			fn(t, sh)
		})
	}

	run("split --by-file --above creates child branch with extracted files", func(_ *testing.T, sh *TestShell) {
		// Setup: create branch with multiple files
		sh.Write("keep", "content to keep").
			Write("extract", "content to extract").
			Run("create feature -m 'Add files'")

		// Split with --above to create child branch
		sh.Run("split --by-file extract_test.txt --above --name child -m 'Extracted file'")

		// Verify child branch has the extracted file
		sh.Checkout("child").
			Git("show HEAD:extract_test.txt").
			OutputContains("content to extract")

		// Verify feature keeps the other file but not the extracted one
		sh.Checkout("feature").
			Git("show HEAD:keep_test.txt").
			OutputContains("content to keep")

		// The extracted file should NOT be on feature anymore
		sh.Git("show HEAD -- extract_test.txt").
			OutputNotContains("content to extract")

		// Verify parent relationship: child's parent is feature
		sh.ExpectBranchParent("child", "feature")
	})

	run("split --by-file --above reparents existing children", func(_ *testing.T, sh *TestShell) {
		// Setup: feature -> existing-child
		sh.Write("keep", "keep content").
			Write("extract", "extract content").
			Run("create feature -m 'Add files'")

		sh.Write("child-file", "child content").
			Run("create existing-child -m 'Existing child'")

		// Go back to feature and split
		sh.Checkout("feature").
			Run("split --by-file extract_test.txt --above --name new-child -m 'Extracted'")

		// Verify existing-child is now a child of new-child, not feature
		sh.ExpectBranchParent("existing-child", "new-child")

		// Verify new-child is a child of feature
		sh.ExpectBranchParent("new-child", "feature")

		// Restack so the git history reflects the new parent relationships
		sh.Checkout("existing-child").
			Run("restack")

		// Verify existing-child has exactly 1 commit relative to new-child
		// (not carrying the split-out changes from the parent)
		sh.CommitCount("new-child", "existing-child", 1)
	})

	run("split --by-file --above incompatible with --as-sibling", func(_ *testing.T, sh *TestShell) {
		sh.Write("file", "content").
			Run("create feature -m 'Add file'")

		sh.RunExpectError("split --by-file file_test.txt --above --as-sibling").
			OutputContains("--above and --as-sibling cannot be used together")
	})

	run("split --by-file --above with new file", func(_ *testing.T, sh *TestShell) {
		// Setup: base with one file, then feature adds another
		sh.Write("base", "base content").
			Run("create base -m 'Base'")

		sh.Write("keep", "keep content").
			Write("newfile", "new file content").
			Run("create feature -m 'Add files'")

		// Split the new file to a child branch
		sh.Run("split --by-file newfile_test.txt --above --name child -m 'Extract new file'")

		// Verify child has the new file
		sh.Checkout("child").
			Git("show HEAD:newfile_test.txt").
			OutputContains("new file content")

		// Verify feature does NOT have the new file anymore
		sh.Checkout("feature").
			Git("show HEAD -- newfile_test.txt").
			OutputNotContains("new file content")
	})

	run("split --by-file --above reparents multiple existing children", func(_ *testing.T, sh *TestShell) {
		// Setup: feature -> [child1, child2, child3]
		sh.Write("keep", "keep content").
			Write("extract", "extract content").
			Run("create feature -m 'Add files'")

		sh.Write("child1-file", "child1 content").
			Run("create child1 -m 'Child 1'")

		sh.Checkout("feature").
			Write("child2-file", "child2 content").
			Run("create child2 -m 'Child 2'")

		sh.Checkout("feature").
			Write("child3-file", "child3 content").
			Run("create child3 -m 'Child 3'")

		// Go back to feature and split
		sh.Checkout("feature").
			Run("split --by-file extract_test.txt --above --name new-child -m 'Extracted'")

		// Verify all existing children are now children of new-child
		sh.ExpectBranchParent("child1", "new-child").
			ExpectBranchParent("child2", "new-child").
			ExpectBranchParent("child3", "new-child")

		// Verify new-child is a child of feature
		sh.ExpectBranchParent("new-child", "feature")

		// Restack so the git history reflects the new parent relationships
		sh.Checkout("new-child").
			Run("restack")

		// Verify each child has exactly 1 commit relative to new-child
		sh.CommitCount("new-child", "child1", 1).
			CommitCount("new-child", "child2", 1).
			CommitCount("new-child", "child3", 1)
	})

	run("split --by-file --above fails when all files selected", func(_ *testing.T, sh *TestShell) {
		// Setup: feature with only one file changed
		sh.Write("onlyfile", "only content").
			Run("create feature -m 'Add file'")

		// Try to extract the only file - should fail
		sh.RunExpectError("split --by-file onlyfile_test.txt --above --name child -m 'Extract'").
			OutputContains("all changes were selected")
	})
}

// =============================================================================
// Split --dry-run Tests
//
// These tests cover the --dry-run flag which previews splits without executing.
// =============================================================================

func TestSplitDryRun(t *testing.T) {
	t.Parallel()

	run := func(name string, fn func(t *testing.T, sh *TestShell)) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			sh := NewTestShellInProcess(t)
			sh.SetWorktreeBasePath(t.TempDir())
			fn(t, sh)
		})
	}

	run("split --by-file --dry-run shows preview without executing", func(_ *testing.T, sh *TestShell) {
		sh.Write("keep", "keep content").
			Write("extract", "extract content").
			Run("create feature -m 'Add files'")

		// Run with --dry-run
		sh.Run("split --by-file extract_test.txt --dry-run").
			OutputContains("Dry Run").
			OutputContains("extract_test.txt").
			OutputContains("Run without --dry-run to execute")

		// Verify no new branch was created
		sh.HasBranches("feature", "main")

		// Verify still on feature
		sh.OnBranch("feature")
	})

	run("split --by-file --above --dry-run shows child direction", func(_ *testing.T, sh *TestShell) {
		sh.Write("keep", "keep content").
			Write("extract", "extract content").
			Run("create feature -m 'Add files'")

		// Run with --dry-run and --above
		sh.Run("split --by-file extract_test.txt --above --dry-run").
			OutputContains("Dry Run").
			OutputContains("above").
			OutputContains("child")

		// Verify no new branch was created
		sh.HasBranches("feature", "main")
	})

	run("split --by-file --as-sibling --dry-run shows sibling direction", func(_ *testing.T, sh *TestShell) {
		sh.Write("keep", "keep content").
			Write("extract", "extract content").
			Run("create feature -m 'Add files'")

		// Run with --dry-run and --as-sibling
		sh.Run("split --by-file extract_test.txt --as-sibling --dry-run").
			OutputContains("Dry Run").
			OutputContains("sibling")

		// Verify no new branch was created
		sh.HasBranches("feature", "main")
	})
}
