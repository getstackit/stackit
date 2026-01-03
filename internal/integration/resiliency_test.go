package integration

import (
	"testing"
)

func TestResiliencyStaleParentSHA(t *testing.T) {
	shell := NewTestShellInProcess(t)

	// 1. Create a stack: main -> branch-a -> branch-b
	shell.Log("Creating stack: main -> branch-a -> branch-b")
	shell.Write("file-a.txt", "content a").Run("create branch-a -m 'Commit A'")
	shell.Write("file-b.txt", "content b").Run("create branch-b -m 'Commit B'")

	// 2. Manually break branch-a's SHA. Keep metadata for branch-b, but it's now stale.
	shell.Log("Amending branch-a outside of stackit")
	shell.Checkout("branch-a")
	shell.Git("commit --amend -m 'Commit A Amended'")

	// 3. Now branch-b's metadata is stale (points to old Commit A SHA).
	// Try to restack branch-b. It should now auto-discover the fork point and succeed.
	shell.Log("Attempting to restack branch-b with stale metadata")
	shell.Checkout("branch-b")
	shell.Run("restack --only")

	// 4. Verify branch-b is now on top of the amended branch-a
	shell.Log("Verifying branch-b is correctly rebased")
	shell.OnBranch("branch-b")
	// Verify that branch-a amended commit is an ancestor of branch-b
	shell.Git("merge-base --is-ancestor branch-a branch-b")
}
