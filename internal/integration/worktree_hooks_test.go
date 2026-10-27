package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// normalizeDir resolves symlinks in the directory path.
// This is needed on macOS where /var is a symlink to /private/var.
func normalizeDir(t *testing.T, dir string) string {
	t.Helper()
	normalized, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	return normalized
}

// writeProjectConfig writes a .stackit.yaml file to the repo root
func writeProjectConfig(t *testing.T, dir string, content string) {
	t.Helper()
	normalizedDir := normalizeDir(t, dir)
	path := filepath.Join(normalizedDir, ".stackit.yaml")
	err := os.WriteFile(path, []byte(content), 0644)
	require.NoError(t, err)
}

// addApprovedHook adds a hook to the approved hooks list in git config
func addApprovedHook(t *testing.T, dir string, hook string) {
	t.Helper()
	cmd := exec.Command("git", "config", "--local", "--add", "stackit.hooks.approvedPostWorktreeCreate", hook)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "failed to add approved hook: %s", string(out))
}

func TestWorktreePostCreateHooks(t *testing.T) {
	t.Parallel()
	t.Run("filters empty hooks from config", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Create .stackit.yaml with empty hooks mixed in
		writeProjectConfig(t, sh.Dir(), `hooks:
  post-worktree-create:
    - ""
    - touch .valid_hook_ran
    - "   "
`)
		sh.Git("add .stackit.yaml").Git("commit -m 'add hooks config'")

		// Pre-approve only the valid hook (using git config)
		addApprovedHook(t, sh.Dir(), "touch .valid_hook_ran")

		// Create a worktree
		sh.WriteFile("empty.txt", "empty").
			Run("create emptystack -w -m 'empty hook test'")

		// Get the worktree path
		worktreePath := sh.GetWorktreePath("emptystack")

		// Verify only the valid hook ran (empty hooks were filtered)
		markerPath := filepath.Join(worktreePath, ".valid_hook_ran")
		_, err := os.Stat(markerPath)
		require.NoError(t, err, "Valid hook should have run and created marker file at %s", markerPath)
	})

	t.Run("runs approved hooks after worktree creation", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Create .stackit.yaml with a hook that creates a marker file
		// This needs to be committed to main FIRST so it's available in the main repo
		// when the hooks run (worktrees are created from trunk)
		writeProjectConfig(t, sh.Dir(), `hooks:
  post-worktree-create:
    - touch .hook_ran
`)
		// Commit to main so it persists across branch checkouts
		sh.Git("add .stackit.yaml").Git("commit -m 'add hooks config'")

		// Pre-approve the hook (using git config)
		addApprovedHook(t, sh.Dir(), "touch .hook_ran")

		// Create a worktree
		sh.WriteFile("feature.txt", "feature content").
			Run("create myfeature -w -m 'feature branch'")

		// Get the worktree path
		worktreePath := sh.GetWorktreePath("myfeature")

		// Verify the hook created the marker file in the worktree
		markerPath := filepath.Join(worktreePath, ".hook_ran")
		_, err := os.Stat(markerPath)
		require.NoError(t, err, "Hook should have created marker file at %s", markerPath)
	})

	t.Run("runs multiple hooks in order", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Create .stackit.yaml with multiple hooks
		// Commit to main first so it's available when hooks run
		writeProjectConfig(t, sh.Dir(), `hooks:
  post-worktree-create:
    - echo "first" > .hook_order
    - echo "second" >> .hook_order
`)
		sh.Git("add .stackit.yaml").Git("commit -m 'add hooks config'")

		// Pre-approve both hooks (using git config)
		addApprovedHook(t, sh.Dir(), `echo "first" > .hook_order`)
		addApprovedHook(t, sh.Dir(), `echo "second" >> .hook_order`)

		// Create a worktree
		sh.WriteFile("multi.txt", "multi").
			Run("create multistack -w -m 'multi hook test'")

		// Get the worktree path
		worktreePath := sh.GetWorktreePath("multistack")

		// Verify hooks ran in order
		orderPath := filepath.Join(worktreePath, ".hook_order")
		content, err := os.ReadFile(orderPath)
		require.NoError(t, err, "Failed to read hook order file")
		expected := "first\nsecond\n"
		require.Equal(t, expected, string(content), "Hooks should run in order")
	})

	t.Run("skips unapproved hooks", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Create .stackit.yaml with a hook
		// Commit to main first
		writeProjectConfig(t, sh.Dir(), `hooks:
  post-worktree-create:
    - touch .unapproved_hook_ran
`)
		sh.Git("add .stackit.yaml").Git("commit -m 'add hooks config'")

		// Do NOT pre-approve the hook - trunk is already set by init

		// Create a worktree (hook should be skipped since it requires approval and we're non-interactive)
		sh.WriteFile("skip.txt", "skip").
			Run("create skipstack -w -m 'skip hook test'")

		// Get the worktree path
		worktreePath := sh.GetWorktreePath("skipstack")

		// Verify the hook did NOT create the marker file (since it's unapproved and prompts fail in non-interactive mode)
		markerPath := filepath.Join(worktreePath, ".unapproved_hook_ran")
		_, err := os.Stat(markerPath)
		require.ErrorIs(t, err, os.ErrNotExist, "Unapproved hook should not have run - marker file should not exist at %s", markerPath)
	})

	t.Run("no hooks configured does nothing", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// No .stackit.yaml file - should work fine without hooks

		// Create a worktree
		sh.WriteFile("nohooks.txt", "nohooks").
			Run("create nohookstack -w -m 'no hooks test'")

		// Just verify worktree was created successfully
		worktreePath := sh.GetWorktreePath("nohookstack")
		_, err := os.Stat(worktreePath)
		require.NoError(t, err, "Worktree should exist at %s", worktreePath)
	})

	t.Run("hooks run for worktree create command", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Create .stackit.yaml with a hook
		// Commit to main first
		writeProjectConfig(t, sh.Dir(), `hooks:
  post-worktree-create:
    - touch .wt_create_hook_ran
`)
		sh.Git("add .stackit.yaml").Git("commit -m 'add hooks config'")

		// Pre-approve the hook (using git config)
		addApprovedHook(t, sh.Dir(), "touch .wt_create_hook_ran")

		// Create a worktree using the worktree create command
		sh.Run("worktree create my-worktree")

		// Get the worktree path using worktree open
		sh.Run("worktree open my-worktree")
		worktreePath := strings.TrimSpace(sh.Output())

		// Verify the hook created the marker file
		markerPath := filepath.Join(worktreePath, ".wt_create_hook_ran")
		_, err := os.Stat(markerPath)
		require.NoError(t, err, "Hook should have created marker file at %s", markerPath)
	})

	t.Run("hook failure does not prevent worktree creation", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// Create .stackit.yaml with a failing hook followed by a successful one
		writeProjectConfig(t, sh.Dir(), `hooks:
  post-worktree-create:
    - exit 1
    - touch .after_failure_hook_ran
`)
		sh.Git("add .stackit.yaml").Git("commit -m 'add hooks config'")

		// Pre-approve both hooks (using git config)
		addApprovedHook(t, sh.Dir(), "exit 1")
		addApprovedHook(t, sh.Dir(), "touch .after_failure_hook_ran")

		// Create a worktree - should succeed despite hook failure
		sh.WriteFile("fail.txt", "fail").
			Run("create failstack -w -m 'fail hook test'")

		// Get the worktree path
		worktreePath := sh.GetWorktreePath("failstack")

		// Verify worktree was created
		_, err := os.Stat(worktreePath)
		require.NoError(t, err, "Worktree should exist despite hook failure")

		// Verify the second hook still ran (hooks continue after failure)
		markerPath := filepath.Join(worktreePath, ".after_failure_hook_ran")
		_, err = os.Stat(markerPath)
		require.NoError(t, err, "Second hook should have run after first hook failed")
	})
}
