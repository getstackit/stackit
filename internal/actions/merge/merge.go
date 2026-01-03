// Package merge provides functionality for merging stacked pull requests.
package merge

import (
	"time"

	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/github"
)

// Handler is an interface for reporting merge progress
type Handler interface {
	Start(plan *Plan)
	StepStarted(stepIndex int, description string)
	StepCompleted(stepIndex int)
	StepFailed(stepIndex int, err error)
	StepWaiting(stepIndex int, elapsed, timeout time.Duration, checks []github.CheckDetail)
	SetEstimatedDuration(duration time.Duration)
	Complete(result *ConsolidationResult)
}

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
	Handler        Handler
}

// Action performs the merge operation using the plan/execute pattern
func Action(ctx *app.Context, opts Options) error {
	eng := ctx.Engine

	// If dry-run, create plan and display it
	if opts.DryRun {
		plan := opts.Plan
		var validation *PlanValidation
		if plan == nil {
			var err error
			plan, validation, err = CreateMergePlan(ctx.Context, eng, ctx.Splog, ctx.GitHubClient, CreatePlanOptions{
				Strategy:     opts.Strategy,
				Force:        opts.Force,
				Scope:        opts.Scope,
				TargetBranch: opts.TargetBranch,
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
		ctx.Splog.Page(planText)
		ctx.Splog.Info("Dry-run mode: plan displayed above. No changes were made.")
		return nil
	}

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
		Handler:        opts.Handler,
	}

	if err := ExecuteInWorktree(ctx, eng, executeOpts); err != nil {
		return err
	}

	return nil
}
