package common

import (
	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/actions/sync"
	"github.com/getstackit/stackit/internal/app"
)

// SyncDeps builds sync's collaborators from the command's runtime context.
// Build it after the context's GitHub client, runner and config are in place:
// the deps capture them.
func SyncDeps(ctx *app.Context) sync.Deps {
	return sync.Deps{
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
	}
}
