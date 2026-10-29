package merge

import (
	"github.com/getstackit/stackit/internal/actions/checkout"
	"github.com/getstackit/stackit/internal/actions/submit"
	"github.com/getstackit/stackit/internal/actions/sync"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/app/actiondeps"
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
		PRSync:      actiondeps.PRSync(ctx),
		Prompts:     ctx.Prompts(),
		RepoRoot:    ctx.RepoRoot,
		Config:      ctx.Config,
		Sync: sync.Deps{
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
		},
		Submit: submit.Deps{
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
		},
		Checkout: checkout.Deps{
			Engine: ctx.Engine,
			Out:    ctx.Output,
			Log:    ctx.Logger,
			Quiet:  ctx.Quiet,
			Here:   actiondeps.Here(ctx),
		},
	}
}
