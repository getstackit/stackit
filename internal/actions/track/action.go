package track

import (
	"context"
	"errors"
	"fmt"

	"github.com/getstackit/stackit/internal/workpool"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/output"
)

// Options contains options for the track command
type Options struct {
	BranchName string
	Force      bool
	Parent     string
}

// Engine lists exactly the engine methods track calls. The interactive parent
// picker (Handler.PromptSelectParent) brings its own collaborators from the
// adapter.
type Engine interface {
	actions.OwnershipEngine
	CurrentBranch(ctx context.Context) *engine.Branch
	Trunk() engine.Branch
	AllBranches() engine.Branches
	BranchNames() *engine.BranchSet
	GetRevision(ctx context.Context, branch engine.Branch) (string, error)
	BatchRevisions(ctx context.Context, branches engine.Branches) engine.RevisionMap
	GetMergeBase(ctx context.Context, rev1, rev2 string) (string, error)
	IsAncestor(ctx context.Context, ancestor, descendant string) (bool, error)
	FindMostRecentTrackedAncestors(ctx context.Context, branchName string) ([]string, error)
	TrackBranch(ctx context.Context, branchName string, parentBranchName string) error
	ConfigureRemoteMetadataSync(ctx context.Context) error
}

// Deps are the collaborators track needs. Adapters build them from their own
// runtime state; the action never sees bootstrap types.
type Deps struct {
	Engine Engine
	Out    output.Output
	Here   actions.Here
}

// Action performs the track operation
func Action(ctx context.Context, deps Deps, opts Options, handler Handler) error {
	if handler == nil {
		handler = &NullHandler{}
	}
	defer handler.Cleanup()

	eng := deps.Engine
	branchName := opts.BranchName
	if branchName == "" {
		currentBranch := eng.CurrentBranch(ctx)
		if currentBranch != nil {
			branchName = currentBranch.Name()
		}
	}
	if branchName != "" {
		if err := actions.EnsureCanModifyNames(ctx, eng, deps.Here, branchName); err != nil {
			return err
		}
	}

	// Opportunistically configure the metadata fetch refspec so a plain
	// `git fetch` keeps pulling branch metadata. Local, idempotent, and a no-op
	// when no remote is configured — closes the gap left by removing the implicit
	// bootstrap from engine construction (issue #1330).
	if err := eng.ConfigureRemoteMetadataSync(ctx); err != nil {
		deps.Out.Debug("Failed to configure metadata refspec: %v", err)
	}

	// Handle --parent flag (single branch tracking)
	if opts.Parent != "" {
		parent := opts.Parent
		// Validate parent exists
		if parent != eng.Trunk().Name() && !eng.BranchNames().Contains(parent) {
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
			revisions := eng.BatchRevisions(ctx, engine.BranchesOf(parentBranch, eng.GetBranch(branchName)))
			parentRev, ok := revisions.Rev(parent)
			if !ok {
				return errors.New("failed to get parent revision")
			}
			branchRev, ok := revisions.Rev(branchName)
			if !ok {
				return errors.New("failed to get branch revision")
			}
			isAnc, err := eng.IsAncestor(ctx, parentRev, branchRev)
			if err != nil {
				return fmt.Errorf("failed to check ancestry: %w", err)
			}
			if !isAnc {
				return fmt.Errorf("parent branch %s is not an ancestor of %s (use --force to override)", parent, branchName)
			}
		}

		// Track the branch
		if err := eng.TrackBranch(ctx, branchName, parent); err != nil {
			return fmt.Errorf("failed to track branch: %w", err)
		}

		deps.Out.Info("Tracked %s with parent %s.", output.BranchName(branchName), output.BranchName(parent))
		return nil
	}

	// Handle --force flag (auto-detection without prompt)
	if opts.Force {
		ancestors, err := eng.FindMostRecentTrackedAncestors(ctx, branchName)
		if err != nil {
			return fmt.Errorf("failed to find tracked ancestor: %w", err)
		}
		if len(ancestors) == 0 {
			return fmt.Errorf("no tracked ancestors found for branch %s", branchName)
		}
		parentBranch := ancestors[0]

		if err := eng.TrackBranch(ctx, branchName, parentBranch); err != nil {
			return fmt.Errorf("failed to track branch: %w", err)
		}

		deps.Out.Info("Tracked %s with parent %s.", output.BranchName(branchName), output.BranchName(parentBranch))
		return nil
	}

	// Non-interactive mode requires --parent or --force
	if !handler.IsInteractive() {
		return errors.New("parent branch is required in non-interactive mode; use --parent or --force")
	}

	// Interactive mode: recursively track a stack
	return trackBranchRecursively(ctx, deps, branchName, handler)
}

// trackBranchRecursively interactively tracks a branch and its descendants
func trackBranchRecursively(ctx context.Context, deps Deps, branchName string, handler Handler) error {
	eng := deps.Engine

	// Check if branch is already tracked
	branch := eng.GetBranch(branchName)
	if branch.IsTracked() {
		deps.Out.Info("%s is already tracked.", output.BranchName(branchName))
		// Still ask if user wants to track descendants
	} else {
		// Try auto-detection (single unambiguous non-trunk tracked ancestor)
		var parentBranch string
		ancestors, err := eng.FindMostRecentTrackedAncestors(ctx, branchName)
		if err == nil && len(ancestors) == 1 && ancestors[0] != eng.Trunk().Name() {
			candidate := ancestors[0]
			candidateBranch := eng.GetBranch(candidate)
			// Skip worktree anchors for auto-detection
			if !candidateBranch.IsWorktreeAnchor() {
				parentBranch = candidate
				deps.Out.Info("Auto-detected parent %s for %s.", output.BranchName(parentBranch), output.BranchName(branchName))
			}
		}

		// If auto-detection didn't find a valid parent, prompt interactively
		if parentBranch == "" {
			parentBranch, err = handler.PromptSelectParent(ctx, branchName)
			if err != nil {
				return err
			}

			// Handle user cancellation (empty parent)
			if parentBranch == "" {
				deps.Out.Info("Track canceled.")
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
		if err := eng.TrackBranch(ctx, branchName, parentBranch); err != nil {
			return fmt.Errorf("failed to track branch: %w", err)
		}

		deps.Out.Info("Tracked %s with parent %s.", output.BranchName(branchName), output.BranchName(parentBranch))
	}

	// Find untracked children and ask to track them
	allBranches := eng.AllBranches()
	untrackedChildren := []string{}

	// branchRev is loop-invariant; a failed lookup just disables child detection.
	branchRev, revErr := eng.GetRevision(ctx, eng.GetBranch(branchName))
	if revErr == nil {
		// GetMergeBase spawns a git subprocess per call with no batch/cached
		// equivalent, so candidates are checked concurrently with a bounded
		// worker pool instead of serially. Each worker writes only its own
		// index, so isChild is assembled without synchronization.
		type indexedCandidate struct {
			index     int
			candidate engine.Branch
		}
		candidates := make([]indexedCandidate, 0, len(allBranches))
		for _, candidateBranch := range allBranches {
			if candidateBranch.Name() == branchName {
				continue
			}
			candidates = append(candidates, indexedCandidate{index: len(candidates), candidate: candidateBranch})
		}

		isChild := make([]bool, len(candidates))
		workpool.Run(candidates, func(item indexedCandidate) {
			mergeBase, err := eng.GetMergeBase(ctx, item.candidate.Name(), branchName)
			if err != nil {
				return
			}
			// If merge base is the branch we just tracked, candidate is a child
			isChild[item.index] = mergeBase == branchRev && !item.candidate.IsTracked()
		})

		for _, item := range candidates {
			if isChild[item.index] {
				untrackedChildren = append(untrackedChildren, item.candidate.Name())
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
			if err := trackBranchRecursively(ctx, deps, child, handler); err != nil {
				return err
			}
		}
	}

	return nil
}
