package integration

import (
	"testing"
)

func TestSubmitUntrackedBranch(t *testing.T) {
	t.Parallel()

	t.Run("submit on untracked branch shows tracking suggestion in non-interactive mode", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		sh.Git("checkout -b untracked-feature")
		sh.Write("feature.txt", "some content")
		sh.Git("add .")
		sh.Git("commit -m 'add feature'")

		sh.Run("ss --dry-run").
			OutputContains("not tracked").
			OutputContains("stackit track")
	})

	t.Run("submit with --branch flag on untracked branch shows tracking suggestion", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		sh.Git("checkout -b untracked-feature")
		sh.Write("feature.txt", "some content")
		sh.Git("add .")
		sh.Git("commit -m 'add feature'")

		sh.Checkout("main")

		sh.Run("ss --dry-run --branch untracked-feature").
			OutputContains("not tracked").
			OutputContains("stackit track")
	})

	t.Run("submit on tracked branch works normally", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		sh.Write("feature.txt", "some content")
		sh.Run("create feature -m 'add feature'")

		sh.Run("ss --dry-run").
			OutputContains("feature").
			OutputContains("create")
	})

	t.Run("submit with --branch flag on tracked branch works normally", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		sh.Write("feature.txt", "some content")
		sh.Run("create feature -m 'add feature'")

		sh.Checkout("main")

		sh.Run("ss --dry-run --branch feature").
			OutputContains("feature").
			OutputContains("create")
	})
}
