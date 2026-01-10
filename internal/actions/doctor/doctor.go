// Package doctor provides diagnostic functionality for checking stackit environment and repository health.
package doctor

import (
	"fmt"

	"stackit.dev/stackit/internal/app"
)

// Options contains options for the doctor command
type Options struct {
	Fix   bool
	Trunk string // Trunk branch name from config
}

// Action runs diagnostic checks on the stackit environment and repository
func Action(ctx *app.Context, opts Options, handler Handler) error {
	eng := ctx.Engine

	// Use null handler if none provided
	if handler == nil {
		handler = &NullHandler{}
	}
	defer handler.Cleanup()

	handler.Start(opts.Fix)

	var warningCount, errorCount int

	// Environment checks
	handler.OnCategory(CategoryEnvironment)
	warningCount, errorCount = checkEnvironment(ctx.Git(), handler, warningCount, errorCount)

	// Repository checks
	handler.OnCategory(CategoryRepository)
	warningCount, errorCount = checkRepository(ctx, handler, warningCount, errorCount, opts.Trunk)

	// Stack state checks
	handler.OnCategory(CategoryStackState)
	warningCount, errorCount = checkStackState(ctx.Context, eng, handler, warningCount, errorCount, opts.Fix)

	// Calculate passed count (we don't track individual checks, so estimate)
	passedCount := 0 // Will be counted by handlers that track it

	handler.Complete(passedCount, warningCount, errorCount)

	if errorCount > 0 {
		return fmt.Errorf("doctor found %d error(s)", errorCount)
	}

	return nil
}
