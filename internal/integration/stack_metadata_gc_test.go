package integration

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStackMetadataGC(t *testing.T) {
	t.Parallel()

	t.Run("orphaned stack ref is cleaned after all branches deleted", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t, WithRemote())

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
		sh := NewTestShellInProcess(t, WithRemote())

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
		sh := NewTestShellInProcess(t, WithRemote())

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
		sh := NewTestShellInProcess(t, WithRemote())

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
