package doctor

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type recordedCheck struct {
	name    string
	status  CheckStatus
	message string
}

type recordingHandler struct {
	NullHandler
	checks []recordedCheck
}

func (h *recordingHandler) OnCheck(name string, status CheckStatus, message string) {
	h.checks = append(h.checks, recordedCheck{name, status, message})
}

func (h *recordingHandler) gitLocksCheck(t *testing.T) recordedCheck {
	t.Helper()
	for _, c := range h.checks {
		if c.name == "git_locks" {
			return c
		}
	}
	require.Fail(t, "no git_locks check was recorded")
	return recordedCheck{}
}

// newGitDir returns a repo root with an empty .git directory. checkGitLocks only
// needs the directory structure, not a real repository.
func newGitDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git", "refs", "heads"), 0o755))
	return dir
}

func writeLock(t *testing.T, path string, age time.Duration) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, nil, 0o644))
	when := time.Now().Add(-age)
	require.NoError(t, os.Chtimes(path, when, when))
}

func TestCheckGitLocks(t *testing.T) {
	t.Parallel()

	t.Run("no locks passes", func(t *testing.T) {
		t.Parallel()
		dir := newGitDir(t)
		rec := &recordingHandler{}

		require.Equal(t, 0, checkGitLocks(dir, rec, 0))
		require.Equal(t, CheckPassed, rec.gitLocksCheck(t).status)
	})

	t.Run("stale index.lock warns", func(t *testing.T) {
		t.Parallel()
		dir := newGitDir(t)
		writeLock(t, filepath.Join(dir, ".git", "index.lock"), 10*time.Minute)
		rec := &recordingHandler{}

		require.Equal(t, 1, checkGitLocks(dir, rec, 0))
		check := rec.gitLocksCheck(t)
		require.Equal(t, CheckWarning, check.status)
		require.Contains(t, check.message, "index.lock")
	})

	t.Run("stale per-ref lock warns", func(t *testing.T) {
		t.Parallel()
		dir := newGitDir(t)
		writeLock(t, filepath.Join(dir, ".git", "refs", "heads", "main.lock"), 10*time.Minute)
		rec := &recordingHandler{}

		require.Equal(t, 1, checkGitLocks(dir, rec, 0))
		check := rec.gitLocksCheck(t)
		require.Equal(t, CheckWarning, check.status)
		require.Contains(t, check.message, "main.lock")
	})

	t.Run("recent lock does not warn", func(t *testing.T) {
		t.Parallel()
		dir := newGitDir(t)
		writeLock(t, filepath.Join(dir, ".git", "index.lock"), 2*time.Second)
		rec := &recordingHandler{}

		require.Equal(t, 0, checkGitLocks(dir, rec, 0))
		require.Equal(t, CheckPassed, rec.gitLocksCheck(t).status)
	})
}
