//go:build unix

package git

import (
	"bytes"
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestCommandContextWaitDelayUnblocksPipeHoldingChild reproduces the deadlock
// behind issue #1330 at the exec layer and proves that cmd.WaitDelay is what
// breaks it.
//
// The runner captures a subprocess's stdout/stderr into buffers, so Go wires
// them through os.Pipe and copies in a background goroutine. A `git fetch`
// spawns an `ssh` grandchild that inherits the pipe's write end. When the
// context deadline fires, exec kills the direct child (git/sh) — but the
// grandchild survives and keeps the pipe open, so cmd.Run() blocks on the
// copy goroutine forever even though the command "timed out". WaitDelay makes
// Go force-close the pipe shortly after the kill, guaranteeing Run() returns.
//
// The shim `sleep <n> & sleep <n>` models this exactly: the backgrounded sleep
// inherits stdout and outlives the foreground process that exec actually kills.
func TestCommandContextWaitDelayUnblocksPipeHoldingChild(t *testing.T) {
	t.Parallel()

	// Short enough to keep the test fast; long enough to outlive the deadline
	// and the assertion windows below, so the negative control is observably
	// blocked before the orphaned child exits and cleans everything up.
	const shim = "sleep 2 & sleep 2"

	run := func(t *testing.T, waitDelay time.Duration) (chan error, time.Time) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		t.Cleanup(cancel)

		cmd := exec.CommandContext(ctx, "/bin/sh", "-c", shim)
		cmd.WaitDelay = waitDelay
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out

		done := make(chan error, 1)
		start := time.Now()
		go func() { done <- cmd.Run() }()
		return done, start
	}

	t.Run("with WaitDelay Run returns shortly after the deadline", func(t *testing.T) {
		t.Parallel()
		done, start := run(t, 300*time.Millisecond)

		select {
		case <-done:
			require.Less(t, time.Since(start), 1500*time.Millisecond,
				"Run should return shortly after deadline+WaitDelay, not wait for the pipe-holding child")
		case <-time.After(5 * time.Second):
			t.Fatal("cmd.Run() hung despite WaitDelay — the issue #1330 deadlock is not fixed")
		}
	})

	t.Run("without WaitDelay Run stays blocked on the pipe-holding child", func(t *testing.T) {
		t.Parallel()
		done, _ := run(t, 0)

		select {
		case <-done:
			t.Fatal("expected Run() to stay blocked past the deadline (child holds the pipe) without WaitDelay")
		case <-time.After(1 * time.Second):
			// Expected: still blocked well past the 200ms deadline. Drain the
			// channel once the orphaned child exits (~2s) so nothing leaks.
		}
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("orphaned child never released the pipe")
		}
	})
}
