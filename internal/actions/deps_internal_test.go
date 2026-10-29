package actions

import (
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
)

// Internal tests cannot import internal/app/actiondeps (it imports this
// package), so they build deps from a scenario's context here.

func restackDepsFrom(ctx *app.Context) RestackDeps {
	return RestackDeps{
		Engine:      ctx.Engine,
		Out:         ctx.Output,
		Log:         ctx.Logger,
		RepoRoot:    ctx.RepoRoot,
		Snapshots:   Snapshots{Engine: ctx.Engine, Enabled: ctx.Config == nil || ctx.Config.UndoEnabled(), Log: ctx.Output},
		Interactive: ctx.Interactive,
		Quiet:       ctx.Quiet,
	}
}

func ensureCanModifyHere(ctx *app.Context, branches ...engine.Branch) error {
	return EnsureCanModify(ctx.Context, ctx.Engine, Here{InManagedWorktree: ctx.InManagedWorktree, Worktree: ctx.WorktreeInfo}, branches...)
}
