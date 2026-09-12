// Package restack is the core of every command that rebases branches onto
// their parents. It is split in two so each half depends only on what it uses:
//
//   - Planner works out what a restack would do and proves it in a scratch
//     worktree. It reads the repository and never changes it.
//   - Executor applies a Plan: it moves branch refs, or stops at the first
//     conflict and leaves the repository mid-rebase for `stackit continue`.
//
// Neither touches bootstrap state. Adapters build them from whatever runtime
// they have (see actions.RestackDepsFrom for the CLI's).
package restack

import (
	"context"
	"fmt"

	"github.com/getstackit/stackit/internal/engine"
)

// Logger receives diagnostic lines. output.Logger satisfies it.
type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
}

// PlanEngine lists exactly the engine methods planning calls. All of them
// read; validation rebases in a temporary worktree, never in the user's.
type PlanEngine interface {
	BatchRevisions(ctx context.Context, branches engine.Branches) engine.RevisionMap
	GetMergeBase(ctx context.Context, rev1, rev2 string) (string, error)
	PlanRestack(ctx context.Context, branches engine.Branches) (*engine.RestackPlan, error)
	ValidateRebases(ctx context.Context, specs []engine.RebaseSpec, opts engine.ValidateRebasesOpts) (*engine.RebaseValidation, error)
}

// Planner computes Plans.
type Planner struct {
	Engine PlanEngine
	Log    Logger
	// Activity, when non-nil, receives live rebase-validation events with
	// Parent mapped to the planned parent branch name. See
	// engine.RebaseProgressFunc for its concurrency contract.
	Activity engine.RebaseProgressFunc
}

// Plan is a restack worked out in full before anything moves.
type Plan struct {
	// Branches is the requested set, in topological order.
	Branches engine.Branches
	// Engine is the engine's per-branch plan.
	Engine *engine.RestackPlan
	// Validation is the result of trial-rebasing Engine.Specs.
	Validation *engine.RebaseValidation

	// Apply holds the branches proven safe to apply, in order.
	Apply engine.Branches
	// Conflicts holds branches whose trial rebase conflicted.
	Conflicts []string
	// Blocked holds branches that were not tried because an ancestor failed.
	Blocked []string
}

// Empty reports whether the plan moves nothing: no branch needs a rebase and
// none needs its metadata applied.
func (p *Plan) Empty() bool {
	return p == nil || p.Engine == nil || (len(p.Engine.Specs) == 0 && len(p.Engine.ApplyMap) == 0)
}

// Plan validates branch ancestry, builds the engine plan (or reuses prePlan
// when the caller already has one for these branches), trial-rebases it, and
// classifies every branch. It returns an error only for a system failure; a
// conflict is a classification, not an error.
func (p Planner) Plan(ctx context.Context, branches engine.Branches, prePlan *engine.RestackPlan) (*Plan, error) {
	plan := &Plan{Branches: branches}
	if len(branches) == 0 {
		return plan, nil
	}
	p.Log.Info("restack started branches=%v count=%v", branches.Names(), len(branches))

	if err := p.validateAncestry(ctx, branches); err != nil {
		return nil, fmt.Errorf("pre-flight validation failed: %w", err)
	}

	plan.Engine = prePlan
	if plan.Engine == nil {
		enginePlan, err := p.Engine.PlanRestack(ctx, branches)
		if err != nil {
			return nil, fmt.Errorf("failed to plan restack: %w", err)
		}
		plan.Engine = enginePlan
	}
	if plan.Empty() {
		p.Log.Info("restack no specs built, nothing to do")
		return plan, nil
	}

	plan.Validation = &engine.RebaseValidation{Success: true, NewSHAs: map[string]string{}, RerereResolved: map[string]int{}}
	if len(plan.Engine.Specs) > 0 {
		validation, err := p.Engine.ValidateRebases(ctx, plan.Engine.Specs, engine.ValidateRebasesOpts{Progress: planParentActivity(plan.Engine, p.Activity)})
		if err != nil {
			return nil, fmt.Errorf("failed to validate rebases: %w", err)
		}
		plan.Validation = validation
	}

	// A system error is fatal; a conflict is handled by the executor.
	if !plan.Validation.Success {
		for _, f := range plan.Validation.Failed {
			if f.ErrorType == engine.ValidationErrorSystem {
				return nil, fmt.Errorf("validation failed for %s: %s", f.Branch, f.ErrorMessage)
			}
			if len(f.ConflictingFiles) > 0 {
				p.Log.Debug("conflict detected during validation branch=%v files=%v", f.Branch, f.ConflictingFiles)
			}
		}
	}

	plan.Apply, plan.Conflicts, plan.Blocked = classifyValidatedBranches(branches, plan.Engine, plan.Validation)
	return plan, nil
}

// planParentActivity adapts an activity callback for display: specs name the
// rebase target by revision, so Parent is replaced with the planned parent
// branch name. Returns nil when there is no callback so validation skips
// reporting entirely.
func planParentActivity(plan *engine.RestackPlan, activity engine.RebaseProgressFunc) engine.RebaseProgressFunc {
	if activity == nil {
		return nil
	}
	return func(event engine.RebaseProgress) {
		if item, ok := plan.Items[event.Branch]; ok {
			event.Parent = item.NewParent
		}
		activity(event)
	}
}

// validateAncestry performs pre-flight checks on branch ancestry relationships.
// Returns an error if any branch has invalid parent relationships.
// Note: Missing parents are tolerated as buildRebaseSpecs will handle auto-reparenting.
func (p Planner) validateAncestry(ctx context.Context, branches engine.Branches) error {
	// Resolve every branch and parent revision in one batched call instead of
	// a git rev-parse per branch (and per parent).
	revBranches := make(engine.Branches, 0, len(branches)*2)
	for _, branch := range branches {
		if branch.IsTrunk() {
			continue
		}
		revBranches = append(revBranches, branch)
		if parent := branch.Parent(); parent != nil {
			revBranches = append(revBranches, *parent)
		}
	}
	revisions := p.Engine.BatchRevisions(ctx, revBranches)

	for _, branch := range branches {
		branchName := branch.Name()

		// Trunk branches don't need validation
		if branch.IsTrunk() {
			continue
		}

		// Verify branch itself exists
		branchRev, ok := revisions.Rev(branchName)
		if !ok {
			return fmt.Errorf("branch %s cannot be resolved", branchName)
		}

		// If branch has a parent, validate it only if it exists
		// (missing parents will be handled by auto-reparenting logic)
		parent := branch.Parent()
		if parent == nil {
			continue
		}
		parentName := parent.Name()
		parentRev, ok := revisions.Rev(parentName)
		if !ok {
			// Parent doesn't exist - this is OK, auto-reparenting will handle it
			p.Log.Debug("parent branch missing, will auto-reparent branch=%v parent=%v", branchName, parentName)
			continue
		}

		// Parent exists - verify they have a common ancestor
		if _, err := p.Engine.GetMergeBase(ctx, parentRev, branchRev); err != nil {
			return fmt.Errorf("branch %s and parent %s have no common ancestor: %w",
				branchName, parentName, err)
		}
	}
	return nil
}

// classifyValidatedBranches splits the planned branches into ones safe to
// apply now, ones that conflicted, and ones blocked behind a conflict, using
// the validation result as the source of truth instead of position-in-specs
// arithmetic. A branch is applied only if its action proves it's safe:
// Frozen/Anchor/MetadataRefresh branches don't touch a rebase worktree at
// all, and Validated branches need a confirmed SHA. Every planned branch
// lands in exactly one of the three buckets so none silently disappears from
// reporting — a Validated branch with no SHA that isn't in the failed set was
// skipped because an ancestor failed, and is classified blocked.
func classifyValidatedBranches(branches engine.Branches, plan *engine.RestackPlan, validation *engine.RebaseValidation) (success engine.Branches, conflicts, blocked []string) {
	if plan == nil || validation == nil {
		return nil, nil, nil
	}
	failed := make(map[string]bool, len(validation.Failed))
	for _, f := range validation.Failed {
		failed[f.Branch] = true
	}
	builder := engine.NewBranchesBuilder(len(branches))
	for _, branch := range branches {
		branchName := branch.Name()
		if _, exists := plan.ApplyMap[branchName]; !exists {
			continue
		}
		item, ok := plan.Items[branchName]
		if !ok {
			continue
		}

		switch item.Action {
		case engine.RestackPlanApplyFrozen, engine.RestackPlanApplyAnchor, engine.RestackPlanApplyMetadataRefresh:
			builder.Add(branch)
		case engine.RestackPlanApplyValidated:
			switch {
			case validation.NewSHAs[branchName] != "":
				builder.Add(branch)
			case failed[branchName]:
				conflicts = append(conflicts, branchName)
			default:
				blocked = append(blocked, branchName)
			}
		}
	}
	return builder.Build(), conflicts, blocked
}
