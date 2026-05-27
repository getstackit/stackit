package scope

import (
	"fmt"
	"strings"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/errors"
	"github.com/getstackit/stackit/internal/tui/style"
)

// Options contains options for the scope command
type Options struct {
	Scope string
	Unset bool
	Show  bool
}

// Action implements the stackit scope command
func Action(ctx *app.Context, opts Options, handler Handler) error {
	if handler == nil {
		handler = &NullHandler{}
	}
	defer handler.Cleanup()

	eng := ctx.Engine
	out := ctx.Output

	// Get current branch
	currentBranch := ""
	if cb := eng.CurrentBranch(); cb != nil {
		currentBranch = cb.GetName()
	}
	isOnTrunk := currentBranch == eng.Trunk().GetName() || currentBranch == ""

	// Handle Show
	if opts.Show {
		if isOnTrunk {
			return errors.ErrNotOnBranch
		}
		currentBranchObj := eng.GetBranch(currentBranch)
		explicitScope := currentBranchObj.GetExplicitScope()
		resolvedScope := eng.GetScope(currentBranchObj)

		switch {
		case !explicitScope.IsEmpty():
			if explicitScope.IsNone() {
				out.Info("Branch %s has scope inheritance DISABLED (explicitly set to '%s').", style.ColorBranchName(currentBranch, false), explicitScope.String())
			} else {
				out.Info("Branch %s has explicit scope: %s", style.ColorBranchName(currentBranch, false), style.ColorDim(explicitScope.String()))
			}
		case !resolvedScope.IsEmpty():
			out.Info("Branch %s inherits scope: %s", style.ColorBranchName(currentBranch, false), style.ColorDim(resolvedScope.String()))
		default:
			out.Info("Branch %s has no scope set.", style.ColorBranchName(currentBranch, false))
		}
		return nil
	}

	// Handle Unset
	if opts.Unset {
		if isOnTrunk {
			return fmt.Errorf("cannot unset scope on trunk")
		}
		if err := eng.SetScope(ctx.Context, eng.GetBranch(currentBranch), engine.Empty()); err != nil {
			return fmt.Errorf("failed to unset scope: %w", err)
		}
		out.Info("Unset explicit scope for branch %s. It will now inherit from its parent.", style.ColorBranchName(currentBranch, false))

		// Mark branch for PR update and push metadata (defer GitHub API calls to sync)
		if err := eng.BatchMarkNeedsPRBodyUpdate(ctx, []string{currentBranch}); err != nil {
			out.Debug("Failed to mark branch for PR body update: %v", err)
		}
		if err := actions.PushMetadataOnly(ctx, eng, []string{currentBranch}); err != nil {
			out.Debug("Failed to push metadata changes: %v", err)
		}
		return nil
	}

	// If no scope provided and not show/unset, we don't know what to do
	if opts.Scope == "" {
		return fmt.Errorf("no scope name provided")
	}

	// Cannot set scope on trunk
	if isOnTrunk {
		return fmt.Errorf("cannot set scope on trunk")
	}

	// Update the current branch's scope
	currentBranchObj := eng.GetBranch(currentBranch)
	oldScope := eng.GetScope(currentBranchObj)
	newScope := engine.NewScope(opts.Scope)
	if err := eng.SetScope(ctx.Context, eng.GetBranch(currentBranch), newScope); err != nil {
		return fmt.Errorf("failed to set scope: %w", err)
	}

	if newScope.IsNone() {
		out.Info("Disabled scope for branch %s (breaks inheritance).", style.ColorBranchName(currentBranch, false))
	} else {
		out.Info("Set scope for branch %s to: %s", style.ColorBranchName(currentBranch, false), style.ColorDim(opts.Scope))

		// Rename prompt - only if scope changed and branch name contains old scope
		if oldScope.IsDefined() && !oldScope.Equal(newScope) && strings.Contains(currentBranch, oldScope.String()) {
			confirmed, err := handler.PromptConfirmRename(currentBranch, oldScope.String(), opts.Scope)
			if err == nil && confirmed {
				newName := strings.Replace(currentBranch, oldScope.String(), opts.Scope, 1)
				if err := eng.RenameBranch(ctx.Context, eng.GetBranch(currentBranch), eng.GetBranch(newName)); err != nil {
					out.Info("Warning: failed to rename branch: %v", err)
				} else {
					out.Info("Renamed branch %s to %s.", style.ColorBranchName(currentBranch, false), style.ColorBranchName(newName, true))
				}
			}
		}
	}

	// Mark branch for PR update and push metadata (defer GitHub API calls to sync)
	if err := eng.BatchMarkNeedsPRBodyUpdate(ctx, []string{currentBranch}); err != nil {
		out.Debug("Failed to mark branch for PR body update: %v", err)
	}
	if err := actions.PushMetadataOnly(ctx, eng, []string{currentBranch}); err != nil {
		out.Debug("Failed to push metadata changes: %v", err)
	}

	return nil
}
