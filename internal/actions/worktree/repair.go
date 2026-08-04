package worktree

import (
	"fmt"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/output"
)

type RepairOptions struct {
	NameOrBranch string
}

type RepairEntry struct {
	Name         string `json:"name"`
	Action       string `json:"action"`
	AnchorBranch string `json:"anchor_branch,omitempty"`
}

type RepairResult struct {
	Repaired []RepairEntry  `json:"repaired"`
	Skipped  []SkippedEntry `json:"skipped,omitempty"`
}

func RepairAction(ctx *app.Context, opts RepairOptions) (*RepairResult, error) {
	if count, err := ctx.Engine.PruneOrphanedWorktreePathRefs(ctx.Context); err != nil {
		return nil, err
	} else if count > 0 {
		ctx.Output.Info("Removed %d orphaned worktree path registration(s)", count)
	}
	listResult, err := listEntries(ctx, ListOptions(opts))
	if err != nil {
		return nil, err
	}
	if opts.NameOrBranch != "" && len(listResult.Worktrees) == 0 {
		return nil, fmt.Errorf("no worktree found for %s", output.BranchName(opts.NameOrBranch))
	}

	result := &RepairResult{
		Repaired: []RepairEntry{},
		Skipped:  []SkippedEntry{},
	}

	for _, entry := range listResult.Worktrees {
		if !entry.NeedsRepair {
			result.Skipped = append(result.Skipped, SkippedEntry{
				Name:   entry.displayName(),
				Reason: "registration is already healthy",
			})
			continue
		}

		repaired, err := repairEntry(ctx, entry)
		if err != nil {
			result.Skipped = append(result.Skipped, SkippedEntry{
				Name:   entry.displayName(),
				Reason: err.Error(),
			})
			continue
		}
		result.Repaired = append(result.Repaired, repaired)
	}

	return result, nil
}

func repairEntry(ctx *app.Context, entry Entry) (RepairEntry, error) {
	switch entry.RegistrationState {
	case RegistrationStateLegacy:
		wtInfo, err := findWorktreeByNameOrBranch(ctx, entry.AnchorBranch)
		if err != nil {
			return RepairEntry{}, err
		}
		anchorName, err := convertLegacyRegistration(ctx, *wtInfo, entry.AnchorBranch)
		if err != nil {
			return RepairEntry{}, err
		}
		return RepairEntry{
			Name:         entry.displayName(),
			Action:       "converted legacy registration to hidden anchor",
			AnchorBranch: anchorName,
		}, nil

	case RegistrationStateInvalid:
		wtInfo, err := findWorktreeByNameOrBranch(ctx, entry.AnchorBranch)
		if err != nil {
			return RepairEntry{}, err
		}

		if !entry.Exists {
			if err := ctx.Engine.UnregisterWorktree(ctx.Context, wtInfo.AnchorBranch); err != nil {
				return RepairEntry{}, fmt.Errorf("failed to remove stale registration: %w", err)
			}
			return RepairEntry{
				Name:   entry.displayName(),
				Action: "removed stale registration",
			}, nil
		}

		if entry.CurrentBranch == "" {
			if err := ctx.Engine.UnregisterWorktree(ctx.Context, wtInfo.AnchorBranch); err != nil {
				return RepairEntry{}, fmt.Errorf("failed to remove detached invalid registration: %w", err)
			}
			return RepairEntry{Name: entry.displayName(), Action: "removed invalid detached registration"}, nil
		}

		currentBranch := ctx.Engine.GetBranch(entry.CurrentBranch)
		if !currentBranch.IsTracked() {
			if err := ctx.Engine.UnregisterWorktree(ctx.Context, wtInfo.AnchorBranch); err != nil {
				return RepairEntry{}, fmt.Errorf("failed to remove untracked invalid registration: %w", err)
			}
			return RepairEntry{Name: entry.displayName(), Action: "removed invalid untracked registration"}, nil
		}

		stackRootName := ctx.Engine.GetStackRootForBranch(currentBranch)
		if stackRootName == "" {
			stackRootName = entry.CurrentBranch
		}
		stackRoot := ctx.Engine.GetBranch(stackRootName)
		if !stackRoot.IsTracked() {
			return RepairEntry{}, fmt.Errorf("cannot repair %s automatically because no tracked stack root could be determined", output.BranchName(entry.displayName()))
		}

		if ctx.Engine.IsWorktreeAnchor(stackRoot) {
			existing, err := ctx.Engine.GetWorktreeForStack(stackRootName)
			if err != nil {
				return RepairEntry{}, fmt.Errorf("failed to inspect anchor registration: %w", err)
			}
			if existing != nil && existing.Path != wtInfo.Path {
				return RepairEntry{}, fmt.Errorf("cannot repair %s automatically because anchor %s is already registered to %s", output.BranchName(entry.displayName()), output.BranchName(stackRootName), existing.Path)
			}
			if existing == nil {
				if err := moveRegistration(ctx, *wtInfo, stackRootName); err != nil {
					return RepairEntry{}, err
				}
			} else if err := ctx.Engine.UnregisterWorktree(ctx.Context, wtInfo.AnchorBranch); err != nil {
				return RepairEntry{}, fmt.Errorf("failed to remove stale registration %s: %w", output.BranchName(wtInfo.AnchorBranch), err)
			}
			return RepairEntry{
				Name:         entry.displayName(),
				Action:       "moved registration to existing anchor",
				AnchorBranch: stackRootName,
			}, nil
		}

		anchorName, err := convertLegacyRegistration(ctx, *wtInfo, stackRootName)
		if err != nil {
			return RepairEntry{}, err
		}
		return RepairEntry{
			Name:         entry.displayName(),
			Action:       "recovered worktree by inserting hidden anchor",
			AnchorBranch: anchorName,
		}, nil
	}

	return RepairEntry{}, fmt.Errorf("registration is already healthy")
}

// moveRegistration transfers a registration after the caller has established
// that the destination anchor is safe. The registry's reverse path claim is
// exclusive, so the old claim must be removed before creating the new one.
// Restore the old registration if the replacement fails.
func moveRegistration(ctx *app.Context, from engine.WorktreeInfo, toAnchor string) error {
	if err := ctx.Engine.UnregisterWorktree(ctx.Context, from.AnchorBranch); err != nil {
		return fmt.Errorf("failed to remove stale registration %s: %w", output.BranchName(from.AnchorBranch), err)
	}
	if err := ctx.Engine.RegisterWorktreeWithName(toAnchor, from.Path, from.Name); err != nil {
		if restoreErr := ctx.Engine.RegisterWorktreeWithName(from.AnchorBranch, from.Path, from.Name); restoreErr != nil {
			return fmt.Errorf("failed to register worktree under anchor %s: %w (also failed to restore %s: %w)", toAnchor, err, output.BranchName(from.AnchorBranch), restoreErr)
		}
		return fmt.Errorf("failed to register worktree under anchor %s: %w", toAnchor, err)
	}
	return nil
}

func convertLegacyRegistration(ctx *app.Context, wtInfo engine.WorktreeInfo, rootBranchName string) (string, error) {
	rootBranch := ctx.Engine.GetBranch(rootBranchName)
	if !rootBranch.IsTracked() {
		return "", fmt.Errorf("branch %s is not tracked by stackit", output.BranchName(rootBranchName))
	}

	originalParent := ctx.Engine.Trunk().GetName()
	if parent := rootBranch.GetParent(); parent != nil {
		originalParent = parent.GetName()
	}
	scope := rootBranch.GetScope().String()
	name := wtInfo.Name
	if name == "" {
		name = rootBranchName
	}

	anchorBranchName, err := generateAnchorBranchName(ctx, wtInfo.MainRepoDir, name, scope)
	if err != nil {
		return "", err
	}
	if ctx.Engine.BranchNames().Contains(anchorBranchName) {
		return "", fmt.Errorf("generated anchor branch %s already exists", output.BranchName(anchorBranchName))
	}

	parentBranch := ctx.Engine.GetBranch(originalParent)
	parentSHA, err := parentBranch.GetRevision()
	if err != nil {
		return "", fmt.Errorf("failed to get parent revision for %s: %w", output.BranchName(originalParent), err)
	}
	if err := ctx.Engine.CreateBranch(ctx.Context, anchorBranchName, parentSHA); err != nil {
		return "", fmt.Errorf("failed to create anchor branch %s: %w", output.BranchName(anchorBranchName), err)
	}

	anchorBranch := ctx.Engine.GetBranch(anchorBranchName)
	anchorCreated := true
	rootReparented := false
	registered := false
	legacyUnregistered := false
	cleanup := func() {
		if registered {
			_ = ctx.Engine.UnregisterWorktree(ctx.Context, anchorBranchName)
		}
		if legacyUnregistered {
			_ = ctx.Engine.RegisterWorktreeWithName(wtInfo.AnchorBranch, wtInfo.Path, wtInfo.Name)
		}
		if rootReparented {
			_ = ctx.Engine.ReparentBranch(ctx.Context, ctx.Engine.GetBranch(rootBranchName), ctx.Engine.GetBranch(originalParent))
		}
		if anchorCreated {
			cleanupAnchorBranch(ctx.Context, ctx.Engine, anchorBranchName, ctx.Output)
		}
	}

	if err := ctx.Engine.SetParent(ctx.Context, anchorBranch, parentBranch, engine.DivergenceRecompute); err != nil {
		cleanup()
		return "", fmt.Errorf("failed to set parent on anchor branch %s: %w", output.BranchName(anchorBranchName), err)
	}
	if err := ctx.Engine.SetBranchType(anchorBranch, git.BranchTypeWorktreeAnchor); err != nil {
		cleanup()
		return "", fmt.Errorf("failed to mark %s as a worktree anchor: %w", output.BranchName(anchorBranchName), err)
	}
	if scope != "" {
		if err := ctx.Engine.SetScope(ctx.Context, anchorBranch, engine.NewScope(scope)); err != nil {
			cleanup()
			return "", fmt.Errorf("failed to set scope on anchor branch %s: %w", output.BranchName(anchorBranchName), err)
		}
	}
	if err := ctx.Engine.ReparentBranch(ctx.Context, ctx.Engine.GetBranch(rootBranchName), anchorBranch); err != nil {
		cleanup()
		return "", fmt.Errorf("failed to reparent %s under anchor %s: %w", output.BranchName(rootBranchName), output.BranchName(anchorBranchName), err)
	}
	rootReparented = true
	// The path's reverse registration claim is intentionally exclusive. Remove
	// the legacy forward claim before creating the anchored one; cleanup restores
	// it if the new registration cannot be written.
	if err := ctx.Engine.UnregisterWorktree(ctx.Context, wtInfo.AnchorBranch); err != nil {
		cleanup()
		return "", fmt.Errorf("failed to remove legacy registration %s: %w", output.BranchName(wtInfo.AnchorBranch), err)
	}
	legacyUnregistered = true
	if err := ctx.Engine.RegisterWorktreeWithName(anchorBranchName, wtInfo.Path, wtInfo.Name); err != nil {
		cleanup()
		return "", fmt.Errorf("failed to register worktree under anchor %s: %w", output.BranchName(anchorBranchName), err)
	}
	registered = true
	return anchorBranchName, nil
}
