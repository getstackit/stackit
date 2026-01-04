package integration

import (
	"testing"
)

func TestGetCommand(t *testing.T) {
	t.Parallel()

	t.Run("basic get from remote", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellWithRemoteInProcess(t)

		// Create a branch on remote
		sh.Log("Creating feature branch on remote...")
		sh.Git("checkout -b feature-a").
			WriteFile("a", "content a").
			Git("commit -m 'Add a'").
			Git("push -u origin feature-a")

		// Remove it locally
		sh.Log("Removing feature branch locally...")
		sh.Git("checkout main").
			Git("branch -D feature-a")

		// Get it back
		sh.Log("Running stackit get...")
		sh.Run("get feature-a")

		// Verify it's back
		sh.OnBranch("feature-a").
			Run("info").
			OutputContains("feature-a").
			OutputContains("(frozen)")
	})

	t.Run("get with force flag", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellWithRemoteInProcess(t)

		// Create branch on remote and track it
		sh.Log("Creating feature branch on remote...")
		sh.Git("checkout -b feature-a").
			WriteFile("a", "remote content").
			Git("commit -m 'Remote change'").
			Git("push -u origin feature-a")

		sh.Run("track feature-a --parent main")

		// Diverge locally
		sh.Log("Diverging locally...")
		sh.WriteFile("a", "local content").
			Git("commit --amend --no-edit")

		// Get should fail without force (it tries to merge and might conflict)
		sh.Log("Running stackit get (expecting failure)...")
		sh.RunExpectError("get feature-a")

		// Clean up the conflict left by the failed merge
		sh.Git("reset --hard")

		// Get with force should succeed
		sh.Log("Running stackit get --force...")
		sh.Run("get feature-a --force")

		// Verify local matches remote
		sh.Run("info").
			OutputContains("Remote change")
	})
}
