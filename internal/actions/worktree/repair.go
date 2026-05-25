package worktree

import (
	"fmt"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/tui/style"
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
	listResult, err := listEntries(ctx, ListOptions(opts))
	if err != nil {
		return nil, err
	}
	if opts.NameOrBranch != "" && len(listResult.Worktrees) == 0 {
		return nil, fmt.Errorf("no worktree found for %s", style.ColorBranchName(opts.NameOrBranch, false))
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
			return RepairEntry{}, fmt.Errorf("cannot repair %s automatically because the worktree has no branch checked out", style.ColorBranchName(entry.displayName(), false))
		}

		currentBranch := ctx.Engine.GetBranch(entry.CurrentBranch)
		if !currentBranch.IsTracked() {
			return RepairEntry{}, fmt.Errorf("cannot repair %s automatically because %s is not tracked by stackit", style.ColorBranchName(entry.displayName(), false), style.ColorBranchName(entry.CurrentBranch, false))
		}

		stackRootName := ctx.Engine.GetStackRootForBranch(currentBranch)
		if stackRootName == "" {
			stackRootName = entry.CurrentBranch
		}
		stackRoot := ctx.Engine.GetBranch(stackRootName)
		if !stackRoot.IsTracked() {
			return RepairEntry{}, fmt.Errorf("cannot repair %s automatically because no tracked stack root could be determined", style.ColorBranchName(entry.displayName(), false))
		}

		if ctx.Engine.IsWorktreeAnchor(stackRoot) {
			existing, err := ctx.Engine.GetWorktreeForStack(stackRootName)
			if err != nil {
				return RepairEntry{}, fmt.Errorf("failed to inspect anchor registration: %w", err)
			}
			if existing != nil && existing.Path != wtInfo.Path {
				return RepairEntry{}, fmt.Errorf("cannot repair %s automatically because anchor %s is already registered to %s", style.ColorBranchName(entry.displayName(), false), style.ColorBranchName(stackRootName, false), existing.Path)
			}
			if err := ctx.Engine.RegisterWorktreeWithName(stackRootName, wtInfo.Path, wtInfo.Name); err != nil {
				return RepairEntry{}, fmt.Errorf("failed to register worktree under anchor %s: %w", stackRootName, err)
			}
			if err := ctx.Engine.UnregisterWorktree(ctx.Context, wtInfo.AnchorBranch); err != nil {
				_ = ctx.Engine.UnregisterWorktree(ctx.Context, stackRootName)
				return RepairEntry{}, fmt.Errorf("failed to remove stale registration %s: %w", style.ColorBranchName(wtInfo.AnchorBranch, false), err)
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

func convertLegacyRegistration(ctx *app.Context, wtInfo engine.WorktreeInfo, rootBranchName string) (string, error) {
	rootBranch := ctx.Engine.GetBranch(rootBranchName)
	if !rootBranch.IsTracked() {
		return "", fmt.Errorf("branch %s is not tracked by stackit", style.ColorBranchName(rootBranchName, false))
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
		return "", fmt.Errorf("generated anchor branch %s already exists", style.ColorBranchName(anchorBranchName, false))
	}

	parentBranch := ctx.Engine.GetBranch(originalParent)
	parentSHA, err := parentBranch.GetRevision()
	if err != nil {
		return "", fmt.Errorf("failed to get parent revision for %s: %w", style.ColorBranchName(originalParent, false), err)
	}
	if err := ctx.Engine.CreateBranch(ctx.Context, anchorBranchName, parentSHA); err != nil {
		return "", fmt.Errorf("failed to create anchor branch %s: %w", style.ColorBranchName(anchorBranchName, false), err)
	}

	anchorBranch := ctx.Engine.GetBranch(anchorBranchName)
	anchorCreated := true
	rootReparented := false
	registered := false
	cleanup := func() {
		if registered {
			_ = ctx.Engine.UnregisterWorktree(ctx.Context, anchorBranchName)
		}
		if rootReparented {
			_ = ctx.Engine.ReparentBranch(ctx.Context, ctx.Engine.GetBranch(rootBranchName), ctx.Engine.GetBranch(originalParent))
		}
		if anchorCreated {
			cleanupAnchorBranch(ctx.Context, ctx.Engine, anchorBranchName, ctx.Output)
		}
	}

	if err := ctx.Engine.SetParent(ctx.Context, anchorBranch, parentBranch); err != nil {
		cleanup()
		return "", fmt.Errorf("failed to set parent on anchor branch %s: %w", style.ColorBranchName(anchorBranchName, false), err)
	}
	if err := ctx.Engine.SetBranchType(anchorBranch, git.BranchTypeWorktreeAnchor); err != nil {
		cleanup()
		return "", fmt.Errorf("failed to mark %s as a worktree anchor: %w", style.ColorBranchName(anchorBranchName, false), err)
	}
	if scope != "" {
		if err := ctx.Engine.SetScope(ctx.Context, anchorBranch, engine.NewScope(scope)); err != nil {
			cleanup()
			return "", fmt.Errorf("failed to set scope on anchor branch %s: %w", style.ColorBranchName(anchorBranchName, false), err)
		}
	}
	if err := ctx.Engine.ReparentBranch(ctx.Context, ctx.Engine.GetBranch(rootBranchName), anchorBranch); err != nil {
		cleanup()
		return "", fmt.Errorf("failed to reparent %s under anchor %s: %w", style.ColorBranchName(rootBranchName, false), style.ColorBranchName(anchorBranchName, false), err)
	}
	rootReparented = true
	if err := ctx.Engine.RegisterWorktreeWithName(anchorBranchName, wtInfo.Path, wtInfo.Name); err != nil {
		cleanup()
		return "", fmt.Errorf("failed to register worktree under anchor %s: %w", style.ColorBranchName(anchorBranchName, false), err)
	}
	registered = true
	if err := ctx.Engine.UnregisterWorktree(ctx.Context, wtInfo.AnchorBranch); err != nil {
		cleanup()
		return "", fmt.Errorf("failed to remove legacy registration %s: %w", style.ColorBranchName(wtInfo.AnchorBranch, false), err)
	}
	return anchorBranchName, nil
}
