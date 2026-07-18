package integration

import (
	"testing"
)

// TestSubmitPrunesUnsubmittableSubtrees locks in submit's degrade-gracefully
// behavior: a branch that is not restacked on its in-submission parent is
// skipped together with its descendants (with a warning naming each), while
// the submittable part of the stack proceeds. Previously this was a hard
// error that blocked the entire submission.
func TestSubmitPrunesUnsubmittableSubtrees(t *testing.T) {
	t.Parallel()
	sh := NewTestShellInProcess(t)

	// main -> p -> c -> gc
	sh.Write("p.txt", "p").Run("create p -m 'p'").OnBranch("p")
	sh.Write("c.txt", "c").Run("create c -m 'c'").OnBranch("c")
	sh.Write("gc.txt", "gc").Run("create gc -m 'gc'").OnBranch("gc")

	// Advance trunk, then restack ONLY p — stranding c and gc on p's old tip.
	sh.Checkout("main")
	sh.Write("trunk.txt", "trunk")
	sh.Git("commit -m 'advance trunk'")
	sh.Checkout("p")
	sh.Run("restack --only")

	// Submitting the stack prunes c (not restacked on p) and gc (parent
	// pruned) but still plans p.
	sh.Run("ss --dry-run").
		OutputContains("Skipping c").
		OutputContains("not been restacked on its parent").
		OutputContains("Skipping gc").
		OutputContains("its parent c was skipped").
		OutputContains("Dry run complete")
}
