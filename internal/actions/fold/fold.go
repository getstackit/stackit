// Package fold provides functionality for folding stacked branches.
package fold

import (
	"context"
	"errors"
	"fmt"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/actions/validation"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/output"
)

// Options contains options for the fold command
type Options struct {
	Keep       bool // If true, keeps the name of the current branch instead of using the name of its parent
	AllowTrunk bool // If true, allows folding into the trunk branch
	DryRun     bool // If true, only shows what would happen
}

// Engine lists exactly the engine methods fold calls: the git-operation
// precondition checks, the merge/delete/reparent mutations, and the reads the
// dry-run preview needs.
type Engine interface {
	validation.GitOperationEngine
	actions.OwnershipEngine
	Trunk() engine.Branch
	Graph(ctx context.Context, strategy engine.SortStrategy) *engine.StackGraph
	GetRevision(ctx context.Context, branch engine.Branch) (string, error)
	GetMergeBase(ctx context.Context, rev1, rev2 string) (string, error)
	ShowDiff(ctx context.Context, rr git.RevRange, format git.DiffFormat) (string, error)
	CheckoutBranch(ctx context.Context, branch engine.Branch) error
	Merge(ctx context.Context, revision string, opts engine.MergeOptions) error
	DeleteBranch(ctx context.Context, branch engine.Branch) error
	SetParent(ctx context.Context, branch engine.Branch, parentBranch engine.Branch, mode engine.DivergenceMode) error
	ReparentBranchesToParents(ctx context.Context, moves []engine.BranchParentMove, opts engine.ReparentOpts) error
}

func showDryRun(ctx context.Context, eng Engine, out output.Output, current, parent engine.Branch) {
	out.Info("%s", output.Yellow("Dry Run: Folding plan"))
	out.Info("  Fold branch: %s", output.CurrentBranch(current.Name()))
	out.Info("  Into parent: %s", output.BranchName(parent.Name()))
	out.Newline()

	// Show combined commit messages
	out.Info("%s", output.Cyan("Proposed Commit History:"))
	parentCommitsData, err := parent.AllCommits(ctx)
	parentCommits := parentCommitsData.Onelines()
	if err != nil {
		out.Debug("Failed to get parent commits for %s: %v", parent.Name(), err)
	}
	for _, commit := range parentCommits {
		out.Info("  %s", output.Dim(commit))
	}

	currentCommitsData, err := current.AllCommits(ctx)
	currentCommits := currentCommitsData.Onelines()
	if err != nil {
		out.Debug("Failed to get current commits for %s: %v", current.Name(), err)
	}
	for _, commit := range currentCommits {
		out.Info("  %s", commit)
	}
	out.Newline()

	// Show combined diff stat
	out.Info("%s", output.Cyan("Combined Diff Stat:"))
	// Base is parent's parent (or trunk)
	grandparentName := parent.ParentOrTrunk()
	baseRev, err := eng.GetRevision(ctx, eng.GetBranch(grandparentName))
	if err != nil {
		out.Debug("Failed to get revision for grandparent %s: %v", grandparentName, err)
		var mbErr error
		baseRev, mbErr = eng.GetMergeBase(ctx, eng.Trunk().Name(), parent.Name())
		if mbErr != nil {
			out.Debug("Failed to get merge base for %s: %v", parent.Name(), mbErr)
		}
	}

	headRev, err := current.Revision(ctx)
	if err != nil {
		out.Debug("Failed to get revision for current branch %s: %v", current.Name(), err)
	}

	diffStat, err := eng.ShowDiff(ctx, git.RevRange{Base: baseRev, Head: headRev}, git.DiffFormatStat)
	if err == nil && diffStat != "" {
		out.Info("%s", diffStat)
	} else {
		if err != nil {
			out.Debug("Failed to get diff stat: %v", err)
		}
		out.Info("  (No changes or error retrieving diff)")
	}

	out.Newline()
	out.Info("%s", output.Dim("No changes were applied."))
}

// Deps are the collaborators fold needs. Adapters build them from their own
// runtime state; the action never sees bootstrap types.
type Deps struct {
	Engine Engine
	actions.MutationDeps
}

// Action performs the fold operation
func Action(ctx context.Context, deps Deps, opts Options, handler Handler) error {
	eng, out := deps.Engine, deps.Out

	// Use null handler if none provided
	if handler == nil {
		handler = &NullHandler{}
	}
	defer handler.Cleanup()

	// Validate preconditions
	if err := validation.GitOperationChain(ctx, eng, "fold").Validate(); err != nil {
		return err
	}
	currentBranch := eng.CurrentBranch(ctx).Name()
	currentBranchObj := eng.GetBranch(currentBranch)

	// Get parent branch
	parentName := currentBranchObj.ParentOrTrunk()

	parentBranch := eng.GetBranch(parentName)
	if !parentBranch.IsTrunk() {
		if err := parentBranch.EnsureCanModify(); err != nil {
			return err
		}
	}
	if err := actions.EnsureCanModify(ctx, eng, deps.Here, currentBranchObj, parentBranch); err != nil {
		return err
	}

	// Take snapshot before modifying the repository, but after the ownership
	// guard, so a refusal leaves the undo stack untouched rather than evicting
	// a real snapshot with a no-op entry.
	snapshotOpts := actions.NewSnapshot("fold",
		actions.WithFlag(opts.Keep, "--keep"),
		actions.WithFlag(opts.AllowTrunk, "--allow-trunk"),
	)
	deps.Snapshots.Take(ctx, snapshotOpts)

	// Prohibit folding branches with different scopes
	if !parentBranch.IsTrunk() {
		currentScope := currentBranchObj.Scope()
		parentScope := parentBranch.Scope()
		if !currentScope.Equal(parentScope) {
			return fmt.Errorf("cannot fold branches with different scopes (current: [%s], parent: [%s])", currentScope.String(), parentScope.String())
		}
	}

	// Start handler with branch info
	handler.Start(currentBranch, parentName, opts.DryRun)

	if opts.DryRun {
		showDryRun(ctx, eng, out, currentBranchObj, parentBranch)
		return nil
	}

	if opts.Keep {
		// Prevent folding onto trunk with --keep, as that would delete trunk
		if parentBranch.IsTrunk() {
			return errors.New("cannot fold into trunk with --keep because it would delete the trunk branch")
		}
		if err := foldWithKeep(ctx, deps.Restack, currentBranchObj, parentBranch, eng, out, opts); err != nil {
			return err
		}
		// With --keep, the parent is folded into current, so parent is deleted
		handler.Complete(Result{
			FoldedBranch: parentName,
			IntoBranch:   currentBranch,
		})
		return nil
	}

	// Check if folding into trunk
	if parentBranch.IsTrunk() && !opts.AllowTrunk {
		return fmt.Errorf("cannot fold into trunk branch %s without --allow-trunk. Folding into trunk will modify your local main branch directly", parentName)
	}

	if err := foldNormal(ctx, deps.Restack, currentBranchObj, parentBranch, eng, out, opts); err != nil {
		return err
	}
	// Normal fold: current is folded into parent, so current is deleted
	handler.Complete(Result{
		FoldedBranch: currentBranch,
		IntoBranch:   parentName,
	})
	return nil
}
