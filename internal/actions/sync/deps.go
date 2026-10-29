package sync

import (
	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/github"
	"github.com/getstackit/stackit/internal/output"
)

// Config lists the configuration getters sync reads. A nil Config behaves as
// if no configuration was loaded.
type Config interface {
	WorktreeAutoClean() bool
}

// Deps are the collaborators sync needs. Adapters build them from their own
// runtime state; the action never sees bootstrap types.
type Deps struct {
	// Engine is the full engine: sync reconciles the whole repository —
	// trunk, remote and stack metadata, PR info, worktrees, branch cleanup —
	// so the surface it touches spans nearly every engine role, and the
	// restack and cleanup pipelines it delegates to take engine.Engine too.
	Engine engine.Engine
	Out    output.Output
	Log    output.Logger
	// GitHub returns the client, or nil when GitHub is unavailable. It is a
	// function so the client is only built when sync actually needs it.
	GitHub func() github.Client
	// GHRunner runs the GraphQL queries that fetch PR info.
	GHRunner github.GitCommandRunner
	// PRSync refreshes PR footers and titles for flagged branches and pushes
	// orphaned metadata the user chose to keep.
	PRSync actions.PRSync
	// Restack is what the restack phase runs actions.RunRestack with. Its
	// Interactive and Quiet flags also decide whether sync may prompt to
	// enable rerere.
	Restack actions.RestackDeps
	// Cleanup is what the merged/closed branch cleanup phase runs with.
	Cleanup   actions.CleanBranchesDeps
	Snapshots actions.Snapshots
	// Here is where sync runs; a managed worktree never removes itself and
	// cannot delete branches checked out elsewhere.
	Here   actions.Here
	Config Config
}
