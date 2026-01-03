// Package merge provides functionality for merging stacked pull requests.
package merge

import (
	"stackit.dev/stackit/internal/app"
)

// Options contains options for the merge command
type Options struct {
	DryRun         bool
	Confirm        bool
	Strategy       Strategy
	Force          bool
	Scope          string
	TargetBranch   string
	Plan           *Plan // Optional pre-calculated plan
	UndoStackDepth int   // Maximum undo stack depth (from config)
}

// Action performs the merge operation using the plan/execute pattern
func Action(ctx *app.Context, opts Options) error {
	eng := ctx.Engine

	// 1. Prepare execute options
	// Most logic (planning, sync checks, etc.) is now deferred to ExecuteInWorktree
	// to ensure it happens in isolation.
	executeOpts := ExecuteOptions{
		Plan:           opts.Plan,
		Strategy:       opts.Strategy,
		Force:          opts.Force,
		DryRun:         opts.DryRun,
		Confirm:        opts.Confirm,
		Scope:          opts.Scope,
		TargetBranch:   opts.TargetBranch,
		UndoStackDepth: opts.UndoStackDepth,
	}

	if err := ExecuteInWorktree(ctx, eng, executeOpts); err != nil {
		return err
	}

	return nil
}
