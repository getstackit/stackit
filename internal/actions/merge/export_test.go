package merge

import (
	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/actions/checkout"
	"github.com/getstackit/stackit/internal/actions/submit"
	"github.com/getstackit/stackit/internal/actions/sync"
	"github.com/getstackit/stackit/internal/app"
)

// DepsFrom builds merge's collaborators from a test's runtime context. Call it
// after the test has installed its GitHub client, runner and config so the
// action sees them.
func DepsFrom(ctx *app.Context) Deps {
	return Deps{
		Engine:      ctx.Engine,
		Out:         ctx.Output,
		GitHub:      ctx.GitHub,
		GitHubError: ctx.GitHubError,
		GHRunner:    ctx.GHRunner,
		PRSync:      actions.PRSyncFrom(ctx),
		Prompts:     ctx.Prompts(),
		RepoRoot:    ctx.RepoRoot,
		Config:      ctx.Config,
		Sync: sync.Deps{
			Engine:    ctx.Engine,
			Out:       ctx.Output,
			Log:       ctx.Logger,
			GitHub:    ctx.GitHub,
			GHRunner:  ctx.GHRunner,
			PRSync:    actions.PRSyncFrom(ctx),
			Restack:   actions.RestackDepsFrom(ctx),
			Cleanup:   actions.CleanBranchesDepsFrom(ctx),
			Snapshots: actions.SnapshotsFrom(ctx),
			Here:      actions.HereFrom(ctx),
			Config:    ctx.Config,
		},
		Submit: submit.Deps{
			Engine:      ctx.Engine,
			Out:         ctx.Output,
			Log:         ctx.Logger,
			GitHub:      ctx.GitHub,
			GitHubError: ctx.GitHubError,
			GHRunner:    ctx.GHRunner,
			PRSync:      actions.PRSyncFrom(ctx),
			Restack:     actions.RestackDepsFrom(ctx),
			Snapshots:   actions.SnapshotsFrom(ctx),
			Prompts:     ctx.Prompts(),
			Verify:      ctx.Verify,
			Config:      ctx.Config,
		},
		Checkout: checkout.Deps{
			Engine: ctx.Engine,
			Out:    ctx.Output,
			Log:    ctx.Logger,
			Quiet:  ctx.Quiet,
			Here:   actions.HereFrom(ctx),
		},
	}
}
