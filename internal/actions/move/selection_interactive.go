package move

import (
	"fmt"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/tui"
)

// SelectOntoInteractive shows an interactive branch selector for choosing the "onto" branch
// and returns precomputed rebase specs for the final selection.
// This uses a compact inline bubbletea model that combines selection and confirmation.
func SelectOntoInteractive(ctx *app.Context, sourceBranch string) (string, []engine.RebaseSpec, error) {
	selection, err := PrepareSelection(ctx, sourceBranch)
	if err != nil {
		return "", nil, err
	}

	// Create validation function that checks for conflicts when moving to a branch.
	validator := func(ontoBranch string) (*tui.MoveValidation, error) {
		validation, commits, rebaseSpecs, err := selection.ValidateOnto(ctx.Context, ontoBranch)
		if err != nil {
			return nil, err
		}

		if !validation.Success {
			return &tui.MoveValidation{
				Valid:          false,
				Message:        fmt.Sprintf("Conflicts on %s: %s", validation.FailedBranch, validation.ErrorMessage),
				Commits:        commits,
				HasConflicts:   true,
				ConflictBranch: validation.FailedBranch,
				ConflictError:  validation.ErrorMessage,
				RebaseSpecs:    rebaseSpecs,
			}, nil
		}

		return &tui.MoveValidation{
			Valid:       true,
			Message:     "Move will complete without conflicts",
			Commits:     commits,
			RebaseSpecs: rebaseSpecs,
		}, nil
	}

	// Use the compact move model with selection + confirmation.
	config := tui.MoveModelConfig{
		SourceBranch: sourceBranch,
		Descendants:  selection.Descendants(),
		OldParent:    selection.OldParent(),
		OldParentRev: selection.OldParentRev(),
		Validator:    validator,
	}

	result, err := tui.PromptMoveSelect(ctx.Engine, config)
	if err != nil {
		return "", nil, err
	}

	return result.SelectedParent, result.RebaseSpecs, nil
}
