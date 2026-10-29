// Package rename implements the rename command, which renames the current
// branch and updates the stack metadata referencing it.
package rename

import (
	"context"
	"errors"
	"fmt"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/actions/validation"
	"github.com/getstackit/stackit/internal/branchname"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/output"
)

// Options contains options for the rename command
type Options struct {
	NewName string
	Force   bool
}

// Handler supplies a new branch name interactively when one is not given
// on the command line. The CLI adapter implements it; the action layer does not
// import the TUI.
type Handler interface {
	// PromptNewName asks the user for a new branch name, given the current one.
	// Implementations should return an error when prompting is not possible
	// (e.g. non-interactive mode).
	PromptNewName(currentName string) (string, error)
}

// Engine lists exactly the engine methods rename calls.
type Engine interface {
	validation.BranchValidationEngine
	AllBranches() engine.Branches
	UpsertPrInfo(ctx context.Context, branch engine.Branch, prInfo *engine.PrInfo) error
	RenameBranch(ctx context.Context, oldBranch, newBranch engine.Branch) error
}

// Deps are the collaborators rename needs. Adapters build them from their own
// runtime state; the action never sees bootstrap types.
type Deps struct {
	Engine    Engine
	Out       output.Output
	Snapshots actions.Snapshots
}

// Action renames the current branch and updates metadata
func Action(ctx context.Context, deps Deps, opts Options, handler Handler) error {
	eng := deps.Engine
	out := deps.Out

	// Validate preconditions
	if err := validation.ModifyBranchChain(ctx, eng, "rename").Validate(); err != nil {
		return err
	}
	currentBranch := eng.CurrentBranch(ctx).Name()

	newName := opts.NewName
	if newName == "" {
		var err error
		newName, err = handler.PromptNewName(currentBranch)
		if err != nil {
			return err
		}
	}

	newName = branchname.Sanitize(newName)
	if newName == "" {
		return errors.New("invalid branch name")
	}

	if newName == currentBranch {
		out.Info("Branch is already named %s.", newName)
		return nil
	}

	allBranches := eng.AllBranches()
	for _, b := range allBranches {
		if b.Name() == newName {
			return fmt.Errorf("branch %s already exists", newName)
		}
	}

	branch := eng.GetBranch(currentBranch)
	prInfo, _ := branch.PrInfo()

	if prInfo != nil && prInfo.Number() != nil {
		if !opts.Force {
			return fmt.Errorf("branch %s is associated with PR #%d. Renaming it will remove this association. Use --force to proceed", currentBranch, *prInfo.Number())
		}
		out.Info("Removing association with PR #%d as GitHub PR branch names are immutable.", *prInfo.Number())
		if err := eng.UpsertPrInfo(ctx, branch, nil); err != nil {
			return fmt.Errorf("failed to update metadata: %w", err)
		}
	}

	snapshotOpts := actions.NewSnapshot("rename",
		actions.WithArg(newName),
		actions.WithFlag(opts.Force, "--force"),
	)
	deps.Snapshots.Take(ctx, snapshotOpts)

	oldBranchObj := eng.GetBranch(currentBranch)
	newBranchObj := eng.GetBranch(newName)
	if err := eng.RenameBranch(ctx, oldBranchObj, newBranchObj); err != nil {
		return fmt.Errorf("failed to rename branch: %w", err)
	}

	out.Info("Renamed %s to %s.", output.BranchName(currentBranch), output.CurrentBranch(newName))

	return nil
}
