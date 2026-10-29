package common

import (
	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/actions/merge"
	"github.com/getstackit/stackit/internal/app"
)

// MergeDeps builds merge's collaborators from the command's runtime context,
// including the sync, submit and checkout deps its post-merge follow-ups run
// with. Build it after the context's GitHub client, runner and config are in
// place: the deps capture them.
func MergeDeps(ctx *app.Context) merge.Deps {
	return merge.Deps{
		Engine:      ctx.Engine,
		Out:         ctx.Output,
		GitHub:      ctx.GitHub,
		GitHubError: ctx.GitHubError,
		GHRunner:    ctx.GHRunner,
		PRSync:      actions.PRSyncFrom(ctx),
		Prompts:     ctx.Prompts(),
		RepoRoot:    ctx.RepoRoot,
		Config:      ctx.Config,
		Sync:        SyncDeps(ctx),
		Submit:      SubmitDeps(ctx),
		Checkout:    CheckoutDeps(ctx),
	}
}
