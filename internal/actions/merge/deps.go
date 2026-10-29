package merge

import (
	"errors"
	"fmt"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/actions/checkout"
	"github.com/getstackit/stackit/internal/actions/handler"
	"github.com/getstackit/stackit/internal/actions/submit"
	"github.com/getstackit/stackit/internal/actions/sync"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/github"
	"github.com/getstackit/stackit/internal/output"
)

// Config lists the configuration getters (and the one setter) merge uses: the
// repository merge method, read and persisted on first use, and the local CI
// command multi-stack shipping validates with. A nil Config behaves as if no
// configuration was loaded.
type Config interface {
	MergeMethodConfig
	CIConfig
}

// Deps are the collaborators merge needs. Adapters build them from their own
// runtime state; the action never sees bootstrap types.
type Deps struct {
	// Engine is the full engine: merge plans, executes and cleans up across
	// the whole stack graph — locking, PR info, restacks, worktrees, pushes —
	// and hands the engine to the multi-stack worktree executor and stack
	// discovery, which take engine.Engine too. A merge in a temporary
	// worktree swaps in an engine rooted there.
	Engine engine.Engine
	Out    output.Output
	// GitHub returns the client, or nil when GitHub is unavailable. It is a
	// function so the client is only built when merge actually needs it.
	GitHub func() github.Client
	// GitHubError explains why GitHub returned nil, when known. May be nil.
	GitHubError func() error
	// GHRunner runs the GraphQL queries merge and automerge go through.
	GHRunner github.GitCommandRunner
	// PRSync pushes metadata and refreshes the PRs of locked or closed
	// branches.
	PRSync  actions.PRSync
	Prompts handler.Prompter
	// RepoRoot is where restack continuation state is persisted.
	RepoRoot string
	Config   Config

	// Sync is what the post-merge sync runs with.
	Sync sync.Deps
	// Submit is what drain publishes restacked branches with.
	Submit submit.Deps
	// Checkout is what the post-merge trunk checkout runs with.
	Checkout checkout.Deps
}

// requireGitHub returns an error explaining why the GitHub client is
// unavailable, or nil when there is one.
func (deps Deps) requireGitHub() error {
	if deps.GitHub() != nil {
		return nil
	}
	if deps.GitHubError != nil {
		if err := deps.GitHubError(); err != nil {
			return fmt.Errorf("GitHub client not available: %w", err)
		}
	}
	return errors.New("GitHub client not available — check your GITHUB_TOKEN or run 'gh auth login'")
}
