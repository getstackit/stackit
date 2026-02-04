package integration

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStackMetadataGC(t *testing.T) {
	t.Parallel()

	t.Run("orphaned stack ref is cleaned after all branches deleted", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Create a stack: main -> a -> b
		sh.CreateLinearStack("a", "b")

		// Trigger stack ID creation by setting a description (stack IDs are created lazily)
		sh.Run("describe -m 'Test stack for GC'")

		// Capture the stack ID
		stackID := sh.GetStackID("a")
		require.NotEmpty(t, stackID, "stack should have a stack ID")

		// Verify stack ref exists
		sh.ExpectStackMetaRefExists(stackID)

		// Delete the entire stack (a and all its children)
		sh.Checkout("main")
		sh.Run("delete a --force --upstack")

		// Run sync to trigger GC
		sh.Run("sync")

		// Verify stack ref is gone
		sh.ExpectStackMetaRefNotExists(stackID)
	})

	t.Run("active stack refs are NOT deleted", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Create a stack: main -> a -> b -> c
		sh.CreateLinearStack3()

		// Trigger stack ID creation by setting a description (stack IDs are created lazily)
		sh.Run("describe -m 'Test stack for GC'")

		// Capture the stack ID
		stackID := sh.GetStackID("a")
		require.NotEmpty(t, stackID, "stack should have a stack ID")

		// Verify stack ref exists before sync
		sh.ExpectStackMetaRefExists(stackID)

		// Run sync (nothing should be deleted)
		sh.Run("sync")

		// Verify stack ref still exists
		sh.ExpectStackMetaRefExists(stackID)
		sh.ExpectStackIDsMatch("a", "b", "c")
	})

	t.Run("stack ref survives when some branches remain after partial deletion", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Create a stack: main -> a -> b -> c
		sh.CreateLinearStack3()

		// Trigger stack ID creation by setting a description (stack IDs are created lazily)
		sh.Run("describe -m 'Test stack for GC'")

		// Capture the stack ID
		stackID := sh.GetStackID("a")
		require.NotEmpty(t, stackID, "stack should have a stack ID")
		sh.ExpectStackIDsMatch("a", "b", "c")

		// Verify stack ref exists
		sh.ExpectStackMetaRefExists(stackID)

		// Delete only the middle branch
		sh.Checkout("a")
		sh.Run("delete b --force")

		// Run sync
		sh.Run("sync")

		// c should be reparented to a
		sh.ExpectBranchParent("c", "a")

		// Stack ref should STILL exist because a and c still have this stack ID
		sh.ExpectStackMetaRefExists(stackID)
		sh.ExpectStackIDsMatch("a", "c")
	})

	t.Run("multiple orphaned stacks are cleaned in one sync", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Create first stack: main -> a
		sh.Write("a.txt", "content for a").
			Run("create a -m 'Add a'")

		// Trigger stack ID creation for first stack
		sh.Run("describe -m 'First stack'")
		stackID1 := sh.GetStackID("a")
		require.NotEmpty(t, stackID1)

		// Create second stack: main -> x
		sh.Checkout("main").
			Write("x.txt", "content for x").
			Run("create x -m 'Add x'")

		// Trigger stack ID creation for second stack
		sh.Run("describe -m 'Second stack'")
		stackID2 := sh.GetStackID("x")
		require.NotEmpty(t, stackID2)

		// Verify both stack refs exist
		sh.ExpectStackMetaRefExists(stackID1)
		sh.ExpectStackMetaRefExists(stackID2)

		// Delete both stacks
		sh.Checkout("main")
		sh.Run("delete a --force")
		sh.Run("delete x --force")

		// Run sync to trigger GC
		sh.Run("sync")

		// Verify both stack refs are gone
		sh.ExpectStackMetaRefNotExists(stackID1)
		sh.ExpectStackMetaRefNotExists(stackID2)
	})
}

// ExpectStackMetaRefExists asserts that a stack metadata ref exists.
func (s *TestShell) ExpectStackMetaRefExists(stackID string) *TestShell {
	s.t.Helper()
	refName := "refs/stackit/stacks/" + stackID
	cmd := exec.Command("git", "show-ref", "-s", refName)
	cmd.Dir = s.scene.Dir
	_, err := cmd.Output()
	require.NoError(s.t, err, "expected stack ref %s to exist, but it doesn't", stackID)
	return s
}

// ExpectStackMetaRefNotExists asserts that a stack metadata ref does not exist.
func (s *TestShell) ExpectStackMetaRefNotExists(stackID string) *TestShell {
	s.t.Helper()
	refName := "refs/stackit/stacks/" + stackID
	cmd := exec.Command("git", "show-ref", "-s", refName)
	cmd.Dir = s.scene.Dir
	output, err := cmd.CombinedOutput()
	require.Error(s.t, err, "expected stack ref %s to NOT exist, but got: %s", stackID, strings.TrimSpace(string(output)))
	return s
}

// SimulateBranchMerged marks a branch as merged in its metadata.
// This simulates what happens when a PR is merged on GitHub.
func (s *TestShell) SimulateBranchMerged(branch string) *TestShell {
	s.t.Helper()

	// Read existing metadata
	refName := "refs/stackit/metadata/" + branch
	cmd := exec.Command("git", "show-ref", "-s", refName)
	cmd.Dir = s.scene.Dir
	shaOutput, err := cmd.Output()
	require.NoError(s.t, err, "failed to get metadata ref for %s", branch)

	sha := strings.TrimSpace(string(shaOutput))
	cmd = exec.Command("git", "cat-file", "-p", sha)
	cmd.Dir = s.scene.Dir
	blobOutput, err := cmd.Output()
	require.NoError(s.t, err, "failed to read metadata blob for %s", branch)

	// Parse and modify metadata
	var meta map[string]any
	err = json.Unmarshal(blobOutput, &meta)
	require.NoError(s.t, err, "failed to parse metadata for %s", branch)

	// Set PR info to merged state
	prInfo := map[string]any{
		"number": 1,
		"state":  "MERGED",
		"base":   "main",
	}
	meta["prInfo"] = prInfo

	// Write updated metadata
	updatedMeta, err := json.Marshal(meta)
	require.NoError(s.t, err, "failed to marshal updated metadata")

	cmd = exec.Command("git", "hash-object", "-w", "--stdin")
	cmd.Dir = s.scene.Dir
	cmd.Stdin = strings.NewReader(string(updatedMeta))
	newShaOutput, err := cmd.Output()
	require.NoError(s.t, err, "failed to create metadata blob")

	newSha := strings.TrimSpace(string(newShaOutput))
	cmd = exec.Command("git", "update-ref", refName, newSha)
	cmd.Dir = s.scene.Dir
	err = cmd.Run()
	require.NoError(s.t, err, "failed to update metadata ref")

	return s
}

// ExpectBranchNotExists asserts that a branch does not exist.
func (s *TestShell) ExpectBranchNotExists(branch string) *TestShell {
	s.t.Helper()
	cmd := exec.Command("git", "rev-parse", "--verify", "refs/heads/"+branch)
	cmd.Dir = s.scene.Dir
	err := cmd.Run()
	require.Error(s.t, err, "expected branch %s to NOT exist, but it does", branch)
	return s
}
