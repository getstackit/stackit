package actions

import (
	"fmt"

	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/tui"
	"stackit.dev/stackit/internal/tui/style"
	"stackit.dev/stackit/internal/utils"
)

// TrackOptions contains options for the track command
type TrackOptions struct {
	BranchName string
	Force      bool
	Parent     string
}

// TrackAction performs the track operation
func TrackAction(ctx *app.Context, opts TrackOptions) error {
	eng := ctx.Engine
	branchName := opts.BranchName

	// Handle --parent flag (single branch tracking)
	if opts.Parent != "" {
		parent := opts.Parent
		// Validate parent exists (refresh branch list if needed)
		allBranches := eng.AllBranches()
		parentExists := false
		for _, branch := range allBranches {
			if branch.GetName() == parent {
				parentExists = true
				break
			}
		}
		if !parentExists && parent != eng.Trunk().GetName() {
			// Refresh branches list and check again
			allBranches := eng.AllBranches()
			parentExists = false
			for _, b := range allBranches {
				if b.GetName() == parent {
					parentExists = true
					break
				}
			}
			if !parentExists {
				return fmt.Errorf("parent branch %s does not exist", parent)
			}
		}

		// Validate parent is tracked (or is trunk)
		parentBranch := eng.GetBranch(parent)
		if !parentBranch.IsTrunk() && !parentBranch.IsTracked() {
			return fmt.Errorf("parent branch %s must be tracked (or be trunk)", parent)
		}

		// Validate parent is an ancestor (unless force is used)
		if !opts.Force {
			parentRev, err := eng.GetRevision(eng.GetBranch(parent))
			if err != nil {
				return fmt.Errorf("failed to get parent revision: %w", err)
			}
			branchRev, err := eng.GetRevision(eng.GetBranch(branchName))
			if err != nil {
				return fmt.Errorf("failed to get branch revision: %w", err)
			}
			isAnc, err := eng.IsAncestor(parentRev, branchRev)
			if err != nil {
				return fmt.Errorf("failed to check ancestry: %w", err)
			}
			if !isAnc {
				return fmt.Errorf("parent branch %s is not an ancestor of %s (use --force to override)", parent, branchName)
			}
		}

		// Track the branch
		if err := eng.TrackBranch(ctx.Context, branchName, parent); err != nil {
			return fmt.Errorf("failed to track branch: %w", err)
		}

		ctx.Output.Info("Tracked %s with parent %s.", style.ColorBranchName(branchName, false), style.ColorBranchName(parent, false))
		return nil
	}

	// Handle --force flag (auto-detection without prompt)
	if opts.Force {
		ancestors, err := eng.FindMostRecentTrackedAncestors(ctx.Context, branchName)
		if err != nil {
			return fmt.Errorf("failed to find tracked ancestor: %w", err)
		}
		parentBranch := ancestors[0]

		if err := eng.TrackBranch(ctx.Context, branchName, parentBranch); err != nil {
			return fmt.Errorf("failed to track branch: %w", err)
		}

		ctx.Output.Info("Tracked %s with parent %s.", style.ColorBranchName(branchName, false), style.ColorBranchName(parentBranch, false))
		return nil
	}

	// Non-interactive mode requires --parent or --force
	if !utils.IsInteractive() {
		return fmt.Errorf("parent branch is required in non-interactive mode; use --parent or --force")
	}

	// Interactive mode: recursively track a stack
	return trackBranchRecursively(ctx, branchName)
}

// trackBranchRecursively interactively tracks a branch and its descendants
func trackBranchRecursively(ctx *app.Context, branchName string) error {
	eng := ctx.Engine

	// Check if branch is already tracked
	branch := eng.GetBranch(branchName)
	if branch.IsTracked() {
		ctx.Output.Info("%s is already tracked.", style.ColorBranchName(branchName, false))
		// Still ask if user wants to track descendants
	} else {
		// Try auto-detection (single unambiguous non-trunk tracked ancestor)
		var parentBranch string
		ancestors, err := eng.FindMostRecentTrackedAncestors(ctx.Context, branchName)
		if err == nil && len(ancestors) == 1 && ancestors[0] != eng.Trunk().GetName() {
			parentBranch = ancestors[0]
			ctx.Output.Info("Auto-detected parent %s for %s.", style.ColorBranchName(parentBranch, false), style.ColorBranchName(branchName, false))
		} else {
			// Select parent interactively
			parentBranch, err = selectParentBranch(ctx, branchName)
			if err != nil {
				return err
			}
		}

		// Track the branch
		if err := eng.TrackBranch(ctx.Context, branchName, parentBranch); err != nil {
			return fmt.Errorf("failed to track branch: %w", err)
		}

		ctx.Output.Info("Tracked %s with parent %s.", style.ColorBranchName(branchName, false), style.ColorBranchName(parentBranch, false))
	}

	// Find untracked children and ask to track them
	allBranches := eng.AllBranches()
	untrackedChildren := []string{}

	for _, candidateBranch := range allBranches {
		candidate := candidateBranch.GetName()
		if candidate == branchName {
			continue
		}

		// Check if candidate is a child (has this branch as merge base)
		mergeBase, err := eng.GetMergeBase(candidate, branchName)
		if err != nil {
			continue
		}

		branchRev, err := eng.GetRevision(eng.GetBranch(branchName))
		if err != nil {
			continue
		}

		// If merge base is the branch we just tracked, candidate is a child
		if mergeBase == branchRev && !candidateBranch.IsTracked() {
			untrackedChildren = append(untrackedChildren, candidate)
		}
	}

	// Recursively track children
	for _, child := range untrackedChildren {
		// Ask if user wants to track this child
		shouldTrack, err := promptTrackChild(child, branchName)
		if err != nil {
			return err
		}

		if shouldTrack {
			if err := trackBranchRecursively(ctx, child); err != nil {
				return err
			}
		}
	}

	return nil
}

// selectParentBranch interactively selects a parent branch for tracking
func selectParentBranch(ctx *app.Context, branchName string) (string, error) {
	eng := ctx.Engine

	// Show interactive selector
	selected, err := tui.PromptLogSelect(ctx.Context, ctx.Engine, ctx.GitHubClient, tui.LogOptions{
		Style:  "FULL",
		Logger: ctx.Logger,
		Exclude: map[string]bool{
			branchName: true,
		},
	})
	if err != nil {
		return "", err
	}

	// Validate parent is tracked (or is trunk)
	parentBranch := eng.GetBranch(selected)
	if !parentBranch.IsTrunk() && !parentBranch.IsTracked() {
		return "", fmt.Errorf("parent branch %s must be tracked (or be trunk)", selected)
	}

	return selected, nil
}

// promptTrackChild asks if user wants to track a child branch
func promptTrackChild(childName, parentName string) (bool, error) {
	message := fmt.Sprintf("Found untracked child branch %s of %s. Track it?", style.ColorBranchName(childName, false), style.ColorBranchName(parentName, false))
	options := []tui.SelectOption{
		{Label: "Yes", Value: yesResponse},
		{Label: "No", Value: noResponse},
	}

	selected, err := tui.PromptSelect(message, options, 0)
	if err != nil {
		return false, err
	}

	return selected == yesResponse, nil
}
