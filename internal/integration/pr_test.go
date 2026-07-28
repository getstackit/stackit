package integration

import (
	"testing"
)

// TestPrCommand verifies that `stackit pr` resolves the pull request for a
// branch, the current branch, or the root of a stack, and opens it in the
// browser (best-effort — the tests only assert on the printed URL).
func TestPrCommand(t *testing.T) {
	t.Parallel()

	t.Run("opens the current branch's pull request", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		sh.CreateLinearStack3()
		sh.SetPrMetadata("c", PRMetadata{Number: 42, State: "OPEN", URL: "https://github.com/acme/widgets/pull/42"})

		sh.Run("pr").
			OutputContains("#42").
			OutputContains("https://github.com/acme/widgets/pull/42")
	})

	t.Run("opens the pull request for an explicit branch", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		sh.CreateLinearStack3()
		sh.SetPrMetadata("a", PRMetadata{Number: 7, State: "OPEN", URL: "https://github.com/acme/widgets/pull/7"})
		sh.Checkout("main")

		sh.Run("pr a").
			OutputContains("#7").
			OutputContains("https://github.com/acme/widgets/pull/7")
	})

	t.Run("--stack opens the root pull request of the stack", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// main -> a -> b -> c, currently on c.
		sh.CreateLinearStack3()
		sh.SetPrMetadata("a", PRMetadata{Number: 1, State: "OPEN", URL: "https://github.com/acme/widgets/pull/1"})
		sh.SetPrMetadata("c", PRMetadata{Number: 3, State: "OPEN", URL: "https://github.com/acme/widgets/pull/3"})

		sh.Run("pr --stack").
			OutputContains("#1").
			OutputContains("https://github.com/acme/widgets/pull/1")
	})

	t.Run("errors on trunk", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		sh.CreateLinearStack3()
		sh.Checkout("main")

		sh.RunExpectError("pr").
			OutputContains("trunk branch")
	})

	t.Run("errors when the branch is not tracked", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		sh.CreateLinearStack3()

		sh.RunExpectError("pr does-not-exist").
			OutputContains("not tracked")
	})

	t.Run("errors when the branch has no pull request", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		sh.CreateLinearStack3()

		sh.RunExpectError("pr a").
			OutputContains("no associated pull request")
	})
}
