package integration

import (
	"testing"
)

// TestPrCommand verifies that `stackit pr` resolves and prints the URL of the
// relevant pull request for the current branch, an explicit branch, a PR
// number, or the root of the current stack.
func TestPrCommand(t *testing.T) {
	t.Parallel()

	t.Run("opens the current branch's PR", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		sh.Write("a", "content a").Run("create branch-a -m 'feat: a'")
		sh.SetPrMetadata("branch-a", PRMetadata{Number: 42, URL: "https://github.com/owner/repo/pull/42"})

		sh.Run("pr").OutputContains("https://github.com/owner/repo/pull/42")
	})

	t.Run("opens a PR by branch name", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		sh.Write("a", "content a").Run("create branch-a -m 'feat: a'")
		sh.SetPrMetadata("branch-a", PRMetadata{Number: 42, URL: "https://github.com/owner/repo/pull/42"})
		sh.Checkout("main")

		sh.Run("pr branch-a").OutputContains("https://github.com/owner/repo/pull/42")
	})

	t.Run("opens a PR by number", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		sh.Write("a", "content a").Run("create branch-a -m 'feat: a'")
		sh.SetPrMetadata("branch-a", PRMetadata{Number: 42, URL: "https://github.com/owner/repo/pull/42"})
		sh.Checkout("main")

		sh.Run("pr 42").OutputContains("https://github.com/owner/repo/pull/42")
	})

	t.Run("--stack opens the root PR of the stack", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// main -> a -> b -> c, currently on c.
		sh.CreateLinearStack3()
		sh.SetPrMetadata("a", PRMetadata{Number: 1, URL: "https://github.com/owner/repo/pull/1"})
		sh.SetPrMetadata("c", PRMetadata{Number: 3, URL: "https://github.com/owner/repo/pull/3"})

		sh.Run("pr --stack").
			OutputContains("https://github.com/owner/repo/pull/1").
			OutputNotContains("https://github.com/owner/repo/pull/3")
	})

	t.Run("errors when the branch has no PR", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		sh.Write("a", "content a").Run("create branch-a -m 'feat: a'")

		sh.RunExpectError("pr").OutputContains("no associated pull request")
	})

	t.Run("errors for an unknown branch", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		sh.RunExpectError("pr does-not-exist").OutputContains("not found")
	})

	t.Run("errors for an unknown PR number", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		sh.RunExpectError("pr 999").OutputContains("no tracked branch found")
	})

	t.Run("errors when HEAD is detached", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		sh.Git("checkout --detach main")

		sh.RunExpectError("pr").OutputContains("not on a branch")
	})

	t.Run("--stack errors when the stack root has no PR", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// main -> a -> b -> c, currently on c. Only c has a PR; the stack
		// root (a) does not.
		sh.CreateLinearStack3()
		sh.SetPrMetadata("c", PRMetadata{Number: 3, URL: "https://github.com/owner/repo/pull/3"})

		sh.RunExpectError("pr --stack").OutputContains("no associated pull request")
	})
}
