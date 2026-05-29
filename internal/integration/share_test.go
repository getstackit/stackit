package integration

import (
	"testing"
)

// TestShareCommand verifies that `stackit share` renders the current stack as
// Slack-ready markdown.
func TestShareCommand(t *testing.T) {
	t.Parallel()

	t.Run("renders stack base-first with current marker", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// main -> a -> b -> c, currently on c.
		sh.CreateLinearStack3()

		sh.Run("share").
			OutputContains("• `a` _(no PR)_").
			OutputContains("• `b` _(no PR)_").
			OutputContains("• `c` _(no PR)_ 👈")
	})

	t.Run("shares the stack for an explicit branch", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		sh.CreateLinearStack3()
		sh.Checkout("main")

		sh.Run("share --branch b").
			OutputContains("• `a` _(no PR)_").
			OutputContains("• `b` _(no PR)_").
			OutputContains("• `c` _(no PR)_")
	})

	t.Run("uses a custom marker", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		sh.CreateLinearStack3()

		sh.Run("share --marker (here)").
			OutputContains("• `c` _(no PR)_ (here)")
	})

	t.Run("errors on trunk", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		sh.CreateLinearStack3()
		sh.Checkout("main")

		sh.RunExpectError("share").
			OutputContains("trunk branch")
	})
}
