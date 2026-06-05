package actions

import (
	"fmt"

	"github.com/getstackit/stackit/internal/actions/validation"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/tui"
	"github.com/getstackit/stackit/internal/utils"
)

// RenameOptions contains options for the rename command
type RenameOptions struct {
	NewName string
	Force   bool
}

// RenameAction renames the current branch and updates metadata
func RenameAction(ctx *app.Context, opts RenameOptions) error {
	eng := ctx.Engine
	out := ctx.Output

	// Validate preconditions
	if err := validation.ModifyBranchChain(eng, "rename").Validate(); err != nil {
		return err
	}
	currentBranch := eng.CurrentBranch().GetName()

	newName := opts.NewName
	if newName == "" {
		if !utils.IsInteractive() {
			return fmt.Errorf("branch name is required in non-interactive mode")
		}

		var err error
		newName, err = tui.PromptTextInput("Enter new branch name:", currentBranch)
		if err != nil {
			return err
		}
	}

	newName = utils.SanitizeBranchName(newName)
	if newName == "" {
		return fmt.Errorf("invalid branch name")
	}

	if newName == currentBranch {
		out.Info("Branch is already named %s.", newName)
		return nil
	}

	allBranches := eng.AllBranches()
	for _, b := range allBranches {
		if b.GetName() == newName {
			return fmt.Errorf("branch %s already exists", newName)
		}
	}

	branch := eng.GetBranch(currentBranch)
	prInfo, _ := branch.GetPrInfo()

	if prInfo != nil && prInfo.Number() != nil {
		if !opts.Force {
			return fmt.Errorf("branch %s is associated with PR #%d. Renaming it will remove this association. Use --force to proceed", currentBranch, *prInfo.Number())
		}
		out.Info("Removing association with PR #%d as GitHub PR branch names are immutable.", *prInfo.Number())
		if err := eng.UpsertPrInfo(ctx.Context, branch, nil); err != nil {
			return fmt.Errorf("failed to update metadata: %w", err)
		}
	}

	snapshotOpts := NewSnapshot("rename",
		WithArg(newName),
		WithFlag(opts.Force, "--force"),
	)
	TakeBestEffortSnapshot(ctx, snapshotOpts)

	oldBranchObj := eng.GetBranch(currentBranch)
	newBranchObj := eng.GetBranch(newName)
	if err := eng.RenameBranch(ctx.Context, oldBranchObj, newBranchObj); err != nil {
		return fmt.Errorf("failed to rename branch: %w", err)
	}

	out.Info("Renamed %s to %s.", output.Branch(currentBranch, false), output.Branch(newName, true))

	return nil
}
