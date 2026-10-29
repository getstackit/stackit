// Package freeze implements the freeze and unfreeze commands, which stop (and
// resume) restacking for a branch and the branches it depends on or that
// depend on it.
package freeze

import (
	"context"
	"fmt"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/output"
)

// Engine lists exactly the engine methods freeze and unfreeze call.
type Engine interface {
	GetBranch(branchName string) engine.Branch
	Graph(ctx context.Context, strategy engine.SortStrategy) *engine.StackGraph
	SetFrozen(ctx context.Context, branches engine.Branches, state engine.FreezeState) (engine.BatchFreezeResult, error)
}

// Deps are the collaborators freeze needs. Adapters build them from their own
// runtime state; the action never sees bootstrap types.
type Deps struct {
	Engine Engine
	Out    output.Output
}

// Freeze freezes the specified branch and all branches downstack of it
// (recursive parents).
func Freeze(ctx context.Context, deps Deps, branchName string) error {
	branch := deps.Engine.GetBranch(branchName)
	if branch.IsTrunk() {
		return fmt.Errorf("cannot freeze trunk branch %s", branchName)
	}
	if !branch.IsTracked() {
		return fmt.Errorf("branch %s is not tracked by stackit", branchName)
	}

	downstack := deps.Engine.Graph(ctx, engine.SortStrategyAlphabetical).Range(branch, engine.StackRange{
		RecursiveParents: true,
		IncludeCurrent:   true,
	})
	return setFrozen(ctx, deps, branchName, downstack.WithoutTrunk(), engine.BranchFrozen)
}

// Unfreeze unfreezes the specified branch and all branches upstack of it
// (recursive children).
func Unfreeze(ctx context.Context, deps Deps, branchName string) error {
	branch := deps.Engine.GetBranch(branchName)
	if !branch.IsTracked() {
		return fmt.Errorf("branch %s is not tracked by stackit", branchName)
	}

	upstack := deps.Engine.Graph(ctx, engine.SortStrategyAlphabetical).Range(branch, engine.StackRange{
		IncludeCurrent:    true,
		RecursiveChildren: true,
	})
	return setFrozen(ctx, deps, branchName, upstack.WithoutTrunk(), engine.BranchThawed)
}

func setFrozen(ctx context.Context, deps Deps, branchName string, branches engine.Branches, state engine.FreezeState) error {
	if len(branches) == 0 {
		return nil
	}

	verb, done := "freeze", "Frozen"
	if state == engine.BranchThawed {
		verb, done = "unfreeze", "Unfrozen"
	}

	res, err := deps.Engine.SetFrozen(ctx, branches, state)
	if err != nil {
		for name, branchErr := range res.Errors {
			deps.Out.Warn("Failed to %s %s: %v", verb, name, branchErr)
		}
		return fmt.Errorf("failed to %s branches: %w", verb, err)
	}

	for _, name := range res.AffectedBranches {
		deps.Out.Info("%s %s locally.", done, output.Branch(name, name == branchName))
	}
	return nil
}
