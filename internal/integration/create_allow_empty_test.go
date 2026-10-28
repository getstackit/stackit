package integration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCreateEmptyBranchGuard verifies that, in non-interactive mode, `create`
// refuses to silently produce an empty branch when the working tree has changes
// that simply weren't staged (the common "forgot to git add" agent footgun),
// while still allowing intentional empty branches on a clean tree or with
// --allow-empty.
//
// The steps share one shell: each rejection leaves the repo untouched, and each
// successful create leaves exactly the working-tree state the next step needs.
func TestCreateEmptyBranchGuard(t *testing.T) { //nolint:tparallel // subtests share one fixture and run in order
	t.Parallel()
	sh := NewTestShellInProcess(t)
	sh.Write("base", "v1").
		Run("create base -m 'feat: base'")

	t.Run("rejects create when untracked files exist but nothing staged", func(t *testing.T) {
		sh := sh.WithT(t)
		sh.WriteFile("untracked.txt", "x").
			Git("reset HEAD untracked.txt")
		requireReadOnly(sh, func() {
			sh.RunExpectError("create next -m 'feat: next'").
				OutputContains("nothing staged but the working tree has changes")
		})
		require.NoError(t, os.Remove(filepath.Join(sh.Dir(), "untracked.txt")))
	})

	t.Run("rejects create when unstaged tracked changes exist but nothing staged", func(t *testing.T) {
		sh := sh.WithT(t)
		// Modify the tracked file, then unstage so it is a dirty-but-unstaged change.
		sh.WriteFile("base_test.txt", "v2").
			Git("reset HEAD base_test.txt")
		requireReadOnly(sh, func() {
			sh.RunExpectError("create next -m 'feat: next'").
				OutputContains("nothing staged but the working tree has changes")
		})
	})

	t.Run("--allow-empty creates a branch despite unstaged changes", func(t *testing.T) {
		sh := sh.WithT(t)
		sh.Run("create next --allow-empty -m 'feat: empty'").
			OnBranch("next")
	})

	t.Run("--all stages the changes and avoids the guard", func(t *testing.T) {
		sh := sh.WithT(t)
		// The unstaged base_test.txt change carried over from the previous step.
		sh.Run("create next-all --all -m 'feat: next'").
			OnBranch("next-all")
	})

	t.Run("clean tree still creates an empty scaffolding branch", func(t *testing.T) {
		sh := sh.WithT(t)
		// --all committed everything, so the working tree is clean here; an
		// empty branch is intentional.
		sh.Run("create scaffold -m 'feat: scaffold'").
			OnBranch("scaffold")
	})
}
