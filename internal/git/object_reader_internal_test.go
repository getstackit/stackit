package git

import (
	"bufio"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// initTestRepo creates a minimal git repo in a temp dir and returns its path.
func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test",
			"GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Test",
			"GIT_COMMITTER_EMAIL=test@example.com",
			"GIT_CONFIG_NOSYSTEM=1",
			"HOME="+t.TempDir(),
		)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}

	run("init", "-b", "main")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "f.txt"), []byte("hello"), 0o644))
	run("add", ".")
	run("commit", "-m", "init")
	return dir
}

// TestReadObjectsBatch_ClearsCmdOnWriteError verifies that ReadObjectsBatch
// clears r.cmd when stdin becomes unwritable, so the next call restarts a
// fresh process instead of failing forever on the dead handle.
func TestReadObjectsBatch_ClearsCmdOnWriteError(t *testing.T) {
	t.Parallel()

	dir := initTestRepo(t)
	reader := newObjectReader(func() (string, error) { return dir, nil })

	// Warm up the process with a successful call.
	results, err := reader.ReadObjectsBatch([]string{"HEAD"})
	require.NoError(t, err)
	require.NotNil(t, results)
	require.NotNil(t, reader.cmd, "process should be running after a successful call")

	// Close stdin to simulate the pipe becoming unwritable (e.g. process died).
	// The next write to r.stdin will fail with "io: read/write on closed pipe".
	_ = reader.stdin.Close()

	// First call after stdin closes: must fail AND clear r.cmd.
	_, err = reader.ReadObjectsBatch([]string{"HEAD"})
	require.Error(t, err, "ReadObjectsBatch must return an error when stdin is closed")
	require.Nil(t, reader.cmd, "r.cmd must be nil after write failure so the next call can restart")

	// Second call must restart the process and succeed.
	results2, err := reader.ReadObjectsBatch([]string{"HEAD"})
	require.NoError(t, err, "ReadObjectsBatch must succeed after restarting the process")
	require.NotEmpty(t, results2)
}

// erroringReader always fails, simulating a broken stdout stream.
type erroringReader struct{}

func (erroringReader) Read([]byte) (int, error) { return 0, io.ErrClosedPipe }

// TestReadObject_ClearsCmdOnReadError verifies that ReadObject, like
// ReadObjectsBatch, clears r.cmd when the stdout stream breaks, so a
// single bad read doesn't permanently poison the shared process for every
// later call. The stdout reader is swapped out directly (rather than
// killing the process) so the write path stays healthy and the test
// isolates ReadObject's read-error branch from its separate write-retry
// logic.
func TestReadObject_ClearsCmdOnReadError(t *testing.T) {
	t.Parallel()

	dir := initTestRepo(t)

	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	require.NoError(t, err)
	headSHA := strings.TrimSpace(string(out))

	reader := newObjectReader(func() (string, error) { return dir, nil })

	// Warm up so the process is running.
	_, found, err := reader.ReadObject(headSHA)
	require.NoError(t, err)
	require.True(t, found)

	reader.mu.Lock()
	reader.stdout = bufio.NewReader(erroringReader{})
	reader.mu.Unlock()

	_, _, err = reader.ReadObject(headSHA)
	require.Error(t, err, "ReadObject must return an error when stdout is broken")
	require.Nil(t, reader.cmd, "r.cmd must be nil after stdout read failure")

	// Recovery: the next call restarts cleanly.
	content2, found2, err := reader.ReadObject(headSHA)
	require.NoError(t, err, "ReadObject must recover after clearing the dead process")
	require.True(t, found2)
	require.NotEmpty(t, content2)
}

// TestReadObjectsBatch_ClearsCmdOnReadError verifies that a broken stdout
// stream also clears r.cmd so subsequent calls recover correctly.
func TestReadObjectsBatch_ClearsCmdOnReadError(t *testing.T) {
	t.Parallel()

	dir := initTestRepo(t)

	// Get a real commit SHA for use in assertions.
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	require.NoError(t, err)
	headSHA := strings.TrimSpace(string(out))

	reader := newObjectReader(func() (string, error) { return dir, nil })

	// Warm up.
	results, err := reader.ReadObjectsBatch([]string{headSHA})
	require.NoError(t, err)
	require.Contains(t, results, headSHA)

	// Kill the process so that reading stdout returns an error (EOF or broken pipe).
	_ = reader.cmd.Process.Kill()
	_ = reader.cmd.Wait()

	// The batch call may partially write refs before the read fails; either
	// path (write failure or read failure) must clear r.cmd.
	//nolint:errcheck // result intentionally discarded — we only care that cmd is cleared
	reader.ReadObjectsBatch([]string{headSHA, headSHA})
	require.Nil(t, reader.cmd, "r.cmd must be nil after stdout read failure")

	// Recovery: the next call restarts cleanly.
	results2, err := reader.ReadObjectsBatch([]string{headSHA})
	require.NoError(t, err, "ReadObjectsBatch must recover after clearing the dead process")
	require.Contains(t, results2, headSHA)
}
