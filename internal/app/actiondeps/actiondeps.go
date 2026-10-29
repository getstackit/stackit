// Package actiondeps builds the collaborators each action takes from the
// CLI's runtime context. Actions never see *app.Context; adapters call these
// builders at the boundary.
//
// Build deps after the context's GitHub client, runner and config are in
// place: the deps capture them.
package actiondeps

import (
	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/actions/restack"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/pr"
)

// Here reads where the command runs.
func Here(ctx *app.Context) actions.Here {
	return actions.Here{InManagedWorktree: ctx.InManagedWorktree, Worktree: ctx.WorktreeInfo}
}

// Snapshots builds the undo snapshot recorder, honoring undo.enabled.
func Snapshots(ctx *app.Context) actions.Snapshots {
	return actions.Snapshots{Engine: ctx.Engine, Enabled: ctx.Config == nil || ctx.Config.UndoEnabled(), Log: ctx.Output}
}

// Restack builds the restack command pipeline's deps.
func Restack(ctx *app.Context) actions.RestackDeps {
	return actions.RestackDeps{
		Engine:      ctx.Engine,
		Out:         ctx.Output,
		Log:         ctx.Logger,
		RepoRoot:    ctx.RepoRoot,
		Snapshots:   Snapshots(ctx),
		Interactive: ctx.Interactive,
		Quiet:       ctx.Quiet,
	}
}

// RestackRunner builds a restack runner that prints per-branch results.
func RestackRunner(ctx *app.Context) restack.Runner {
	return actions.NewRestackRunner(Restack(ctx))
}

// Mutation builds the collaborators every branch-rewriting action shares.
func Mutation(ctx *app.Context) actions.MutationDeps {
	return actions.MutationDeps{
		Out:       ctx.Output,
		Here:      Here(ctx),
		Snapshots: Snapshots(ctx),
		Restack:   RestackRunner(ctx),
	}
}

// CleanBranches builds branch cleanup's deps.
func CleanBranches(ctx *app.Context) actions.CleanBranchesDeps {
	return actions.CleanBranchesDeps{Engine: ctx.Engine, Out: ctx.Output, Log: ctx.Logger}
}

// PRNavigation resolves the stack navigation settings from config, falling
// back to defaults when no config is loaded.
func PRNavigation(ctx *app.Context) pr.NavigationOptions {
	navOpts := pr.DefaultNavigationOptions()
	if ctx.Config != nil {
		navOpts.When = ctx.Config.NavigationWhen()
		navOpts.Marker = ctx.Config.NavigationMarker()
		navOpts.Location = ctx.Config.NavigationLocation()
		navOpts.ShowMerged = ctx.Config.NavigationShowMerged()
	}
	return navOpts
}

// PRSync builds the PR metadata syncer. The GitHub client stays lazy.
func PRSync(ctx *app.Context) actions.PRSync {
	return actions.PRSync{Engine: ctx.Engine, GitHub: ctx.GitHub, GHRunner: ctx.GHRunner, Nav: PRNavigation(ctx), Log: ctx.Output}
}

// Continue builds stackit continue's deps.
func Continue(ctx *app.Context) actions.ContinueDeps {
	return actions.ContinueDeps{
		Engine:    ctx.Engine,
		Out:       ctx.Output,
		RepoRoot:  ctx.RepoRoot,
		Restack:   RestackRunner(ctx),
		Conflicts: &restack.ConflictReporter{Engine: ctx.Engine, Out: ctx.Output, RepoRoot: ctx.RepoRoot},
	}
}
