package integration

import "testing"

// TestPrCommand verifies error paths for `stackit pr`. Success paths (a
// resolved pull request opened in the browser) are covered at the action
// level in internal/actions/pr_test.go, since opening a real browser isn't
// something CI/sandboxed environments can exercise.
func TestPrCommand(t *testing.T) {
	t.Parallel()

	t.Run("errors when the current branch has no pull request", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)
		sh.CreateLinearStack3()

		sh.RunExpectError("pr").OutputContains(`no pull request found for "c"`)
	})

	t.Run("errors when an explicit branch has no pull request", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)
		sh.CreateLinearStack3()

		sh.RunExpectError("pr b").OutputContains(`no pull request found for "b"`)
	})

	t.Run("--stack resolves to the root branch of the stack", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)
		sh.CreateLinearStack3()

		sh.RunExpectError("pr --stack").OutputContains(`no pull request found for "a"`)
	})

	t.Run("errors resolving a PR number without a configured remote", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)
		sh.CreateLinearStack3()

		sh.RunExpectError("pr 123").OutputContains("cannot resolve PR #123")
	})

	t.Run("rejects more than one argument", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)
		sh.CreateLinearStack3()

		sh.RunExpectError("pr a b")
	})
}
