package integration

import "testing"

// TestCreateEmptyBranchGuard verifies that, in non-interactive mode, `create`
// refuses to silently produce an empty branch when the working tree has changes
// that simply weren't staged (the common "forgot to git add" agent footgun),
// while still allowing intentional empty branches on a clean tree or with
// --allow-empty.
func TestCreateEmptyBranchGuard(t *testing.T) {
	t.Parallel()

	t.Run("rejects create when unstaged tracked changes exist but nothing staged", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)
		sh.Write("base", "v1").
			Run("create base -m 'feat: base'")
		// Modify the tracked file, then unstage so it is a dirty-but-unstaged change.
		sh.WriteFile("base_test.txt", "v2").
			Git("reset HEAD base_test.txt")
		sh.RunExpectError("create next -m 'feat: next'").
			OutputContains("nothing staged but the working tree has changes")
	})

	t.Run("rejects create when untracked files exist but nothing staged", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)
		sh.Write("base", "v1").
			Run("create base -m 'feat: base'")
		sh.WriteFile("untracked.txt", "x").
			Git("reset HEAD untracked.txt")
		sh.RunExpectError("create next -m 'feat: next'").
			OutputContains("nothing staged but the working tree has changes")
	})

	t.Run("--allow-empty creates a branch despite unstaged changes", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)
		sh.Write("base", "v1").
			Run("create base -m 'feat: base'")
		sh.WriteFile("base_test.txt", "v2").
			Git("reset HEAD base_test.txt")
		sh.Run("create next --allow-empty -m 'feat: empty'").
			OnBranch("next")
	})

	t.Run("--all stages the changes and avoids the guard", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)
		sh.Write("base", "v1").
			Run("create base -m 'feat: base'")
		sh.WriteFile("base_test.txt", "v2").
			Git("reset HEAD base_test.txt")
		sh.Run("create next --all -m 'feat: next'").
			OnBranch("next")
	})

	t.Run("clean tree still creates an empty scaffolding branch", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)
		sh.Write("base", "v1").
			Run("create base -m 'feat: base'")
		// Working tree is clean here; an empty branch is intentional.
		sh.Run("create scaffold -m 'feat: scaffold'").
			OnBranch("scaffold")
	})
}
