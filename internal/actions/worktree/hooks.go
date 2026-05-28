package worktree

import (
	"context"
	"fmt"

	"github.com/getstackit/stackit/internal/actions/hooks"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/config"
	"github.com/getstackit/stackit/internal/output"
)

// ResolveApprovedHooks loads the project config and returns the approved
// post-worktree-create hook commands, prompting for any unapproved entries.
// Must be called from the main thread (may prompt interactively).
func ResolveApprovedHooks(ctx *app.Context) ([]string, error) {
	projectCfg, err := config.LoadProjectConfig(ctx.RepoRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to load project config: %w", err)
	}
	return hooks.ResolveApproved(ctx, config.PhasePostWorktreeCreate, projectCfg.Hooks.PostWorktreeCreate)
}

// RunResolvedHooks executes a pre-resolved list of hooks in the given directory.
// Failures are warned, not blocking — matches the existing worktree behavior.
func RunResolvedHooks(hookCmds []string, worktreePath string, out output.Output) {
	_ = hooks.Run(context.Background(), hookCmds, out, hooks.RunOptions{
		Dir:      worktreePath,
		Blocking: false,
	})
}

// RunPostCreateHooks runs any configured post-worktree-create hooks. It loads
// the project config, resolves approvals, and executes approved hooks in the
// worktree directory.
func RunPostCreateHooks(ctx *app.Context, worktreePath string) error {
	approved, err := ResolveApprovedHooks(ctx)
	if err != nil {
		return err
	}
	RunResolvedHooks(approved, worktreePath, ctx.Output)
	return nil
}
