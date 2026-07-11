package track

import (
	"fmt"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/output"
)

// Options contains options for the track command
type Options struct {
	BranchName string
	Force      bool
	Parent     string
}

// Action performs the track operation
func Action(ctx *app.Context, opts Options, handler Handler) error {
	if handler == nil {
		handler = &NullHandler{}
	}
	defer handler.Cleanup()

	eng := ctx.Engine
	branchName := opts.BranchName

	// Opportunistically configure the metadata fetch refspec so a plain
	// `git fetch` keeps pulling branch metadata. Local, idempotent, and a no-op
	// when no remote is configured — closes the gap left by removing the implicit
	// bootstrap from engine construction (issue #1330).
	if err := eng.ConfigureRemoteMetadataSync(ctx.Context); err != nil {
		ctx.Output.Debug("Failed to configure metadata refspec: %v", err)
	}

	// Handle --parent flag (single branch tracking)
	if opts.Parent != "" {
		parent := opts.Parent
		// Validate parent exists
		if parent != eng.Trunk().GetName() && !eng.BranchNames().Contains(parent) {
			return fmt.Errorf("parent branch %s does not exist", parent)
		}

		// Validate parent is tracked (or is trunk)
		parentBranch := eng.GetBranch(parent)
		if !parentBranch.IsTrunk() && !parentBranch.IsTracked() {
			return fmt.Errorf("parent branch %s must be tracked (or be trunk)", parent)
		}

		// Prevent tracking with worktree anchor as parent
		if parentBranch.IsWorktreeAnchor() {
			return fmt.Errorf("parent branch %s is a worktree anchor; use 'stackit create' in the worktree instead", parent)
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
			isAnc, err := eng.IsAncestor(ctx.Context, parentRev, branchRev)
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

		ctx.Output.Info("Tracked %s with parent %s.", output.BranchName(branchName), output.BranchName(parent))
		return nil
	}

	// Handle --force flag (auto-detection without prompt)
	if opts.Force {
		ancestors, err := eng.FindMostRecentTrackedAncestors(ctx.Context, branchName)
		if err != nil {
			return fmt.Errorf("failed to find tracked ancestor: %w", err)
		}
		if len(ancestors) == 0 {
			return fmt.Errorf("no tracked ancestors found for branch %s", branchName)
		}
		parentBranch := ancestors[0]

		if err := eng.TrackBranch(ctx.Context, branchName, parentBranch); err != nil {
			return fmt.Errorf("failed to track branch: %w", err)
		}

		ctx.Output.Info("Tracked %s with parent %s.", output.BranchName(branchName), output.BranchName(parentBranch))
		return nil
	}

	// Non-interactive mode requires --parent or --force
	if !handler.IsInteractive() {
		return fmt.Errorf("parent branch is required in non-interactive mode; use --parent or --force")
	}

	// Interactive mode: recursively track a stack
	return trackBranchRecursively(ctx, branchName, handler)
}

// trackBranchRecursively interactively tracks a branch and its descendants
func trackBranchRecursively(ctx *app.Context, branchName string, handler Handler) error {
	eng := ctx.Engine

	// Check if branch is already tracked
	branch := eng.GetBranch(branchName)
	if branch.IsTracked() {
		ctx.Output.Info("%s is already tracked.", output.BranchName(branchName))
		// Still ask if user wants to track descendants
	} else {
		// Try auto-detection (single unambiguous non-trunk tracked ancestor)
		var parentBranch string
		ancestors, err := eng.FindMostRecentTrackedAncestors(ctx.Context, branchName)
		if err == nil && len(ancestors) == 1 && ancestors[0] != eng.Trunk().GetName() {
			candidate := ancestors[0]
			candidateBranch := eng.GetBranch(candidate)
			// Skip worktree anchors for auto-detection
			if !candidateBranch.IsWorktreeAnchor() {
				parentBranch = candidate
				ctx.Output.Info("Auto-detected parent %s for %s.", output.BranchName(parentBranch), output.BranchName(branchName))
			}
		}

		// If auto-detection didn't find a valid parent, prompt interactively
		if parentBranch == "" {
			parentBranch, err = handler.PromptSelectParent(ctx.Context, ctx.Engine, ctx.GitHub(), ctx.Logger, branchName)
			if err != nil {
				return err
			}

			// Handle user cancellation (empty parent)
			if parentBranch == "" {
				ctx.Output.Info("Track canceled.")
				return nil
			}

			// Validate parent is tracked (or is trunk)
			parentBranchObj := eng.GetBranch(parentBranch)
			if !parentBranchObj.IsTrunk() && !parentBranchObj.IsTracked() {
				return fmt.Errorf("parent branch %s must be tracked (or be trunk)", parentBranch)
			}

			// Prevent tracking with worktree anchor as parent
			if parentBranchObj.IsWorktreeAnchor() {
				return fmt.Errorf("parent branch %s is a worktree anchor; use 'stackit create' in the worktree instead", parentBranch)
			}
		}

		// Track the branch
		if err := eng.TrackBranch(ctx.Context, branchName, parentBranch); err != nil {
			return fmt.Errorf("failed to track branch: %w", err)
		}

		ctx.Output.Info("Tracked %s with parent %s.", output.BranchName(branchName), output.BranchName(parentBranch))
	}

	// Find untracked children and ask to track them
	allBranches := eng.AllBranches()
	untrackedChildren := []string{}

	// branchRev is loop-invariant; a failed lookup just disables child detection.
	branchRev, revErr := eng.GetRevision(eng.GetBranch(branchName))
	if revErr == nil {
		for _, candidateBranch := range allBranches {
			candidate := candidateBranch.GetName()
			if candidate == branchName {
				continue
			}

			// Check if candidate is a child (has this branch as merge base)
			mergeBase, err := eng.GetMergeBase(ctx.Context, candidate, branchName)
			if err != nil {
				continue
			}

			// If merge base is the branch we just tracked, candidate is a child
			if mergeBase == branchRev && !candidateBranch.IsTracked() {
				untrackedChildren = append(untrackedChildren, candidate)
			}
		}
	}

	// Recursively track children
	for _, child := range untrackedChildren {
		// Ask if user wants to track this child
		shouldTrack, err := handler.PromptTrackChild(child, branchName)
		if err != nil {
			return err
		}

		if shouldTrack {
			if err := trackBranchRecursively(ctx, child, handler); err != nil {
				return err
			}
		}
	}

	return nil
}
