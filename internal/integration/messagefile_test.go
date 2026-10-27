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

	t.Run("create reads single-line and multi-line messages from file", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		msgPath := writeMessageFile(t, "feat: from file\n")
		sh.Write("feature", "content").
			Run("create feature -F " + msgPath).
			OnBranch("feature").
			Git("log -1 --format=%s").
			OutputContains("feat: from file")

		// Preserves multi-line subject and body.
		msgPath = writeMessageFile(t, "feat: subject line\n\nbody paragraph one\n\nbody paragraph two\n")
		sh.Write("feature_multi", "content").
			Run("create feature-multi -F " + msgPath).
			Git("log -1 --format=%B").
			OutputContains("feat: subject line").
			OutputContains("body paragraph one").
			OutputContains("body paragraph two")
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
		// -F is the shorthand for --message-file (consistent with create/modify/squash).
		sh.Run("split --by-file file_test.txt --as-sibling -F " + msgPath).
			Checkout("feature_split").
			Git("log -1 --format=%s").
			OutputContains("feat: extracted from file")
	})

	// Flag validation fails before any command mutates the repo, so every
	// error case shares one shell. The create errors run before "feature"
	// exists (they prove create propagates the message-file errors); the
	// successful modify/describe runs come last.
	t.Run("validation errors, then modify and describe from file", func(t *testing.T) {
		t.Parallel()
		sh := NewTestShellInProcess(t)

		t.Run("create errors", func(t *testing.T) {
			sh := sh.WithT(t)
			sh.Write("feature", "content")
			requireReadOnly(sh, func() {
				sh.RunExpectError("create feature -m 'inline' -F " + writeMessageFile(t, "from file")).
					OutputContains("cannot use --message and --message-file together")

				sh.RunExpectError("create feature -F /tmp/stackit-nonexistent-msg").
					OutputContains("file not found").
					OutputContains(`use "-" to read from stdin`)

				sh.RunExpectError("create feature -F " + writeMessageFile(t, "")).
					OutputContains("is empty")
			})
		})

		// sh.Write("file", ...) creates a file named "file_test.txt"
		// (helper concatenates "_test.txt"); --by-file references that path.
		sh.Write("file", "content").
			Run("create feature -m 'Original'")

		t.Run("modify, squash, split, and describe errors", func(t *testing.T) {
			sh := sh.WithT(t)
			requireReadOnly(sh, func() {
				sh.RunExpectError("describe").
					OutputContains("nothing to set")

				sh.RunExpectError("describe -m 'inline' -F " + writeMessageFile(t, "Title\n\nBody")).
					OutputContains("cannot use --message-file with --title")

				msgPath := writeMessageFile(t, "from file")
				sh.RunExpectError("modify -m 'inline' -F " + msgPath).
					OutputContains("cannot use --message and --message-file together")

				sh.RunExpectError("squash -m 'inline' -F " + msgPath).
					OutputContains("cannot use --message and --message-file together")

				sh.RunExpectError("split --by-hunk --patch - --message-file -").
					OutputContains("cannot read both --message-file and --patch from stdin")

				// Regression: previously --by-commit --message-file - drained stdin
				// (or read the file) before the mode-compat check fired. Now flag
				// validation runs first.
				sh.RunExpectError("split --by-commit --message-file " + writeMessageFile(t, "feat: should not be read")).
					OutputContains("--message/--message-file can only be used with --by-file")

				sh.RunExpectError("split --by-file file_test.txt --as-sibling -m 'inline' --message-file " + msgPath).
					OutputContains("cannot use --message and --message-file together")
			})
		})

		t.Run("modify updates commit message from file", func(t *testing.T) {
			sh := sh.WithT(t)
			msgPath := writeMessageFile(t, "feat: updated from file\n")
			sh.Run("modify -F " + msgPath).
				Git("log -1 --format=%s").
				OutputContains("feat: updated from file")
		})

		t.Run("describe reads title and body from file", func(t *testing.T) {
			sh := sh.WithT(t)
			msgPath := writeMessageFile(t, "Auth Feature\n\nOAuth2 implementation\n")
			sh.Run("describe -F " + msgPath).
				Run("describe --show").
				OutputContains("Auth Feature").
				OutputContains("OAuth2 implementation")
		})
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
