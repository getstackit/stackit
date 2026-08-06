package worktree

import (
	"context"

	"github.com/getstackit/stackit/internal/actions/handler"
	"github.com/getstackit/stackit/internal/actions/hooks"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/config"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/tui"
)

// ResolveApprovedHooks reads the post-worktree-create hook list from the
// project config already attached to ctx and returns the approved subset,
// prompting for any unapproved entries. Must be called from the main thread
// (may prompt interactively).
func ResolveApprovedHooks(ctx *app.Context) ([]string, error) {
	if ctx.Config == nil {
		return nil, nil
	}
	projectCfg := ctx.Config.ProjectConfig()
	if projectCfg == nil {
		return nil, nil
	}
	return hooks.ResolveApproved(hooks.ResolveRequest{
		Phase:    config.PhasePostWorktreeCreate,
		Commands: projectCfg.Hooks.For(config.PhasePostWorktreeCreate),
		Config:   ctx.Config,
		Prompter: worktreePrompter{},
		Output:   ctx.Output,
	})
}

// RunResolvedHooks executes a pre-resolved list of hooks in the given directory.
// Failures are warned, not blocking — matches the existing worktree behavior.
//
// The caller's context is honored so Ctrl-C interrupts a hook instead of
// waiting out its timeout.
// An empty worktreePath is refused rather than passed through: os/exec treats
// an empty Dir as the current directory, which for these hooks is the user's
// main checkout — the one place they must never run.
func RunResolvedHooks(ctx context.Context, hookCmds []string, worktreePath string, out output.Output) {
	if worktreePath == "" {
		if len(hookCmds) > 0 {
			out.Warn("Skipped post-worktree-create hooks: no worktree directory to run them in")
		}
		return
	}
	_ = hooks.Run(ctx, hookCmds, hooks.RunOptions{
		Dir:      worktreePath,
		Blocking: false,
		Output:   out,
	})
}

// RunPostCreateHooks runs any configured post-worktree-create hooks. It reads
// the project config from ctx, resolves approvals, and executes approved
// hooks in the worktree directory.
func RunPostCreateHooks(ctx *app.Context, worktreePath string) error {
	approved, err := ResolveApprovedHooks(ctx)
	if err != nil {
		return err
	}
	RunResolvedHooks(ctx.Context, approved, worktreePath, ctx.Output)
	return nil
}

// worktreePrompter wraps tui.PromptConfirm with a default-no answer so
// accidental Enter doesn't approve arbitrary post-worktree-create commands.
type worktreePrompter struct{}

func (worktreePrompter) PromptConfirm(message string) (bool, error) {
	return tui.PromptConfirm(message, false)
}

var _ handler.PromptHandler = worktreePrompter{}
