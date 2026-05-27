package worktree

import (
	"context"
	"fmt"
	"strings"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/config"
	"github.com/getstackit/stackit/internal/hooks"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/tui"
)

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
	var hookCmds []string
	for _, hook := range projectCfg.Hooks.PostWorktreeCreate {
		if trimmed := strings.TrimSpace(hook); trimmed != "" {
			hookCmds = append(hookCmds, hook)
		}
	}
	if len(hookCmds) == 0 {
		return nil, nil
	}

	// Load repo config for approvals
	repoCfg, err := config.LoadConfig(ctx.RepoRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to load repo config: %w", err)
	}

	// For each hook, check approval or prompt
	approved := make([]string, 0, len(hookCmds))

	for _, hook := range hookCmds {
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
func RunResolvedHooks(hookCmds []string, worktreePath string, out output.Output) {
	for _, hook := range hookCmds {
		out.Info("Running hook: %s", hook)
		if err := hooks.Run(context.Background(), hook, worktreePath, nil, hooks.DefaultTimeout); err != nil {
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
