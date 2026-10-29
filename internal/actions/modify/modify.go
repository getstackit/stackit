// Package modify implements the modify command, which amends (or adds) a
// commit on the current branch or a downstack ancestor and restacks its
// descendants.
package modify

import (
	"context"
	"fmt"

	"github.com/getstackit/stackit/internal/term"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/actions/validation"
	"github.com/getstackit/stackit/internal/config"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/errors"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/output"
)

// Options contains options for the modify command
type Options struct {
	// Staging options
	All    bool // Stage all changes before committing (-a)
	Update bool // Stage updates to tracked files only (-u)
	Patch  bool // Pick hunks to stage interactively (-p)

	// Commit options
	CreateCommit bool   // Create a new commit instead of amending (-c)
	Message      string // Commit message (-m)
	Edit         bool   // Open editor to edit commit message (-e)
	NoEdit       bool   // Don't edit commit message (computed from flags)
	ResetAuthor  bool   // Reset author to current user
	Verbose      int    // Show diff in commit message template (-v)

	// Interactive rebase
	InteractiveRebase bool // Start interactive rebase on branch commits

	// Into targets a downstack ancestor of the current branch instead of the
	// current branch itself. Empty means "modify the current branch".
	Into string
}

// Engine lists exactly the engine methods modify calls: the branch
// validators, the ownership guard, and the checkout/stage/commit mutations.
type Engine interface {
	validation.GitOperationEngine
	actions.OwnershipEngine
	Trunk() engine.Branch
	Graph(ctx context.Context, strategy engine.SortStrategy) *engine.StackGraph
	ListWorktrees(ctx context.Context) (git.WorktreeList, error)
	CheckoutBranch(ctx context.Context, branch engine.Branch) error
	IsBranchEmpty(ctx context.Context, branchName string) (bool, error)
	StageChanges(ctx context.Context, opts git.StagingOptions) error
	Commit(ctx context.Context, opts git.CommitOptions) error
	InteractiveRebase(ctx context.Context, onto string) error
}

// Deps are the collaborators modify needs. Adapters build them from their own
// runtime state; the action never sees bootstrap types.
type Deps struct {
	Engine Engine
	actions.MutationDeps
	// RepoRoot locates the continuation state a restack conflict writes.
	RepoRoot string
	// Verify runs commit hooks; false passes --no-verify.
	Verify bool
}

// Action performs the modify operation
func Action(ctx context.Context, deps Deps, opts Options) (err error) {
	eng := deps.Engine
	out := deps.Out

	// Validate preconditions
	if err := validation.ModifyBranchChain(ctx, eng, "modify").Validate(); err != nil {
		return err
	}
	originalBranchName := eng.CurrentBranch(ctx).Name()

	if opts.Into != "" && opts.InteractiveRebase {
		return errors.New("--into cannot be combined with --interactive-rebase")
	}

	targetBranchName := originalBranchName
	if opts.Into != "" {
		if err := validateModifyIntoTarget(ctx, eng, originalBranchName, opts.Into); err != nil {
			return err
		}
		if err := ensureModifyTargetNotCheckedOutElsewhere(ctx, eng, opts.Into); err != nil {
			return err
		}
		targetBranchName = opts.Into
	}
	targetBranchObj := eng.GetBranch(targetBranchName)
	if err := actions.EnsureCanModifyNames(ctx, eng, deps.Here, originalBranchName, targetBranchName); err != nil {
		return err
	}

	// Take snapshot before modifying the repository. modify was the only
	// history-rewriting action without one, so `stackit undo` / `abort` fell
	// through to whatever unrelated command's snapshot happened to be latest --
	// restoring the wrong branch, or deleting branches created since it. This
	// matters most with --into, which rewrites a branch the user is not standing
	// on and then restacks its whole subtree.
	//
	// Deliberately above the interactive-rebase branch: that path rewrites
	// history too and needs the same safety net.
	//
	// modify amends the working tree into a commit before it restacks, so a
	// rollback that dropped the working tree would delete the user's
	// uncommitted work along with the commit. Only --all stages untracked
	// files (git add -A); every other mode commits what is already in the
	// index, which the tracked capture holds.
	capture := engine.WorktreeCaptureTracked
	if opts.All {
		capture = engine.WorktreeCaptureUntracked
	}
	snapshotOpts := actions.NewSnapshot("modify",
		actions.WithFlagValue("--into", opts.Into),
		actions.WithFlagValue("-m", opts.Message),
		actions.WithFlag(opts.CreateCommit, "--commit"),
		actions.WithFlag(opts.All, "--all"),
		actions.WithFlag(opts.Update, "--update"),
		actions.WithFlag(opts.Patch, "--patch"),
		actions.WithFlag(opts.InteractiveRebase, "--interactive-rebase"),
		actions.WithWorktreeCapture(capture),
	)
	deps.Snapshots.Take(ctx, snapshotOpts)

	// Handle interactive rebase separately
	if opts.InteractiveRebase {
		return interactiveRebaseAction(ctx, deps)
	}

	// Check if rebase is in progress (only for non-interactive mode)
	if err := validation.MustNotHaveRebaseInProgress(ctx, eng).Validate(); err != nil {
		return err
	}

	if targetBranchName != originalBranchName {
		if err := eng.CheckoutBranch(ctx, targetBranchObj); err != nil {
			if git.IsLocalChangesError(err) {
				return fmt.Errorf("cannot modify %s because your staged changes conflict with it; commit or stash them first, or use 'stackit absorb' to route the change to the commit that should own it", targetBranchName)
			}
			return fmt.Errorf("failed to checkout %s: %w", targetBranchName, err)
		}
		out.Info("Modifying downstack branch %s.", output.CurrentBranch(targetBranchName))
		defer func() {
			// A conflict workflow deliberately leaves HEAD detached (see
			// EnterConflictWorkflow); forcing a checkout back here would
			// defeat that safety measure. `stackit continue`/`abort` own
			// recovery from that state instead — so hand them the branch to
			// return to, or --into's "without checking it out, then returns"
			// contract quietly stops holding the moment a restack conflicts.
			if _, ok := errors.AsType[*errors.ConflictWorkflowError](err); ok {
				recordConflictReturnBranch(deps, originalBranchName)
				return
			}
			if checkoutErr := eng.CheckoutBranch(ctx, eng.GetBranch(originalBranchName)); checkoutErr != nil {
				out.Error("Failed to return to %s: %v", originalBranchName, checkoutErr)
			}
		}()
	}

	// Check if branch is empty when amending
	if !opts.CreateCommit {
		isEmpty, err := eng.IsBranchEmpty(ctx, targetBranchName)
		if err != nil {
			return fmt.Errorf("failed to check if branch is empty: %w", err)
		}
		if isEmpty {
			// If branch is empty, we must create a new commit
			opts.CreateCommit = true
			out.Info("Branch has no commits, creating new commit instead of amending.")
		}
	}

	// Stage changes based on flags
	stagingOpts := git.StagingOptions{
		All:    opts.All,
		Update: opts.Update,
		Patch:  opts.Patch,
	}
	if err := eng.StageChanges(ctx, stagingOpts); err != nil {
		return err
	}

	commitMessage := opts.Message
	if commitMessage == "" && !term.IsInteractive() && !opts.NoEdit {
		stdinMsg, err := term.ReadStdin()
		if err == nil && stdinMsg != "" {
			commitMessage = stdinMsg
			opts.NoEdit = true
		}
	}

	// Check if there are staged changes
	hasStagedChanges, err := eng.HasStagedChanges(ctx)
	if err != nil {
		return fmt.Errorf("failed to check staged changes: %w", err)
	}

	if opts.CreateCommit {
		if !hasStagedChanges {
			return errors.New("no staged changes to commit. Use -a to stage all changes, or stage changes manually with 'git add'")
		}
	} else {
		// When amending, skip if there are no staged changes and no message/edit/author changes
		hasContentChange := commitMessage != "" || opts.Edit || opts.ResetAuthor
		if !hasStagedChanges && !hasContentChange {
			out.Info("Nothing to modify.")
			return nil
		}
	}

	// Perform the commit
	commitOpts := git.CommitOptions{
		Amend:       !opts.CreateCommit,
		Message:     commitMessage,
		NoEdit:      opts.NoEdit,
		Edit:        opts.Edit,
		Verbose:     opts.Verbose,
		ResetAuthor: opts.ResetAuthor,
		NoVerify:    !deps.Verify,
	}

	if err := eng.Commit(ctx, commitOpts); err != nil {
		return fmt.Errorf("failed to commit: %w", err)
	}

	// Log success
	if opts.CreateCommit {
		out.Info("Created new commit in %s.", output.CurrentBranch(targetBranchName))
	} else {
		out.Info("Amended commit in %s.", output.CurrentBranch(targetBranchName))
	}

	// Restack upstack branches (rooted at the target so descendants of a
	// downstack --into target, including the original current branch, are covered)
	graph := eng.Graph(ctx, engine.SortStrategyAlphabetical)
	upstackBranches := graph.Range(targetBranchObj, engine.StackRange{RecursiveChildren: true})

	if len(upstackBranches) > 0 {
		out.Info("Restacking %d upstack branch(es)...", len(upstackBranches))
		if err := deps.Restack.Restack(ctx, upstackBranches); err != nil {
			return fmt.Errorf("failed to restack upstack branches: %w", err)
		}
	}

	return nil
}

// recordConflictReturnBranch tells the conflict workflow which branch to leave
// the user on once it finishes. The continuation state has just been written by
// EnterConflictWorkflow, so this reads it back and adds the one field it could
// not know about — the branch modify was invoked from, as opposed to the branch
// being rebased.
//
// Best-effort on purpose: failing to record where to return is a worse outcome
// to surface as a hard error than the conflict the user is already dealing
// with, and `continue` falls back to the rebased branch when it is unset.
func recordConflictReturnBranch(deps Deps, branchName string) {
	state, err := config.GetContinuationState(deps.RepoRoot)
	if err != nil {
		deps.Out.Debug("Failed to read continuation state to record return branch: %v", err)
		return
	}
	if state.CurrentBranchOverride == branchName {
		return
	}
	state.ReturnToBranch = branchName
	if err := config.PersistContinuationState(deps.RepoRoot, state); err != nil {
		deps.Out.Debug("Failed to record return branch %s: %v", branchName, err)
	}
}

// validateModifyIntoTarget ensures --into targets a genuine, modifiable
// downstack ancestor of the current branch.
func validateModifyIntoTarget(ctx context.Context, eng Engine, currentBranchName, targetName string) error {
	if !eng.BranchNames().Contains(targetName) {
		return fmt.Errorf("branch %s does not exist", targetName)
	}
	if targetName == currentBranchName {
		return fmt.Errorf("--into %s is the current branch; run modify without --into instead", targetName)
	}

	if err := (validation.Chain{
		validation.BranchMustNotBeTrunk(eng, targetName),
		validation.BranchMustBeTracked(eng, targetName),
		validation.BranchMustBeModifiable(eng, targetName),
	}).Validate(); err != nil {
		return err
	}

	graph := eng.Graph(ctx, engine.SortStrategyAlphabetical)
	downstack := graph.Downstack(eng.GetBranch(currentBranchName), engine.ExcludeCurrentBranch)
	if !downstack.Contains(targetName) {
		return fmt.Errorf("branch %s is not a downstack ancestor of %s", targetName, currentBranchName)
	}
	return nil
}

// ensureModifyTargetNotCheckedOutElsewhere refuses to modify a branch that is
// checked out in another worktree, pointing the user at that worktree instead
// of silently detaching HEAD there (which is what a plain CheckoutBranch would do).
func ensureModifyTargetNotCheckedOutElsewhere(ctx context.Context, eng Engine, targetName string) error {
	worktrees, err := eng.ListWorktrees(ctx)
	if err != nil {
		return fmt.Errorf("failed to check worktrees: %w", err)
	}
	if path := worktrees.PathForBranch(targetName); path != "" {
		return fmt.Errorf("branch %s is checked out in worktree %s; run 'stackit modify' from there, or 'cd %s && stackit modify'", targetName, path, path)
	}
	return nil
}

// interactiveRebaseAction performs an interactive rebase on the branch's commits
func interactiveRebaseAction(ctx context.Context, deps Deps) error {
	eng := deps.Engine
	out := deps.Out

	currentBranch := eng.CurrentBranch(ctx)

	// Get the parent branch to determine rebase base
	parent := currentBranch.Parent()
	parentName := ""
	if parent == nil {
		parentName = eng.Trunk().Name()
	} else {
		parentName = parent.Name()
	}

	out.Info("Starting interactive rebase for %s onto %s...",
		output.CurrentBranch(currentBranch.Name()),
		output.BranchName(parentName))

	// Run interactive rebase
	if err := eng.InteractiveRebase(ctx, parentName); err != nil {
		// Check if rebase is in progress (conflict or user canceled)
		if eng.IsRebaseInProgress(ctx) {
			return errors.New("interactive rebase paused. Resolve conflicts and run 'git rebase --continue' or 'git rebase --abort'")
		}
		// Rebase might have been aborted by user
		return nil
	}

	out.Info("Interactive rebase completed.")

	// Restack upstack branches
	graph := eng.Graph(ctx, engine.SortStrategyAlphabetical)
	upstackBranches := graph.Range(*currentBranch, engine.StackRange{RecursiveChildren: true})

	if len(upstackBranches) > 0 {
		out.Info("Restacking %d upstack branch(es)...", len(upstackBranches))
		if err := deps.Restack.Restack(ctx, upstackBranches); err != nil {
			return fmt.Errorf("failed to restack upstack branches: %w", err)
		}
	}

	return nil
}
