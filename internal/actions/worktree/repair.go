package worktree

import (
	"context"
	"errors"
	"fmt"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/output"
)

type RepairOptions struct {
	Selector WorktreeSelector
	// Config is the main repository's resolved configuration, used to name
	// anchors for legacy registrations. Nil falls back to Deps.Config.
	Config RepoConfig
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

func RepairAction(ctx context.Context, deps Deps, opts RepairOptions) (*RepairResult, error) {
	if count, err := deps.Engine.PruneOrphanedWorktreePathRefs(ctx); err != nil {
		return nil, err
	} else if count > 0 {
		deps.Out.Info("Removed %d orphaned worktree path registration(s)", count)
	}
	cfg, err := resolveRepoConfig(opts.Config, deps.Config)
	if err != nil {
		return nil, err
	}
	listResult, err := listEntries(ctx, deps, ListOptions{Selector: opts.Selector})
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

		repaired, err := repairRegistration(ctx, deps, cfg, entry, entry.registration)
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

func repairRegistration(ctx context.Context, deps Deps, cfg RepoConfig, entry Entry, wtInfo engine.WorktreeInfo) (RepairEntry, error) {
	switch entry.Lifecycle.Registration {
	case RegistrationStateLegacy:
		anchorName, err := convertLegacyRegistration(ctx, deps, cfg, wtInfo, entry.AnchorBranch)
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
			if err := deps.Engine.UnregisterWorktree(ctx, wtInfo.AnchorBranch); err != nil {
				return RepairEntry{}, fmt.Errorf("failed to remove stale registration: %w", err)
			}
			return RepairEntry{
				Name:   entry.displayName(),
				Action: "removed stale registration",
			}, nil
		}

		if entry.CurrentBranch == "" {
			if err := deps.Engine.UnregisterWorktree(ctx, wtInfo.AnchorBranch); err != nil {
				return RepairEntry{}, fmt.Errorf("failed to remove detached invalid registration: %w", err)
			}
			return RepairEntry{Name: entry.displayName(), Action: "removed invalid detached registration"}, nil
		}

		currentBranch := deps.Engine.GetBranch(entry.CurrentBranch)
		if !currentBranch.IsTracked() {
			if err := deps.Engine.UnregisterWorktree(ctx, wtInfo.AnchorBranch); err != nil {
				return RepairEntry{}, fmt.Errorf("failed to remove untracked invalid registration: %w", err)
			}
			return RepairEntry{Name: entry.displayName(), Action: "removed invalid untracked registration"}, nil
		}

		stackRootName := deps.Engine.GetStackRootForBranch(currentBranch)
		if stackRootName == "" {
			stackRootName = entry.CurrentBranch
		}
		stackRoot := deps.Engine.GetBranch(stackRootName)
		if !stackRoot.IsTracked() {
			return RepairEntry{}, fmt.Errorf("cannot repair %s automatically because no tracked stack root could be determined", output.BranchName(entry.displayName()))
		}

		if deps.Engine.IsWorktreeAnchor(stackRoot) {
			existing, err := deps.Engine.GetWorktreeForStack(ctx, stackRootName)
			if err != nil {
				return RepairEntry{}, fmt.Errorf("failed to inspect anchor registration: %w", err)
			}
			if existing != nil && existing.Path != wtInfo.Path {
				return RepairEntry{}, fmt.Errorf("cannot repair %s automatically because anchor %s is already registered to %s", output.BranchName(entry.displayName()), output.BranchName(stackRootName), existing.Path)
			}
			if existing == nil {
				if err := moveRegistration(ctx, deps, wtInfo, stackRootName); err != nil {
					return RepairEntry{}, err
				}
			} else if err := deps.Engine.UnregisterWorktree(ctx, wtInfo.AnchorBranch); err != nil {
				return RepairEntry{}, fmt.Errorf("failed to remove stale registration %s: %w", output.BranchName(wtInfo.AnchorBranch), err)
			}
			return RepairEntry{
				Name:         entry.displayName(),
				Action:       "moved registration to existing anchor",
				AnchorBranch: stackRootName,
			}, nil
		}

		anchorName, err := convertLegacyRegistration(ctx, deps, cfg, wtInfo, stackRootName)
		if err != nil {
			return RepairEntry{}, err
		}
		return RepairEntry{
			Name:         entry.displayName(),
			Action:       "recovered worktree by inserting hidden anchor",
			AnchorBranch: anchorName,
		}, nil
	}

	return RepairEntry{}, errors.New("registration is already healthy")
}

// migrateRegistration transfers a registration after the caller has
// established that the destination anchor is safe. The reverse path claim is
// exclusive, so the old claim must be removed before creating the new one.
// The returned undo function restores the old registration for a later
// lifecycle rollback.
func migrateRegistration(ctx context.Context, deps Deps, from engine.WorktreeInfo, toAnchor string) (func() error, error) {
	if err := deps.Engine.UnregisterWorktree(ctx, from.AnchorBranch); err != nil {
		return nil, fmt.Errorf("failed to remove stale registration %s: %w", output.BranchName(from.AnchorBranch), err)
	}
	to := from.Registration()
	to.AnchorBranch = toAnchor
	if err := deps.Engine.RegisterWorktree(ctx, to); err != nil {
		if restoreErr := deps.Engine.RegisterWorktree(ctx, from.Registration()); restoreErr != nil {
			return nil, fmt.Errorf("failed to register worktree under anchor %s: %w (also failed to restore %s: %w)", toAnchor, err, output.BranchName(from.AnchorBranch), restoreErr)
		}
		return nil, fmt.Errorf("failed to register worktree under anchor %s: %w", toAnchor, err)
	}
	return func() error {
		if err := deps.Engine.UnregisterWorktree(ctx, toAnchor); err != nil {
			return fmt.Errorf("failed to remove replacement registration %s: %w", output.BranchName(toAnchor), err)
		}
		if err := deps.Engine.RegisterWorktree(ctx, from.Registration()); err != nil {
			return fmt.Errorf("failed to restore registration %s: %w", output.BranchName(from.AnchorBranch), err)
		}
		return nil
	}, nil
}

func moveRegistration(ctx context.Context, deps Deps, from engine.WorktreeInfo, toAnchor string) error {
	_, err := migrateRegistration(ctx, deps, from, toAnchor)
	return err
}

func convertLegacyRegistration(ctx context.Context, deps Deps, cfg RepoConfig, wtInfo engine.WorktreeInfo, rootBranchName string) (string, error) {
	rootBranch := deps.Engine.GetBranch(rootBranchName)
	if !rootBranch.IsTracked() {
		return "", fmt.Errorf("branch %s is not tracked by stackit", output.BranchName(rootBranchName))
	}

	originalParent := deps.Engine.Trunk().Name()
	if parent := rootBranch.Parent(); parent != nil {
		originalParent = parent.Name()
	}
	scope := rootBranch.Scope().String()
	name := wtInfo.Name
	if name == "" {
		name = engine.WorktreeName(rootBranchName)
	}

	anchorBranchName, err := generateAnchorBranchName(ctx, deps.Engine, cfg.BranchNamePattern(), name.String(), scope)
	if err != nil {
		return "", err
	}
	anchorBranch, cleanupAnchor, err := createWorktreeAnchor(ctx, deps.Engine, deps.Out, anchorBranchName, originalParent, scope)
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
			_ = deps.Engine.ReparentBranch(ctx, deps.Engine.GetBranch(rootBranchName), deps.Engine.GetBranch(originalParent))
		}
		cleanupAnchor()
	}
	if err := deps.Engine.ReparentBranch(ctx, deps.Engine.GetBranch(rootBranchName), anchorBranch); err != nil {
		cleanup()
		return "", fmt.Errorf("failed to reparent %s under anchor %s: %w", output.BranchName(rootBranchName), output.BranchName(anchorBranchName), err)
	}
	rootReparented = true
	undoRegistration, err = migrateRegistration(ctx, deps, wtInfo, anchorBranchName)
	if err != nil {
		cleanup()
		return "", err
	}
	registrationMigrated = true
	return anchorBranchName, nil
}
