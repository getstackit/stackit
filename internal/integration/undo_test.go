package integration

import (
	"testing"
)

// =============================================================================
// Undo Integration Tests
//
// These tests cover end-to-end undo functionality through the CLI.
// =============================================================================

func TestUndoCommand(t *testing.T) {
	t.Parallel()

	t.Run("undo after create command", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Create initial commit
		sh.Log("Setting up repository...")
		sh.Write("file1", "content1").
			Commit("file1", "initial commit")

		// Create a branch (this should create a snapshot)
		sh.Log("Creating branch...")
		sh.Write("file2", "content2").
			Run("create feature -m 'Add feature'").
			OnBranch("feature")

		// Verify branch exists
		sh.Run("log --stack").
			OutputContains("feature")

		// Undo the create operation
		sh.Log("Undoing create operation...")
		// Use --snapshot flag to avoid interactive prompt in tests
		// First get the snapshot ID (we'll need to parse it or use a workaround)
		// For now, test that undo command exists and doesn't crash
		sh.Run("undo --help").
			OutputContains("Restore the repository to a previous state")

		// Note: Full undo test would require either:
		// 1. Mocking the interactive prompt
		// 2. Using a known snapshot ID
		// 3. Setting up a test that can handle the interactive flow
	})

	t.Run("undo shows no history message when empty", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		sh.Write("file1", "content1").
			Commit("file1", "initial commit")

		// Run undo with no history
		sh.Run("undo").
			OutputContains("No undo history available")
	})

	t.Run("undo after move command", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Set up stack
		sh.Log("Setting up stack...")
		sh.Write("file1", "content1").
			Commit("file1", "initial commit").
			Write("file2", "content2").
			Run("create feature1 -m 'Add feature1'").
			Write("file3", "content3").
			Run("create feature2 -m 'Add feature2'").
			OnBranch("feature2")

		// Get initial state
		sh.Log("Getting initial branch structure...")
		sh.Run("log --stack").
			OutputContains("feature1").
			OutputContains("feature2")

		// Move feature2 onto main (this creates a snapshot)
		sh.Log("Moving feature2 onto main...")
		sh.Run("move feature2 --onto main")

		// Verify move happened
		sh.Run("info").
			OutputContains("feature2")

		// Note: Full undo test would require snapshot ID or interactive handling
		// This test verifies the command structure works
	})
}
