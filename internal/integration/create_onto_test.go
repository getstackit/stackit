package integration

import (
	"testing"
)

// TestCreateOnto verifies `stackit create --onto <branch>` creates a branch
// tracked as a child of the target, built directly on the target's tip
// rather than the current branch's history.
func TestCreateOnto(t *testing.T) {
	t.Parallel()

	t.Run("branches onto an unrelated tracked branch, not the current branch", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// main -> branch-a (current), main -> branch-b (onto target). Siblings.
		sh.Write("a", "content a").Run("create branch-a -m 'feat: a'")
		sh.Checkout("main")
		sh.Write("b", "content b").Run("create branch-b -m 'feat: b'")
		sh.Checkout("branch-a")

		sh.Write("c", "content c").Run("create branch-c --onto branch-b -m 'feat: c'")

		sh.OnBranch("branch-c")
		sh.ExpectBranchParent("branch-c", "branch-b")
		// Exactly the one new commit sits on top of branch-b — none of
		// branch-a's own history leaked in via a wide merge-base.
		sh.CommitCount("branch-b", "branch-c", 1)
	})

	t.Run("branches onto trunk directly", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		sh.Write("a", "content a").Run("create branch-a -m 'feat: a'")

		sh.Write("d", "content d").Run("create branch-d --onto main -m 'feat: d'")

		sh.OnBranch("branch-d")
		sh.ExpectBranchParent("branch-d", "main")
		sh.CommitCount("main", "branch-d", 1)
	})

	t.Run("works when the target is checked out in another worktree", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)
		sh.SetWorktreeBasePath(t.TempDir())

		// Anchor "shared" in its own worktree so it's checked out elsewhere.
		sh.Write("shared.txt", "shared content").Run("create shared -w -m 'feat: shared'")
		sh.OnBranch("main")
		worktreePath := sh.GetWorktreePath("shared")
		shW := sh.InWorktree(worktreePath)
		shW.OnBranch("shared")

		// From the main worktree, branch onto "shared" without disturbing it.
		sh.Write("e", "content e").Run("create branch-e --onto shared -m 'feat: e'")

		sh.OnBranch("branch-e")
		sh.ExpectBranchParent("branch-e", "shared")
		sh.CommitCount("shared", "branch-e", 1)

		// The other worktree's checkout is untouched.
		shW.OnBranch("shared")
	})

	t.Run("errors for a nonexistent target", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		sh.RunExpectError("create branch-x --onto does-not-exist -m 'feat: x' --allow-empty").
			OutputContains("does not exist")
	})

	t.Run("errors for an untracked target", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		sh.Git("branch plain-branch")

		sh.RunExpectError("create branch-x --onto plain-branch -m 'feat: x' --allow-empty").
			OutputContains("not tracked by stackit")
	})

	t.Run("errors for a locked target", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		sh.Write("a", "content a").Run("create branch-a -m 'feat: a'")
		sh.Run("lock branch-a")

		sh.RunExpectError("create branch-x --onto branch-a -m 'feat: x' --allow-empty").
			OutputContains("locked")
	})

	t.Run("errors when combined with --worktree", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		sh.Write("a", "content a").Run("create branch-a -m 'feat: a'")
		sh.Checkout("main")

		sh.RunExpectError("create branch-x --onto branch-a -w -m 'feat: x' --allow-empty").
			OutputContains("cannot be combined")
	})

	t.Run("errors when combined with --insert", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		sh.Write("a", "content a").Run("create branch-a -m 'feat: a'")
		sh.Checkout("main")

		sh.RunExpectError("create branch-x --onto branch-a -i -m 'feat: x' --allow-empty").
			OutputContains("cannot be combined")
	})
}
