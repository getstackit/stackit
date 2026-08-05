// Package create provides functionality for creating new stacked branches.
package create

import (
	"fmt"
	"slices"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/actions/handler"
	"github.com/getstackit/stackit/internal/actions/validation"
	"github.com/getstackit/stackit/internal/actions/worktree"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/config"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/utils"
)

// Options contains options for the create command
type Options struct {
	BranchName    string
	Message       string
	Scope         string
	All           bool
	Insert        bool
	Patch         bool
	Update        bool
	Verbose       int
	BranchPattern config.BranchPattern
	// SelectedChildren is used to specify which children to move during insert
	// in non-interactive mode (mostly for tests)
	SelectedChildren []string
	// Onto creates the branch as a tracked child of this branch instead of the
	// current branch, without checking it out. Mutually exclusive with
	// Worktree and Insert.
	Onto string
	// Worktree creates a dedicated worktree for this stack (only valid from trunk)
	Worktree bool
	// AllowEmpty permits creating a branch with no commit when the working tree
	// has unstaged/untracked changes in non-interactive mode (otherwise that is
	// treated as a "forgot to stage" mistake and rejected).
	AllowEmpty bool
}

// Action creates a new branch stacked on top of the current branch.
func Action(ctx *app.Context, opts Options, h Handler) (Result, error) {
	eng := ctx.Engine
	out := ctx.Output

	// Use null handler if none provided
	if h == nil {
		h = &NullHandler{}
	}
	defer h.Cleanup()

	// Validate preconditions
	if err := validation.MustBeOnBranch(eng).Validate(); err != nil {
		return Result{}, err
	}
	currentBranch := eng.CurrentBranch().GetName()
	requestedParent := currentBranch
	if opts.Onto != "" {
		requestedParent = opts.Onto
	}
	if ctx.InManagedWorktree && ctx.WorktreeInfo != nil && requestedParent == eng.Trunk().GetName() {
		return Result{}, fmt.Errorf("cannot create a branch from trunk inside managed worktree %s; first check out a branch owned by this worktree", ctx.WorktreeInfo.Name)
	}
	if err := actions.EnsureCanModifyNamesHere(ctx, currentBranch); err != nil {
		return Result{}, err
	}

	h.Start(currentBranch)

	// Validate worktree flag - only allowed when creating from trunk
	if opts.Worktree {
		if !eng.IsTrunk(eng.GetBranch(currentBranch)) {
			return Result{}, fmt.Errorf("--worktree/-w flag is only valid when creating a new stack from trunk")
		}
	}

	// Validate --onto: it replaces the implicit "parent is the current
	// branch" behavior, so it can't be combined with flags that also depend
	// on that assumption.
	if opts.Onto != "" {
		if opts.Worktree {
			return Result{}, fmt.Errorf("--onto cannot be combined with --worktree/-w")
		}
		if opts.Insert {
			return Result{}, fmt.Errorf("--onto cannot be combined with --insert/-i")
		}
		if err := validateOntoTarget(eng, opts.Onto); err != nil {
			return Result{}, err
		}
		if err := actions.EnsureCanModifyNamesHere(ctx, opts.Onto); err != nil {
			return Result{}, err
		}
	}

	// Take snapshot before modifying the repository
	snapshotOpts := actions.NewSnapshot("create",
		actions.WithArg(opts.BranchName),
		actions.WithFlagValue("-m", opts.Message),
		actions.WithFlagValue("--scope", opts.Scope),
		actions.WithFlagValue("--onto", opts.Onto),
		actions.WithFlag(opts.All, "--all"),
		actions.WithFlag(opts.Insert, "--insert"),
		actions.WithFlag(opts.Patch, "--patch"),
		actions.WithFlag(opts.Update, "--update"),
		actions.WithFlag(opts.Worktree, "--worktree"),
		actions.WithFlag(opts.AllowEmpty, "--allow-empty"),
	)
	actions.TakeBestEffortSnapshot(ctx, snapshotOpts)

	// Handle staging first if we might need the message to name the branch
	hasStaged, err := eng.HasStagedChanges(ctx.Context)
	if err != nil {
		return Result{}, fmt.Errorf("failed to check staged changes: %w", err)
	}

	// Stage changes based on flags or prompt
	if opts.All || opts.Update || opts.Patch {
		h.OnStep(StepStaging, handler.StatusStarted, "Staging changes")
		stagingOpts := git.StagingOptions{
			All:    opts.All,
			Update: opts.Update,
			Patch:  opts.Patch,
		}
		if err := ctx.Engine.StageChanges(ctx.Context, stagingOpts); err != nil {
			h.OnStep(StepStaging, handler.StatusFailed, err.Error())
			return Result{}, err
		}
		hasStaged = true
		h.OnStep(StepStaging, handler.StatusCompleted, "Changes staged")
	} else if !hasStaged && h.IsInteractive() {
		hasUnstaged, err := eng.HasUnstagedChanges(ctx.Context)
		if err != nil {
			return Result{}, fmt.Errorf("failed to check unstaged changes: %w", err)
		}

		if hasUnstaged {
			confirmed, err := h.PromptStageChanges()
			if err == nil && confirmed {
				h.OnStep(StepStaging, handler.StatusStarted, "Staging changes")
				if err := eng.StageAll(ctx.Context); err != nil {
					h.OnStep(StepStaging, handler.StatusFailed, err.Error())
					return Result{}, fmt.Errorf("failed to stage changes: %w", err)
				}
				hasStaged = true
				h.OnStep(StepStaging, handler.StatusCompleted, "Changes staged")
			}
		}
	}

	// Guard against the common non-interactive footgun: the working tree has
	// changes but nothing is staged (e.g. forgot `git add`), which would
	// silently produce an empty branch. A genuinely clean tree still allows an
	// intentional empty scaffolding branch.
	if !hasStaged && !opts.AllowEmpty && !h.IsInteractive() {
		hasUnstaged, err := eng.HasUnstagedChanges(ctx.Context)
		if err != nil {
			return Result{}, fmt.Errorf("failed to check unstaged changes: %w", err)
		}
		hasUntracked, err := eng.HasUntrackedFiles(ctx.Context)
		if err != nil {
			return Result{}, fmt.Errorf("failed to check untracked files: %w", err)
		}
		if hasUnstaged || hasUntracked {
			return Result{}, fmt.Errorf("nothing staged but the working tree has changes; stage them with 'git add' (or pass --all/-a), or pass --allow-empty to create an empty branch")
		}
	}

	// Get commit message
	commitMessage := opts.Message
	// Get commit message for branch name generation (if needed)
	commitMessage, err = getCommitMessageForBranch(ctx, &opts, commitMessage)
	if err != nil {
		return Result{}, err
	}

	// Determine branch
	// The branch's parent is --onto's target when given, otherwise the current
	// branch. Resolved once, up front, so scope inheritance below and the
	// tracking/Result parent further down agree on the same parent.
	parentBranch := currentBranch
	if opts.Onto != "" {
		parentBranch = opts.Onto
	}

	// Use provided scope if given, otherwise inherit from parent
	var scopeToUse string
	if opts.Scope != "" {
		scopeToUse = opts.Scope
	} else {
		parentScope := eng.GetScope(eng.GetBranch(parentBranch))
		scopeToUse = parentScope.String()
	}

	// Check if pattern needs scope and we don't have one
	if opts.BranchPattern.ContainsScope() && scopeToUse == "" {
		if h.IsInteractive() {
			promptedScope, err := h.PromptScope(opts.BranchPattern.String())
			if err != nil {
				return Result{}, err
			}
			if promptedScope != "" {
				scopeToUse = promptedScope
				opts.Scope = promptedScope // Ensure it gets set in metadata
			}
		} else {
			return Result{}, fmt.Errorf("branch pattern contains {scope} but no scope provided; use --scope to set one")
		}
	}

	branch, err := determineBranch(ctx, &opts, commitMessage, scopeToUse)
	if err != nil {
		return Result{}, err
	}
	branchName := branch.GetName()

	// Check if branch already exists
	allBranches := eng.AllBranches()
	if slices.ContainsFunc(allBranches, branch.Equal) {
		return Result{}, fmt.Errorf("branch %s already exists", branchName)
	}

	// Create and checkout new branch. With --onto, the branch is created at
	// the target's tip SHA (not current HEAD) and tracked immediately, before
	// checkout, so the divergence point stackit records is the target's tip
	// rather than a merge-base that could wander into unrelated history the
	// current branch happens to share with the target.
	h.OnStep(StepBranchCreate, handler.StatusStarted, fmt.Sprintf("Creating branch %s", branchName))
	if opts.Onto != "" {
		ontoBranch := eng.GetBranch(opts.Onto)
		ontoSHA, err := ontoBranch.GetRevision()
		if err != nil {
			h.OnStep(StepBranchCreate, handler.StatusFailed, err.Error())
			return Result{}, fmt.Errorf("failed to resolve %s: %w", opts.Onto, err)
		}
		if err := eng.CreateBranch(ctx.Context, branchName, ontoSHA); err != nil {
			h.OnStep(StepBranchCreate, handler.StatusFailed, err.Error())
			return Result{}, fmt.Errorf("failed to create branch: %w", err)
		}
		if err := eng.TrackBranch(ctx.Context, branchName, opts.Onto); err != nil {
			_ = eng.DeleteBranch(ctx.Context, branch)
			h.OnStep(StepBranchCreate, handler.StatusFailed, err.Error())
			return Result{}, fmt.Errorf("failed to track branch: %w", err)
		}
		if err := eng.CheckoutBranch(ctx.Context, branch); err != nil {
			_ = eng.DeleteBranch(ctx.Context, branch)
			h.OnStep(StepBranchCreate, handler.StatusFailed, err.Error())
			return Result{}, fmt.Errorf("failed to check out %s onto %s: %w", branchName, opts.Onto, err)
		}
	} else if err := eng.CreateAndCheckoutBranch(ctx.Context, branch); err != nil {
		h.OnStep(StepBranchCreate, handler.StatusFailed, err.Error())
		return Result{}, fmt.Errorf("failed to create branch: %w", err)
	}
	h.OnStep(StepBranchCreate, handler.StatusCompleted, fmt.Sprintf("Created branch %s", branchName))

	// Commit if there are staged changes
	if hasStaged {
		h.OnStep(StepCommit, handler.StatusStarted, "Committing changes")
		if err := eng.Commit(ctx.Context, commitMessage, opts.Verbose, !ctx.Verify); err != nil {
			// Restore the original branch before deleting the new one so that
			// DeleteBranch doesn't fall back to trunk when cleaning up the
			// currently-checked-out branch (e.g. a git pre-commit hook failure).
			_ = eng.CheckoutBranch(ctx.Context, eng.GetBranch(currentBranch))
			_ = eng.DeleteBranch(ctx.Context, branch)
			h.OnStep(StepCommit, handler.StatusFailed, err.Error())
			return Result{}, fmt.Errorf("failed to commit: %w", err)
		}
		h.OnStep(StepCommit, handler.StatusCompleted, "Changes committed")
	} else {
		h.OnStep(StepCommit, handler.StatusSkipped, "No staged changes")
	}

	// Track the branch with its parent. With --onto, tracking already happened
	// before checkout (see above) so the divergence point is correct; here we
	// only need to track the common case, where the parent is the branch we
	// started from.
	if opts.Onto == "" {
		h.OnStep(StepTracking, handler.StatusStarted, "Setting up branch tracking")
		if err := eng.TrackBranch(ctx.Context, branchName, currentBranch); err != nil {
			// Log error but don't fail - branch is created, just not tracked
			h.OnStep(StepTracking, handler.StatusFailed, err.Error())
			out.Info("Warning: failed to track branch: %v", err)
		} else {
			h.OnStep(StepTracking, handler.StatusCompleted, "Branch tracked")
		}
	}

	// Opportunistically configure the metadata fetch refspec so a plain
	// `git fetch` keeps pulling branch metadata. Local, idempotent, and a no-op
	// when no remote is configured — closes the gap left by removing the implicit
	// bootstrap from engine construction (issue #1330).
	if err := eng.ConfigureRemoteMetadataSync(ctx.Context); err != nil {
		out.Debug("Failed to configure metadata refspec: %v", err)
	}

	ctx.Logger.Info("branch created name=%v parent=%v hasCommit=%v", branchName, parentBranch, hasStaged)

	// Create worktree if requested
	var worktreePath string
	if opts.Worktree {
		h.OnStep(StepWorktree, handler.StatusStarted, "Creating worktree")
		// Checkout back to trunk first so we can create the worktree for the branch
		trunkBranch := eng.Trunk()
		if err := eng.CheckoutBranch(ctx.Context, trunkBranch); err != nil {
			h.OnStep(StepWorktree, handler.StatusFailed, err.Error())
			out.Warn("Created %s, but could not create its worktree: %v", output.BranchName(branchName), err)
		} else {
			created, err := worktree.CreateAnchoredWorktreeForBranch(ctx, branchName, branchName, opts.Scope)
			if err != nil {
				h.OnStep(StepWorktree, handler.StatusFailed, err.Error())
				out.Warn("Created %s, but could not create its worktree: %v", output.BranchName(branchName), err)
			} else {
				worktreePath = created.Path
				h.OnStep(StepWorktree, handler.StatusCompleted, fmt.Sprintf("Created worktree at %s", worktreePath))
				out.Info("Created worktree at %s", worktreePath)
			}

			// Run post-create hooks in the worktree
			if hookErr := worktree.RunPostCreateHooks(ctx, worktreePath); hookErr != nil {
				out.Warn("Post-create hooks failed: %v", hookErr)
			}
		}
	}

	// Set scope: use provided scope if given, otherwise let it inherit from parent naturally
	if opts.Scope != "" {
		h.OnStep(StepScope, handler.StatusStarted, fmt.Sprintf("Setting scope to %s", opts.Scope))
		// Set explicit scope if provided
		newScope := engine.NewScope(opts.Scope)
		if err := eng.SetScope(ctx.Context, branch, newScope); err != nil {
			h.OnStep(StepScope, handler.StatusFailed, err.Error())
			out.Info("Warning: failed to set scope: %v", err)
		} else {
			h.OnStep(StepScope, handler.StatusCompleted, fmt.Sprintf("Scope set to %s", opts.Scope))
		}
	}
	// If no scope provided, don't set anything - it will inherit from parent automatically

	// Handle insert logic
	if opts.Insert {
		h.OnStep(StepInsert, handler.StatusStarted, "Inserting branch into stack")
		if err := handleInsert(ctx.Context, branchName, currentBranch, ctx, &opts); err != nil {
			h.OnStep(StepInsert, handler.StatusFailed, err.Error())
			out.Info("Warning: failed to insert branch: %v", err)
		} else {
			h.OnStep(StepInsert, handler.StatusCompleted, "Branch inserted")
		}

		// DX Improvement: Return to the original branch after insertion
		originalBranch := eng.GetBranch(currentBranch)
		if err := eng.CheckoutBranch(ctx.Context, originalBranch); err != nil {
			out.Info("Warning: failed to return to original branch %s: %v", currentBranch, err)
		} else {
			out.Info("Inserted %s and returned to %s.", branchName, currentBranch)
		}
	}

	result := Result{
		BranchName:   branchName,
		ParentBranch: parentBranch,
		HasCommit:    hasStaged,
		WorktreePath: worktreePath,
	}
	h.Complete(result)
	return result, nil
}

func determineBranch(ctx *app.Context, opts *Options, commitMessage string, scope string) (engine.Branch, error) {
	branchName := opts.BranchName
	if branchName == "" {
		// Get pattern from options (always valid, default applied in GetBranchPattern)
		pattern := opts.BranchPattern

		// Generate branch name from pattern
		var err error
		branchName, err = pattern.GetBranchName(ctx, commitMessage, scope)
		if err != nil {
			return engine.Branch{}, err
		}
	} else {
		// Sanitize provided branch name
		branchName = utils.SanitizeBranchName(branchName)
	}

	return ctx.Engine.GetBranch(branchName), nil
}

// validateOntoTarget checks that --onto refers to a branch the new branch can
// be safely parented on: it must exist, be trunk or a stackit-tracked branch
// (not a plain untracked git branch), not be a worktree anchor, and not be
// locked or frozen.
func validateOntoTarget(eng engine.Engine, ontoName string) error {
	ontoBranch := eng.GetBranch(ontoName)

	if ontoBranch.IsWorktreeAnchor() {
		return fmt.Errorf("cannot create a branch onto worktree anchor %q; use 'stackit create' inside that worktree instead", ontoName)
	}

	switch {
	case ontoBranch.IsTrunk():
		return nil
	case ontoBranch.IsTracked():
		return ontoBranch.EnsureCanModify()
	case eng.BranchNames().Contains(ontoName):
		return fmt.Errorf("branch %q is not tracked by stackit; track it first with 'stackit track', or choose a tracked branch", ontoName)
	default:
		return fmt.Errorf("branch %q does not exist", ontoName)
	}
}
