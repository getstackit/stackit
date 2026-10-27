package common

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReadMessage(t *testing.T) {
	t.Parallel()

	t.Run("returns inline message when no file given", func(t *testing.T) {
		t.Parallel()
		got, err := ReadMessage("feat: inline", "")
		require.NoError(t, err)
		require.Equal(t, "feat: inline", got)
	})

	t.Run("returns empty when neither given", func(t *testing.T) {
		t.Parallel()
		got, err := ReadMessage("", "")
		require.NoError(t, err)
		require.Empty(t, got)
	})

	t.Run("errors when both given with actionable hint", func(t *testing.T) {
		t.Parallel()
		_, err := ReadMessage("feat: a", "/tmp/msg")
		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot use --message and --message-file together")
		require.Contains(t, err.Error(), "use --message-file - to read from stdin")
	})

	t.Run("reads from file path", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "msg")
		require.NoError(t, os.WriteFile(path, []byte("feat: from file\n"), 0o600))

		got, err := ReadMessage("", path)
		require.NoError(t, err)
		require.Equal(t, "feat: from file", got)
	})

	t.Run("trims surrounding whitespace", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "msg")
		require.NoError(t, os.WriteFile(path, []byte("\n\n  feat: trim me  \n\n"), 0o600))

		got, err := ReadMessage("", path)
		require.NoError(t, err)
		require.Equal(t, "feat: trim me", got)
	})

	t.Run("preserves internal newlines for multi-line messages", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "msg")
		require.NoError(t, os.WriteFile(path, []byte("feat: subject\n\nbody paragraph\n"), 0o600))

		got, err := ReadMessage("", path)
		require.NoError(t, err)
		require.Equal(t, "feat: subject\n\nbody paragraph", got)
	})

	t.Run("errors with stdin hint when file does not exist", func(t *testing.T) {
		t.Parallel()
		_, err := ReadMessage("", "/nonexistent/path/to/msg")
		require.Error(t, err)
		require.Contains(t, err.Error(), "--message-file")
		require.Contains(t, err.Error(), "/nonexistent/path/to/msg")
		require.Contains(t, err.Error(), "file not found")
		require.Contains(t, err.Error(), `use "-" to read from stdin`)
		// Caller should still be able to detect the underlying not-found
		// condition via errors.Is — the wrapped error chain is preserved.
		require.ErrorIs(t, err, fs.ErrNotExist, "expected error chain to satisfy fs.ErrNotExist")
	})

	t.Run("errors when file is empty", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "empty")
		require.NoError(t, os.WriteFile(path, nil, 0o600))

		_, err := ReadMessage("", path)
		require.Error(t, err)
		require.Contains(t, err.Error(), "is empty")
	})

	t.Run("errors when file is whitespace-only", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "ws")
		require.NoError(t, os.WriteFile(path, []byte("\n\n  \t\n"), 0o600))

		_, err := ReadMessage("", path)
		require.Error(t, err)
		require.Contains(t, err.Error(), "is empty")
	})

	t.Run("reads from stdin when path is dash", func(t *testing.T) {
		t.Parallel()
		r, w, err := os.Pipe()
		require.NoError(t, err)
		t.Cleanup(func() { _ = r.Close() })

		go func() {
			_, _ = w.WriteString("feat: from stdin\n")
			_ = w.Close()
		}()

		got, err := readMessageFrom("-", r)
		require.NoError(t, err)
		require.Equal(t, "feat: from stdin", got)
	})

	t.Run("errors when stdin pipe is empty", func(t *testing.T) {
		t.Parallel()
		r, w, err := os.Pipe()
		require.NoError(t, err)
		t.Cleanup(func() { _ = r.Close() })
		require.NoError(t, w.Close())

		_, err = readMessageFrom("-", r)
		require.Error(t, err)
		require.Contains(t, err.Error(), "--message-file - received empty input")
	})
}
