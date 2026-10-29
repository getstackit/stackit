package sync

import (
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/app/actiondeps"
)

// DepsFrom builds sync's collaborators from a test's runtime context. Call it
// after the test has installed its GitHub client, runner and config so the
// action sees them.
func DepsFrom(ctx *app.Context) Deps {
	return Deps{
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
