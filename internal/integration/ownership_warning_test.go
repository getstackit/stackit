package integration

import (
	"testing"
)

// Ownership warnings are the channel that reports a branch checked out somewhere
// its stack does not own. That only works if it stays quiet otherwise: a warning
// printed on every command trains people to skip the one that matters.

// TestOwnershipWarningsQuietFromManagedWorktree pins the false positive. The
// engine built inside a managed worktree reports that worktree as its repo root,
// so comparing checkout paths against the engine's root made the real main
// repository look like an unmanaged worktree — on every command run from a
// worktree.
func TestOwnershipWarningsQuietFromManagedWorktree(t *testing.T) {
	t.Parallel()

	sh := NewTestShellInProcess(t)
	sh.SetWorktreeBasePath(t.TempDir())

	sh.WriteFile("feature.txt", "feature").
		Run("create feature -w -m 'feature branch'")
	shW := sh.InWorktree(sh.GetWorktreePath("feature"))

	// Trunk is checked out in the main repository, which is exactly where it
	// belongs — running from the worktree must not call that a conflict.
	shW.Run("worktree list").
		OutputNotContains("unmanaged worktree")
}

// TestOwnershipWarningsReportGenuineUnmanagedWorktree is the other half: the
// quieting must not have removed the signal. A branch parked in a plain git
// worktree is still divergence worth reporting.
func TestOwnershipWarningsReportGenuineUnmanagedWorktree(t *testing.T) {
	t.Parallel()

	sh := NewTestShellInProcess(t)
	sh.SetWorktreeBasePath(t.TempDir())

	sh.WriteFile("feature.txt", "feature").
		Run("create feature -w -m 'feature branch'")

	strayDir := t.TempDir()
	sh.Git("branch stray-branch").
		Git("worktree add " + strayDir + " stray-branch")

	sh.InWorktree(sh.GetWorktreePath("feature")).
		Run("worktree list").
		OutputContains("unmanaged worktree").
		OutputContains("stray-branch")
}
