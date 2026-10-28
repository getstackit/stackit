package integration

import (
	"testing"
)

// TestPrCommand verifies that `stackit pr` resolves and prints the URL of the
// relevant pull request for the current branch, an explicit branch, a PR
// number, or the root of the current stack.
//
// `pr` only reads, so every case shares one repository: a linear stack
// (main -> a -> b -> c) for the --stack and error cases, and a separate
// branch-a off main for lookups by current branch, name, and number.
func TestPrCommand(t *testing.T) { //nolint:tparallel // subtests share one fixture and run in order
	t.Parallel()
	sh := NewTestShellInProcess(t)

	// main -> a -> b -> c, currently on c.
	sh.CreateLinearStack3()

	t.Run("errors for missing PRs and unknown targets", func(t *testing.T) {
		sh := sh.WithT(t)
		requireReadOnly(sh, func() {
			sh.RunExpectError("pr").OutputContains("no associated pull request")

			sh.RunExpectError("pr does-not-exist").OutputContains("not found")

			sh.RunExpectError("pr 999").OutputContains("no tracked branch found")
		})
	})

	t.Run("--stack errors when the stack root has no PR", func(t *testing.T) {
		sh := sh.WithT(t)
		// Only c has a PR; the stack root (a) does not.
		sh.SetPrMetadata("c", PRMetadata{Number: 3, URL: "https://github.com/owner/repo/pull/3"})
		requireReadOnly(sh, func() {
			sh.RunExpectError("pr --stack").OutputContains("no associated pull request")
		})
	})

	t.Run("--stack opens the root PR of the stack", func(t *testing.T) {
		sh := sh.WithT(t)
		sh.SetPrMetadata("a", PRMetadata{Number: 1, URL: "https://github.com/owner/repo/pull/1"})
		requireReadOnly(sh, func() {
			sh.Run("pr --stack").
				OutputContains("https://github.com/owner/repo/pull/1").
				OutputNotContains("https://github.com/owner/repo/pull/3")
		})
	})

	t.Run("opens the current branch's PR, then by name and by number", func(t *testing.T) {
		sh := sh.WithT(t)
		sh.Checkout("main")
		sh.Write("a", "content a").Run("create branch-a -m 'feat: a'")
		sh.SetPrMetadata("branch-a", PRMetadata{Number: 42, URL: "https://github.com/owner/repo/pull/42"})

		requireReadOnly(sh, func() {
			sh.Run("pr").OutputContains("https://github.com/owner/repo/pull/42")
		})

		sh.Checkout("main")
		requireReadOnly(sh, func() {
			sh.Run("pr branch-a").OutputContains("https://github.com/owner/repo/pull/42")

			sh.Run("pr 42").OutputContains("https://github.com/owner/repo/pull/42")
		})
	})

	t.Run("errors when HEAD is detached", func(t *testing.T) {
		sh := sh.WithT(t)
		sh.Git("checkout --detach main")

		sh.RunExpectError("pr").OutputContains("not on a branch")
	})
}
