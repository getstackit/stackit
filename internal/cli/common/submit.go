package common

import (
	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/actions/submit"
	"github.com/getstackit/stackit/internal/app"
)

// SubmitDeps builds submit's collaborators from the command's runtime context.
// Build it after the context's GitHub client, runner and config are in place:
// the deps capture them.
func SubmitDeps(ctx *app.Context) submit.Deps {
	return submit.Deps{
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
	}
}
