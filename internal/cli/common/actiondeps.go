package common

import (
	"github.com/getstackit/stackit/internal/actions/checkout"
	"github.com/getstackit/stackit/internal/actions/merge"
	"github.com/getstackit/stackit/internal/actions/submit"
	"github.com/getstackit/stackit/internal/actions/sync"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/app/actiondeps"
)

// The builders below assemble the deps of commands that delegate to other
// commands. They live here rather than in actiondeps so the commands' own
// internal tests can build deps without an import cycle.

// CheckoutDeps builds checkout's deps.
func CheckoutDeps(ctx *app.Context) checkout.Deps {
	return checkout.Deps{
		Engine: ctx.Engine,
		Out:    ctx.Output,
		Log:    ctx.Logger,
		Quiet:  ctx.Quiet,
		Here:   actiondeps.Here(ctx),
	}
}

// SubmitDeps builds submit's deps.
func SubmitDeps(ctx *app.Context) submit.Deps {
	return submit.Deps{
		Engine:      ctx.Engine,
		Out:         ctx.Output,
		Log:         ctx.Logger,
		GitHub:      ctx.GitHub,
		GitHubError: ctx.GitHubError,
		GHRunner:    ctx.GHRunner,
		PRSync:      actiondeps.PRSync(ctx),
		Restack:     actiondeps.Restack(ctx),
		Snapshots:   actiondeps.Snapshots(ctx),
		Prompts:     ctx.Prompts(),
		Verify:      ctx.Verify,
		Config:      ctx.Config,
	}
}

// SyncDeps builds sync's deps.
func SyncDeps(ctx *app.Context) sync.Deps {
	return sync.Deps{
		Engine:    ctx.Engine,
		Out:       ctx.Output,
		Log:       ctx.Logger,
		GitHub:    ctx.GitHub,
		GHRunner:  ctx.GHRunner,
		PRSync:    actiondeps.PRSync(ctx),
		Restack:   actiondeps.Restack(ctx),
		Cleanup:   actiondeps.CleanBranches(ctx),
		Snapshots: actiondeps.Snapshots(ctx),
		Here:      actiondeps.Here(ctx),
		Config:    ctx.Config,
	}
}

// MergeDeps builds merge's deps, including the sync, submit and checkout deps its
// post-merge follow-ups run with.
func MergeDeps(ctx *app.Context) merge.Deps {
	return merge.Deps{
		Engine:      ctx.Engine,
		Out:         ctx.Output,
		GitHub:      ctx.GitHub,
		GitHubError: ctx.GitHubError,
		GHRunner:    ctx.GHRunner,
		PRSync:      actiondeps.PRSync(ctx),
		Prompts:     ctx.Prompts(),
		RepoRoot:    ctx.RepoRoot,
		Config:      ctx.Config,
		Sync:        SyncDeps(ctx),
		Submit:      SubmitDeps(ctx),
		Checkout:    CheckoutDeps(ctx),
	}
}
