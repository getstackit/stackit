package integration

import (
	"testing"
)

func TestLockUnlockCommand(t *testing.T) {
	t.Parallel()

	t.Run("lock and unlock workflow", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Create branches with actual commits
		sh.Run("create feature-a").WriteFile("a", "A").Git("add a").Git("commit -m 'A'")
		sh.Run("create feature-b").WriteFile("b", "B").Git("add b").Git("commit -m 'B'")

		// Lock feature-b (and feature-a)
		sh.Run("lock feature-b").
			Run("info").
			OutputContains("(locked)")

		sh.Checkout("feature-a").
			Run("info").
			OutputContains("(locked)")

		// Attempt to modify locked branch should fail
		sh.WriteFile("a", "modified").
			RunExpectError("modify -n").
			OutputContains("locked")

		// Unlock feature-a (and feature-b)
		sh.Run("unlock feature-a").
			Run("info").
			OutputNotContains("(locked)")

		sh.Checkout("feature-b").
			Run("info").
			OutputNotContains("(locked)")

		// Now modification should work
		sh.WriteFile("b", "modified").
			Run("modify -n")
	})

	t.Run("command-specific lock enforcement", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		sh.Run("create feature-a").WriteFile("a", "A").Git("add a").Git("commit -m 'A'")
		sh.Run("lock feature-a")

		// Test various commands
		sh.RunExpectError("squash -m 'squashed'").OutputContains("locked")

		// For absorb, we need something to absorb into the existing commit
		sh.WriteFile("a", "to absorb")
		sh.RunExpectError("absorb").OutputContains("locked")

		sh.RunExpectError("rename renamed").OutputContains("locked")
	})
}
