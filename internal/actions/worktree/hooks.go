package worktree

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/config"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/tui"
)

// HookTimeout is the maximum duration a hook can run before being killed
const HookTimeout = 60 * time.Second

// ResolveApprovedHooks loads the project config, checks for approvals, and prompts
// the user for any unapproved hooks. Returns the list of approved hook commands.
// This must be called from the main thread (it may prompt interactively).
func ResolveApprovedHooks(ctx *app.Context) ([]string, error) {
	out := ctx.Output

	// Load project config from repo root
	projectCfg, err := config.LoadProjectConfig(ctx.RepoRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to load project config: %w", err)
	}

	// Get hooks to run, filtering out empty/whitespace-only entries
	var hooks []string
	for _, hook := range projectCfg.Hooks.PostWorktreeCreate {
		if trimmed := strings.TrimSpace(hook); trimmed != "" {
			hooks = append(hooks, hook)
		}
	}
	if len(hooks) == 0 {
		return nil, nil
	}

	// Load repo config for approvals
	repoCfg, err := config.LoadConfig(ctx.RepoRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to load repo config: %w", err)
	}

	// For each hook, check approval or prompt
	approved := make([]string, 0, len(hooks))

	for _, hook := range hooks {
		if repoCfg.IsPostWorktreeCreateHookApproved(hook) {
			approved = append(approved, hook)
			continue
		}

		// Prompt user (default No for security)
		msg := fmt.Sprintf("This repo wants to run %q after creating worktrees. Allow?", hook)
		allow, promptErr := tui.PromptConfirm(msg, false)
		if promptErr != nil {
			out.Info("Skipping hook (prompt failed): %s", hook)
			continue
		}
		if !allow {
			out.Info("Skipping hook: %s", hook)
			continue
		}

		// Save approval (writes immediately to git config)
		if err := repoCfg.AddApprovedPostWorktreeCreateHook(hook); err != nil {
			out.Warn("Failed to save hook approval: %v", err)
		}
		approved = append(approved, hook)
	}

	return approved, nil
}

// RunResolvedHooks executes a pre-resolved list of hooks in the given directory.
// No prompting is performed — safe for parallel use.
func RunResolvedHooks(hooks []string, worktreePath string, out output.Output) {
	for _, hook := range hooks {
		out.Info("Running hook: %s", hook)
		if err := runHookWithTimeout(hook, worktreePath, HookTimeout); err != nil {
			out.Warn("Hook failed: %s: %v", hook, err)
		}
	}
}

// RunPostCreateHooks runs any configured post-worktree-create hooks.
// It loads the project config, checks for approvals, prompts for unapproved hooks,
// and executes approved hooks in the worktree directory.
func RunPostCreateHooks(ctx *app.Context, worktreePath string) error {
	approved, err := ResolveApprovedHooks(ctx)
	if err != nil {
		return err
	}

	RunResolvedHooks(approved, worktreePath, ctx.Output)
	return nil
}

// runHookWithTimeout executes a hook command with a timeout.
// Returns an error if the command fails or times out.
func runHookWithTimeout(hook string, dir string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", hook)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("timed out after %s", timeout)
	}
	return err
}
