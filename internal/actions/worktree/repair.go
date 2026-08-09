package worktree

import (
	"fmt"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/config"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/output"
)

type RepairOptions struct {
	Selector WorktreeSelector
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
	if opts.Selector != "" && len(listResult.Worktrees) == 0 {
		return nil, fmt.Errorf("no worktree found for %s", output.BranchName(opts.Selector.String()))
	}

	result := &RepairResult{
		Repaired: []RepairEntry{},
		Skipped:  []SkippedEntry{},
	}

	for _, entry := range listResult.Worktrees {
		if !entry.Lifecycle.NeedsRepair() {
			result.Skipped = append(result.Skipped, SkippedEntry{
				Name:   entry.displayName(),
				Reason: "registration is already healthy",
			})
			continue
		}

		repaired, err := repairRegistration(ctx, entry, entry.registration)
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

func repairRegistration(ctx *app.Context, entry Entry, wtInfo engine.WorktreeInfo) (RepairEntry, error) {
	switch entry.Lifecycle.Registration {
	case RegistrationStateLegacy:
		anchorName, err := convertLegacyRegistration(ctx, wtInfo, entry.AnchorBranch)
		if err != nil {
			return RepairEntry{}, err
		}
		return RepairEntry{
			Name:         entry.displayName(),
			Action:       "converted legacy registration to hidden anchor",
			AnchorBranch: anchorName,
		}, nil

	case RegistrationStateInvalid:
		if !entry.Lifecycle.Exists() {
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
				if err := moveRegistration(ctx, wtInfo, stackRootName); err != nil {
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

		anchorName, err := convertLegacyRegistration(ctx, wtInfo, stackRootName)
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

// migrateRegistration transfers a registration after the caller has
// established that the destination anchor is safe. The reverse path claim is
// exclusive, so the old claim must be removed before creating the new one.
// The returned undo function restores the old registration for a later
// lifecycle rollback.
func migrateRegistration(ctx *app.Context, from engine.WorktreeInfo, toAnchor string) (func() error, error) {
	if err := ctx.Engine.UnregisterWorktree(ctx.Context, from.AnchorBranch); err != nil {
		return nil, fmt.Errorf("failed to remove stale registration %s: %w", output.BranchName(from.AnchorBranch), err)
	}
	to := from.Registration()
	to.AnchorBranch = toAnchor
	if err := ctx.Engine.RegisterWorktree(ctx.Context, to); err != nil {
		if restoreErr := ctx.Engine.RegisterWorktree(ctx.Context, from.Registration()); restoreErr != nil {
			return nil, fmt.Errorf("failed to register worktree under anchor %s: %w (also failed to restore %s: %w)", toAnchor, err, output.BranchName(from.AnchorBranch), restoreErr)
		}
		return nil, fmt.Errorf("failed to register worktree under anchor %s: %w", toAnchor, err)
	}
	return func() error {
		if err := ctx.Engine.UnregisterWorktree(ctx.Context, toAnchor); err != nil {
			return fmt.Errorf("failed to remove replacement registration %s: %w", output.BranchName(toAnchor), err)
		}
		if err := ctx.Engine.RegisterWorktree(ctx.Context, from.Registration()); err != nil {
			return fmt.Errorf("failed to restore registration %s: %w", output.BranchName(from.AnchorBranch), err)
		}
		return nil
	}, nil
}

func moveRegistration(ctx *app.Context, from engine.WorktreeInfo, toAnchor string) error {
	_, err := migrateRegistration(ctx, from, toAnchor)
	return err
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
		name = engine.WorktreeName(rootBranchName)
	}

	cfg, err := config.LoadConfig(wtInfo.MainRepoDir)
	if err != nil {
		return "", fmt.Errorf("load worktree configuration: %w", err)
	}
	anchorBranchName, err := generateAnchorBranchName(ctx, cfg.BranchNamePattern(), name.String(), scope)
	if err != nil {
		return "", err
	}
	anchorBranch, cleanupAnchor, err := createWorktreeAnchor(ctx, ctx.Engine, anchorBranchName, originalParent, scope)
	if err != nil {
		return "", err
	}
	rootReparented := false
	registrationMigrated := false
	var undoRegistration func() error
	cleanup := func() {
		if registrationMigrated {
			_ = undoRegistration()
		}
		if rootReparented {
			_ = ctx.Engine.ReparentBranch(ctx.Context, ctx.Engine.GetBranch(rootBranchName), ctx.Engine.GetBranch(originalParent))
		}
		cleanupAnchor()
	}
	if err := ctx.Engine.ReparentBranch(ctx.Context, ctx.Engine.GetBranch(rootBranchName), anchorBranch); err != nil {
		cleanup()
		return "", fmt.Errorf("failed to reparent %s under anchor %s: %w", output.BranchName(rootBranchName), output.BranchName(anchorBranchName), err)
	}
	rootReparented = true
	undoRegistration, err = migrateRegistration(ctx, wtInfo, anchorBranchName)
	if err != nil {
		cleanup()
		return "", err
	}
	registrationMigrated = true
	return anchorBranchName, nil
}
