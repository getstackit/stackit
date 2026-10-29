package submit

import (
	"context"
	"errors"
	"fmt"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/actions/handler"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/github"
	"github.com/getstackit/stackit/internal/output"
)

// Engine lists exactly the engine methods submit calls beyond the read-only
// branch surface: tracking an untracked target, the managed-worktree lookup
// for display, the pending-submit marker, branch and metadata pushes, and the
// PR-info writes.
type Engine interface {
	engine.BranchReader
	actions.MetadataPushEngine
	TrackBranch(ctx context.Context, branchName string, parentBranchName string) error
	ListManagedWorktrees(ctx context.Context) ([]engine.WorktreeInfo, error)
	GetStackRootForBranch(branch engine.Branch) string
	MarkBranchesSubmitPending(ctx context.Context, branchNames []string) error
	ClearBranchesSubmitPending(ctx context.Context, branchNames []string) error
	PushBranches(ctx context.Context, remote string, specs []git.PushSpec, opts git.PushOptions) git.PushResults
	UpsertPrInfo(ctx context.Context, branch engine.Branch, prInfo *engine.PrInfo) error
	BatchUpsertPrInfo(ctx context.Context, updates map[string]*engine.PrInfo) error
	GetStackIDsForBranches(branches engine.Branches) []string
	PushStackMetadata(ctx context.Context, stackIDs []string) error
}

// Config lists the configuration getters submit reads. A nil Config behaves
// as if no configuration was loaded.
type Config interface {
	GitHubStack() bool
	LinearStacks() bool
}

// Deps are the collaborators submit needs. Adapters build them from their own
// runtime state; the action never sees bootstrap types.
type Deps struct {
	Engine Engine
	Out    output.Output
	Log    output.Logger
	// GitHub returns the client, or nil when GitHub is unavailable. It is a
	// function so the client is only built when submit actually needs it.
	GitHub func() github.Client
	// GitHubError explains why GitHub returned nil, when known. May be nil.
	GitHubError func() error
	// GHRunner runs the GraphQL queries that sync PR info before validation.
	GHRunner github.GitCommandRunner
	// PRSync refreshes PR footers and titles after the submission phase.
	PRSync actions.PRSync
	// Restack is what --restack runs actions.RunRestack with.
	Restack   actions.RestackDeps
	Snapshots actions.Snapshots
	Prompts   handler.Prompter
	// Verify reflects the global --verify flag; false skips push hooks.
	Verify bool
	Config Config
}

// requireGitHub returns the GitHub client or an error explaining why it is
// unavailable.
func (deps Deps) requireGitHub() (github.Client, error) {
	if client := deps.GitHub(); client != nil {
		return client, nil
	}
	if deps.GitHubError != nil {
		if err := deps.GitHubError(); err != nil {
			return nil, fmt.Errorf("GitHub client initialization failed: %w", err)
		}
	}
	return nil, errors.New("no GitHub client available - check your GITHUB_TOKEN")
}
