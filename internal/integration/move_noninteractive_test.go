package integration

import (
	"testing"
)

func TestMoveNonInteractive(t *testing.T) {
	t.Parallel()

	t.Run("requires onto flag in non-interactive mode", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Create a stack: main → a → b
		sh.CreateLinearStack3()
		sh.Checkout("b")

		// Running move without --onto in non-interactive mode should fail
		sh.RunExpectError("move --no-interactive").
			OutputContains("target branch must be specified")
	})

	t.Run("works with onto flag in non-interactive mode", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Create a stack: main → a → b → c
		sh.CreateLinearStack3()
		sh.Checkout("b")

		// Moving b onto main with --onto flag should work
		sh.Run("move --onto main --no-interactive --yes").
			OutputContains("Moved b").
			OutputContains("from a to main")

		// Verify the move happened and b only has its own commit
		sh.ExpectBranchParent("b", "main").
			CommitCount("main", "b", 1)
	})

	t.Run("dry-run works with onto flag", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Create a stack: main → a → b → c
		sh.CreateLinearStack3()
		sh.Checkout("b")

		// Dry-run should show what would happen
		sh.Run("move --onto main --dry-run").
			OutputContains("Dry-run").
			OutputContains("Move: b").
			OutputContains("From: a").
			OutputContains("To:   main")

		// Branch should not have moved
		sh.ExpectBranchParent("b", "a")
	})
}
