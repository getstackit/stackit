// Package doctor provides diagnostic functionality for checking stackit environment and repository health.
package doctor

import (
	"context"
	"fmt"

	"github.com/getstackit/stackit/internal/github"
)

// Options contains options for the doctor command
type Options struct {
	Fix   bool
	Trunk string // Trunk branch name from config
}

// Engine lists exactly the engine methods the doctor checks call, grouped by
// the check category that needs them.
type Engine interface {
	gitVersioner
	repositoryEngine
	stackStateEngine
}

// Deps are the collaborators the doctor checks need. Adapters build them from
// their own runtime state; the action never sees bootstrap types.
type Deps struct {
	Engine   Engine
	GHRunner github.GitCommandRunner
	RepoRoot string
}

// Action runs diagnostic checks on the stackit environment and repository
func Action(ctx context.Context, deps Deps, opts Options, handler Handler) error {
	eng := deps.Engine

	// Use null handler if none provided
	if handler == nil {
		handler = &NullHandler{}
	}
	defer handler.Cleanup()

	handler.Start(opts.Fix)

	var warningCount, errorCount int

	// Environment checks
	handler.OnCategory(CategoryEnvironment)
	warningCount, errorCount = checkEnvironment(ctx, eng, deps.GHRunner, handler, warningCount, errorCount)

	// Repository checks
	handler.OnCategory(CategoryRepository)
	warningCount, errorCount = checkRepository(ctx, eng, deps.RepoRoot, handler, warningCount, errorCount, opts.Trunk)

	// Stack state checks
	handler.OnCategory(CategoryStackState)
	warningCount, errorCount = checkStackState(ctx, eng, handler, warningCount, errorCount, opts.Fix)

	// Calculate passed count (we don't track individual checks, so estimate)
	passedCount := 0 // Will be counted by handlers that track it

	handler.Complete(passedCount, warningCount, errorCount)

	if errorCount > 0 {
		return fmt.Errorf("doctor found %d error(s)", errorCount)
	}

	return nil
}
