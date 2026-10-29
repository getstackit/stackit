package submit

import (
	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/app"
)

// DepsFrom builds submit's collaborators from a test's runtime context. Call
// it after the test has installed its GitHub client, runner and config so the
// action sees them.
func DepsFrom(ctx *app.Context) Deps {
	return Deps{
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
