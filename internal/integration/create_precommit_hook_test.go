package integration

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCreatePreCommitHookFailure verifies that when a git pre-commit hook
// rejects the commit during `stackit create`, the user is left on the branch
// they were on before the command ran, not on main/trunk.
func TestCreatePreCommitHookFailure(t *testing.T) {
	t.Parallel()

	t.Run("returns to original branch after pre-commit hook failure", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Build up a stack: main -> base
		sh.Write("base.txt", "v1").
			Run("create base -m 'feat: base'").
			OnBranch("base")

		// Install a git pre-commit hook that always fails.
		hookPath := filepath.Join(sh.Dir(), ".git", "hooks", "pre-commit")
		err := os.WriteFile(hookPath, []byte("#!/bin/sh\nexit 1\n"), 0o755)
		if err != nil {
			t.Fatalf("failed to write pre-commit hook: %v", err)
		}

		// Stage a change, then try to create a new branch. The pre-commit hook
		// will fire and fail, causing the commit to be rejected.
		sh.WriteFile("next.txt", "v1")
		sh.RunExpectError("create next -m 'feat: next'")

		// The user should be back on "base", not on "main" or "next".
		sh.OnBranch("base")
	})

	t.Run("new branch is cleaned up after pre-commit hook failure", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		sh.Write("base.txt", "v1").
			Run("create base -m 'feat: base'").
			OnBranch("base")

		hookPath := filepath.Join(sh.Dir(), ".git", "hooks", "pre-commit")
		err := os.WriteFile(hookPath, []byte("#!/bin/sh\nexit 1\n"), 0o755)
		if err != nil {
			t.Fatalf("failed to write pre-commit hook: %v", err)
		}

		sh.WriteFile("next.txt", "v1")
		sh.RunExpectError("create next -m 'feat: next'")

		// The partially-created branch should not exist.
		sh.HasBranches("base", "main")
	})
}
