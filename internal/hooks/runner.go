// Package hooks executes user-defined hook commands with a timeout and
// optional environment-variable injection. It is shared between
// worktree-create hooks and (future) command lifecycle hooks.
package hooks

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// DefaultTimeout is the maximum duration a hook can run before being killed.
const DefaultTimeout = 60 * time.Second

// Run executes a single hook command via `sh -c`.
//
// dir sets the working directory for the spawned process. env, if non-empty,
// is appended to os.Environ() for the process. stdout/stderr are inherited
// from the current process so hook output reaches the user.
func Run(ctx context.Context, command, dir string, env []string, timeout time.Duration) error {
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "sh", "-c", command)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}

	err := cmd.Run()
	if runCtx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("timed out after %s", timeout)
	}
	return err
}
