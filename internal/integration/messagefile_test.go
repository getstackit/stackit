package integration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestMessageFile verifies the --message-file (-F) flag wires through to
// create, modify, squash, and split — keeping commit-message text out of the
// literal command line for permission-rule stability.
func TestMessageFile(t *testing.T) {
	t.Parallel()

	t.Run("create reads commit message from file", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		msgPath := writeMessageFile(t, "feat: from file\n")

		sh.Write("feature", "content").
			Run("create feature -F " + msgPath).
			OnBranch("feature").
			Git("log -1 --format=%s").
			OutputContains("feat: from file")
	})

	t.Run("create preserves multi-line subject and body", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		msgPath := writeMessageFile(t, "feat: subject line\n\nbody paragraph one\n\nbody paragraph two\n")

		sh.Write("feature", "content").
			Run("create feature -F " + msgPath).
			Git("log -1 --format=%B").
			OutputContains("feat: subject line").
			OutputContains("body paragraph one").
			OutputContains("body paragraph two")
	})

	t.Run("modify updates commit message from file", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		sh.Write("feature", "content").
			Run("create feature -m 'Original'")

		msgPath := writeMessageFile(t, "feat: updated from file\n")
		sh.Run("modify -F " + msgPath).
			Git("log -1 --format=%s").
			OutputContains("feat: updated from file")
	})

	t.Run("squash reads commit message from file", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		sh.Write("a1", "a1").
			Run("create feature -m 'feat: first'")
		sh.Write("a2", "a2").
			Run("modify -c -m 'feat: second'")

		msgPath := writeMessageFile(t, "feat: squashed from file\n")
		sh.Run("squash -F " + msgPath).
			Git("log -1 --format=%s").
			OutputContains("feat: squashed from file")
	})

	t.Run("split reads commit message from file", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		// sh.Write("file", ...) creates a file named "file_test.txt"
		// (helper concatenates "_test.txt"); --by-file references that path.
		sh.Write("file", "content").
			Run("create feature -m 'Add feature'")

		msgPath := writeMessageFile(t, "feat: extracted from file\n")
		sh.Run("split --by-file file_test.txt --as-sibling --message-file " + msgPath).
			Checkout("feature_split").
			Git("log -1 --format=%s").
			OutputContains("feat: extracted from file")
	})

	t.Run("describe reads title and body from file", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		sh.Write("feature", "content").
			Run("create feature -m 'feat: x'")

		msgPath := writeMessageFile(t, "Auth Feature\n\nOAuth2 implementation\n")
		sh.Run("describe -F " + msgPath).
			Run("describe --show").
			OutputContains("Auth Feature").
			OutputContains("OAuth2 implementation")
	})

	t.Run("describe errors with no input in non-interactive mode", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)
		sh.Write("feature", "content").
			Run("create feature -m 'feat: x'")
		sh.RunExpectError("describe").
			OutputContains("nothing to set")
	})

	t.Run("describe errors when both --title and --message-file are given", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)
		sh.Write("feature", "content").Run("create feature -m 'feat: x'")
		msgPath := writeMessageFile(t, "Title\n\nBody")
		sh.RunExpectError("describe -m 'inline' -F " + msgPath).
			OutputContains("cannot use --message-file with --title")
	})

	t.Run("create errors when both --message and --message-file are given", func(t *testing.T) {
		t.Parallel()
		runMutexTest(t, "create feature -m 'inline' -F ")
	})

	t.Run("modify errors when both --message and --message-file are given", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)
		sh.Write("feature", "content").Run("create feature -m 'Original'")
		msgPath := writeMessageFile(t, "from file")
		sh.RunExpectError("modify -m 'inline' -F " + msgPath).
			OutputContains("cannot use --message and --message-file together")
	})

	t.Run("squash errors when both --message and --message-file are given", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)
		sh.Write("feature", "content").Run("create feature -m 'Original'")
		msgPath := writeMessageFile(t, "from file")
		sh.RunExpectError("squash -m 'inline' -F " + msgPath).
			OutputContains("cannot use --message and --message-file together")
	})

	t.Run("create errors when message file does not exist", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)
		sh.Write("feature", "content").
			RunExpectError("create feature -F /tmp/stackit-nonexistent-msg").
			OutputContains("file not found").
			OutputContains(`use "-" to read from stdin`)
	})

	t.Run("create errors when message file is empty", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)
		msgPath := writeMessageFile(t, "")
		sh.Write("feature", "content").
			RunExpectError("create feature -F " + msgPath).
			OutputContains("is empty")
	})

	t.Run("split rejects --message-file - and --patch - together", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)
		sh.Write("file", "content").
			Run("create feature -m 'init'").
			RunExpectError("split --by-hunk --patch - --message-file -").
			OutputContains("cannot read both --message-file and --patch from stdin")
	})

	t.Run("split rejects --message-file before reading any input", func(t *testing.T) {
		// Regression: previously --by-commit --message-file - drained stdin
		// (or read the file) before the mode-compat check fired. Now flag
		// validation runs first.
		t.Parallel()
		sh := NewTestShellInProcess(t)
		msgPath := writeMessageFile(t, "feat: should not be read")
		sh.Write("file", "content").
			Run("create feature -m 'init'").
			RunExpectError("split --by-commit --message-file " + msgPath).
			OutputContains("--message/--message-file can only be used with --by-file")
	})

	t.Run("split errors when both --message and --message-file are given", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)
		sh.Write("file", "content").Run("create feature -m 'init'")
		msgPath := writeMessageFile(t, "from file")
		sh.RunExpectError("split --by-file file_test.txt --as-sibling -m 'inline' --message-file " + msgPath).
			OutputContains("cannot use --message and --message-file together")
	})
}

// writeMessageFile writes content to a temp file outside the repo working
// tree (so it doesn't leak into staged changes) and returns the path.
func writeMessageFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "msg")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

// runMutexTest covers the --message + --message-file mutex error for the
// create command. modify/squash variants need their own setup so they live
// inline above.
func runMutexTest(t *testing.T, cmd string) {
	t.Helper()
	sh := NewTestShellInProcess(t)
	msgPath := writeMessageFile(t, "from file")
	sh.Write("feature", "content").
		RunExpectError(cmd + msgPath).
		OutputContains("cannot use --message and --message-file together")
}
