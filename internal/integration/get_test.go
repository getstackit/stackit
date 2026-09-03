package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetCommand(t *testing.T) {
	t.Parallel()

	t.Run("basic get from remote", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t, WithRemote())

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
		sh := NewTestShellInProcess(t, WithRemote())

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

	// Both update paths act on the checked-out branch: `git reset --hard` moves
	// HEAD's branch and a merge lands in HEAD's branch. Running get from trunk
	// used to rewrite trunk to the fetched branch, silently destroying it.
	t.Run("get does not move trunk when run from trunk", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t, WithRemote())

		sh.Git("checkout -b feature-a").
			WriteFile("a", "remote content").
			Git("commit -m 'Remote change'").
			Git("push -u origin feature-a")

		sh.Run("track feature-a --parent main")

		// Diverge locally so the update path (not the create path) is taken.
		sh.WriteFile("a", "local content").
			Git("commit --amend --no-edit")

		// Run get from trunk, which is what triggered the bug.
		sh.Git("checkout main")
		sh.Git("rev-parse main")
		trunkBefore := strings.TrimSpace(sh.Output())

		sh.Run("get feature-a --force --no-restack")

		sh.Git("rev-parse main")
		require.Equal(t, trunkBefore, strings.TrimSpace(sh.Output()),
			"get must not move trunk")

		// And the branch it was asked to update actually matches the remote.
		sh.Git("rev-parse feature-a")
		branchSHA := strings.TrimSpace(sh.Output())
		sh.Git("rev-parse origin/feature-a")
		require.Equal(t, strings.TrimSpace(sh.Output()), branchSHA,
			"get --force must reset the target branch to the remote")
	})
}
