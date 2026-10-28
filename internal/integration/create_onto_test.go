package integration

import (
	"testing"
)

// TestCreateOnto verifies `stackit create --onto <branch>` creates a branch
// tracked as a child of the target, built directly on the target's tip
// rather than the current branch's history.
func TestCreateOnto(t *testing.T) {
	t.Parallel()

	t.Run("branches onto an unrelated tracked branch, then onto trunk", func(t *testing.T) {
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

		// From a stacked branch, --onto trunk branches directly off main.
		sh.Write("d", "content d").Run("create branch-d --onto main -m 'feat: d'")

		sh.OnBranch("branch-d")
		sh.ExpectBranchParent("branch-d", "main")
		sh.CommitCount("main", "branch-d", 1)
	})

	t.Run("rejects a target owned by another worktree", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)
		sh.SetWorktreeBasePath(t.TempDir())

		// Anchor "shared" in its own worktree so it's checked out elsewhere.
		sh.Write("shared.txt", "shared content").Run("create shared -w -m 'feat: shared'")
		sh.OnBranch("main")
		worktreePath := sh.GetWorktreePath("shared")
		shW := sh.InWorktree(worktreePath)
		shW.OnBranch("shared")

		// Creating a child would mutate shared's stack, so it must be done from
		// the worktree that owns shared.
		sh.Write("e", "content e").RunExpectError("create branch-e --onto shared -m 'feat: e'").
			OutputContains("belongs to worktree")
		shW.OnBranch("shared")
	})

	t.Run("inherits scope from the --onto target, not the current branch", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)
		sh.Run("config set branch.pattern \"{scope}/{message}\"")

		// branch-a (current) carries scope JIRA-1; branch-b (--onto target)
		// carries a different scope, JIRA-2.
		sh.Write("a", "content a").Run("create branch-a -m 'a' --scope JIRA-1")
		sh.Checkout("main")
		sh.Write("b", "content b").Run("create branch-b -m 'b' --scope JIRA-2")
		sh.Checkout("branch-a")

		// No explicit branch name, so it's generated from the pattern. The
		// generated name must use branch-b's scope, not branch-a's, since
		// branch-b is the real parent.
		sh.Write("c", "content c").Run("create --onto branch-b -m 'child feature'")

		sh.OnBranch("JIRA-2/child-feature")
		sh.ExpectBranchParent("JIRA-2/child-feature", "branch-b")
	})

	// Every validation error fires before create mutates anything, so the
	// error cases share one shell. The worktree anchor case runs last because
	// creating the worktree adds an anchor branch.
	t.Run("rejects invalid targets", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)
		sh.SetWorktreeBasePath(t.TempDir())
		sh.Git("branch plain-branch")

		t.Run("nonexistent and untracked targets", func(t *testing.T) {
			sh := sh.WithT(t)
			requireReadOnly(sh, func() {
				sh.RunExpectError("create branch-x --onto does-not-exist -m 'feat: x' --allow-empty").
					OutputContains("does not exist")

				sh.RunExpectError("create branch-x --onto plain-branch -m 'feat: x' --allow-empty").
					OutputContains("not tracked by stackit")
			})
		})

		t.Run("flag combinations and locked target", func(t *testing.T) {
			sh := sh.WithT(t)
			sh.Write("a", "content a").Run("create branch-a -m 'feat: a'")
			sh.Checkout("main")

			requireReadOnly(sh, func() {
				sh.RunExpectError("create branch-x --onto branch-a -w -m 'feat: x' --allow-empty").
					OutputContains("cannot be combined")

				sh.RunExpectError("create branch-x --onto branch-a -i -m 'feat: x' --allow-empty").
					OutputContains("cannot be combined")
			})

			sh.Run("lock branch-a")
			requireReadOnly(sh, func() {
				sh.RunExpectError("create branch-x --onto branch-a -m 'feat: x' --allow-empty").
					OutputContains("locked")
			})
		})

		t.Run("worktree anchor target", func(t *testing.T) {
			sh := sh.WithT(t)
			sh.Run("worktree create my-wt")
			anchorBranch := findWorktreeAnchor(t, sh)

			requireReadOnly(sh, func() {
				sh.RunExpectError("create branch-x --onto " + anchorBranch + " -m 'feat: x' --allow-empty").
					OutputContains("cannot create a branch onto worktree anchor")
			})
		})
	})
}
