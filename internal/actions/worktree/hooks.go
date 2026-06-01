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
func RunResolvedHooks(hookCmds []string, worktreePath string, out output.Output) {
	_ = hooks.Run(context.Background(), hookCmds, hooks.RunOptions{
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
	RunResolvedHooks(approved, worktreePath, ctx.Output)
	return nil
}

// worktreePrompter wraps tui.PromptConfirm with a default-no answer so
// accidental Enter doesn't approve arbitrary post-worktree-create commands.
type worktreePrompter struct{}

func (worktreePrompter) PromptConfirm(message string) (bool, error) {
	return tui.PromptConfirm(message, false)
}

var _ handler.PromptHandler = worktreePrompter{}
