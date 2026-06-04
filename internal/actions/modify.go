package actions

import (
	"fmt"

	"github.com/getstackit/stackit/internal/actions/validation"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/tui/style"
	"github.com/getstackit/stackit/internal/utils"
)

// ModifyOptions contains options for the modify command
type ModifyOptions struct {
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
}

// ModifyAction performs the modify operation
func ModifyAction(ctx *app.Context, opts ModifyOptions) error {
	eng := ctx.Engine
	out := ctx.Output
	gctx := ctx.Context

	// Validate preconditions
	if err := validation.ModifyBranchChain(eng, "modify").Validate(); err != nil {
		return err
	}
	currentBranch := eng.CurrentBranch().GetName()
	currentBranchObj := eng.GetBranch(currentBranch)

	// Handle interactive rebase separately
	if opts.InteractiveRebase {
		return interactiveRebaseAction(ctx, opts)
	}

	// Check if rebase is in progress (only for non-interactive mode)
	if err := validation.MustNotHaveRebaseInProgress(gctx, ctx.Git()).Validate(); err != nil {
		return err
	}

	// Check if branch is empty when amending
	if !opts.CreateCommit {
		isEmpty, err := eng.IsBranchEmpty(gctx, currentBranch)
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
	if err := ctx.Engine.StageChanges(gctx, stagingOpts); err != nil {
		return err
	}

	commitMessage := opts.Message
	if commitMessage == "" && !utils.IsInteractive() && !opts.NoEdit {
		stdinMsg, err := utils.ReadFromStdin()
		if err == nil && stdinMsg != "" {
			commitMessage = stdinMsg
			opts.NoEdit = true
		}
	}

	// Check if there are staged changes
	hasStagedChanges, err := eng.HasStagedChanges(gctx)
	if err != nil {
		return fmt.Errorf("failed to check staged changes: %w", err)
	}

	if opts.CreateCommit {
		if !hasStagedChanges {
			return fmt.Errorf("no staged changes to commit. Use -a to stage all changes, or stage changes manually with 'git add'")
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
		NoVerify:    !ctx.Verify,
	}

	if err := eng.CommitWithOptions(gctx, commitOpts); err != nil {
		return fmt.Errorf("failed to commit: %w", err)
	}

	// Log success
	if opts.CreateCommit {
		out.Info("Created new commit in %s.", style.ColorBranchName(currentBranch, true))
	} else {
		out.Info("Amended commit in %s.", style.ColorBranchName(currentBranch, true))
	}

	// Restack upstack branches
	graph := eng.Graph(engine.SortStrategyAlphabetical)
	upstackBranches := graph.Range(currentBranchObj, engine.StackRange{RecursiveChildren: true})

	if len(upstackBranches) > 0 {
		out.Info("Restacking %d upstack branch(es)...", len(upstackBranches))
		if err := RestackBranches(ctx, upstackBranches); err != nil {
			return fmt.Errorf("failed to restack upstack branches: %w", err)
		}
	}

	return nil
}

// interactiveRebaseAction performs an interactive rebase on the branch's commits
func interactiveRebaseAction(ctx *app.Context, _ ModifyOptions) error {
	eng := ctx.Engine
	out := ctx.Output
	gctx := ctx.Context

	currentBranch := eng.CurrentBranch()

	// Get the parent branch to determine rebase base
	parent := currentBranch.GetParent()
	parentName := ""
	if parent == nil {
		parentName = eng.Trunk().GetName()
	} else {
		parentName = parent.GetName()
	}

	out.Info("Starting interactive rebase for %s onto %s...",
		style.ColorBranchName(currentBranch.GetName(), true),
		style.ColorBranchName(parentName, false))

	// Run interactive rebase
	if err := eng.InteractiveRebase(gctx, parentName); err != nil {
		// Check if rebase is in progress (conflict or user canceled)
		if eng.IsRebaseInProgress(gctx) {
			return fmt.Errorf("interactive rebase paused. Resolve conflicts and run 'git rebase --continue' or 'git rebase --abort'")
		}
		// Rebase might have been aborted by user
		return nil
	}

	out.Info("Interactive rebase completed.")

	// Restack upstack branches
	graph := eng.Graph(engine.SortStrategyAlphabetical)
	upstackBranches := graph.Range(*currentBranch, engine.StackRange{RecursiveChildren: true})

	if len(upstackBranches) > 0 {
		out.Info("Restacking %d upstack branch(es)...", len(upstackBranches))
		if err := RestackBranches(ctx, upstackBranches); err != nil {
			return fmt.Errorf("failed to restack upstack branches: %w", err)
		}
	}

	return nil
}
