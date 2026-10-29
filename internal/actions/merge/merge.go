// Package merge provides functionality for merging stacked pull requests.
package merge

import (
	"context"

	"github.com/getstackit/stackit/internal/github"
)

// Options contains options for the merge command
type Options struct {
	DryRun         bool
	Confirm        bool
	Strategy       Strategy
	Force          bool
	Wait           bool // Whether to wait for CI/merge (applies to consolidate)
	Scope          string
	TargetBranch   string
	Plan           *Plan // Optional pre-calculated plan
	UndoStackDepth int   // Maximum undo stack depth (from config)
	LinearStacks   bool  // stack.shape == linear (from config)
	Handler        EventHandler
	MergeMethod    github.MergeMethod // Optional: override merge method (empty = auto-detect/prompt)
}

// Action performs the merge operation using the plan/execute pattern
func Action(ctx context.Context, deps Deps, opts Options) error {
	eng := deps.Engine

	// If dry-run, create plan and display it
	if opts.DryRun {
		plan := opts.Plan
		var validation *PlanValidation
		if plan == nil {
			var err error
			plan, validation, err = CreateMergePlan(ctx, eng, deps.Out, deps.GitHub(), CreatePlanOptions{
				Strategy:     opts.Strategy,
				Force:        opts.Force,
				Scope:        opts.Scope,
				TargetBranch: opts.TargetBranch,
				Wait:         opts.Wait,
			})
			if err != nil {
				return err
			}
		} else {
			// Validate existing plan
			// For simplicity, we create a new validation or reuse plan's validation if we had one
			validation = &PlanValidation{Valid: true}
		}

		planText := FormatMergePlan(plan, validation)
		deps.Out.Print(planText)
		deps.Out.Info("Dry-run mode: plan displayed above. No changes were made.")
		return nil
	}

	// 1. Prepare execute options
	// Most logic (planning, sync checks, etc.) is now deferred to ExecuteInWorktree
	// to ensure it happens in isolation.
	executeOpts := ExecuteOptions{
		Plan:           opts.Plan,
		Strategy:       opts.Strategy,
		Force:          opts.Force,
		Wait:           opts.Wait,
		UndoStackDepth: opts.UndoStackDepth,
		LinearStacks:   opts.LinearStacks,
		Handler:        opts.Handler,
		MergeMethod:    opts.MergeMethod,
	}

	// If no target branch or scope specified, use current branch
	targetBranch := opts.TargetBranch
	if targetBranch == "" && opts.Scope == "" && opts.Plan == nil {
		cb := eng.CurrentBranch(ctx)
		if cb != nil {
			targetBranch = cb.Name()
		}
	}

	if err := ExecuteInWorktree(ctx, deps, executeOpts, opts.Scope, targetBranch); err != nil {
		return err
	}

	return nil
}
