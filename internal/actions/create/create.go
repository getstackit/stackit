// Package create provides functionality for creating new stacked branches.
package create

import (
	"fmt"
	"path/filepath"

	"stackit.dev/stackit/internal/actions"
	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/config"
	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/git"
	"stackit.dev/stackit/internal/utils"
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
	// Worktree creates a dedicated worktree for this stack (only valid from trunk)
	Worktree bool
}

// Action creates a new branch stacked on top of the current branch.
func Action(ctx *app.Context, opts Options, handler Handler) (Result, error) {
	eng := ctx.Engine
	out := ctx.Output

	// Use null handler if none provided
	if handler == nil {
		handler = &NullHandler{}
	}
	defer handler.Cleanup()

	// Get current branch
	currentBranch, err := eng.ValidateOnBranch()
	if err != nil {
		return Result{}, err
	}

	handler.Start(currentBranch)

	// Validate worktree flag - only allowed when creating from trunk
	if opts.Worktree {
		if !eng.IsTrunk(eng.GetBranch(currentBranch)) {
			return Result{}, fmt.Errorf("--worktree/-w flag is only valid when creating a new stack from trunk")
		}
	}

	// Take snapshot before modifying the repository
	snapshotOpts := actions.NewSnapshot("create",
		actions.WithArg(opts.BranchName),
		actions.WithFlagValue("-m", opts.Message),
		actions.WithFlagValue("--scope", opts.Scope),
		actions.WithFlag(opts.All, "--all"),
		actions.WithFlag(opts.Insert, "--insert"),
		actions.WithFlag(opts.Patch, "--patch"),
		actions.WithFlag(opts.Update, "--update"),
		actions.WithFlag(opts.Worktree, "--worktree"),
	)
	if err := eng.TakeSnapshot(snapshotOpts); err != nil {
		// Log but don't fail - snapshot is best effort
		out.Debug("Failed to take snapshot: %v", err)
	}

	// Handle staging first if we might need the message to name the branch
	hasStaged, err := eng.HasStagedChanges(ctx.Context)
	if err != nil {
		return Result{}, fmt.Errorf("failed to check staged changes: %w", err)
	}

	// Stage changes based on flags or prompt
	if opts.All || opts.Update || opts.Patch {
		handler.OnStep(StepStaging, StatusStarted, "Staging changes")
		stagingOpts := git.StagingOptions{
			All:    opts.All,
			Update: opts.Update,
			Patch:  opts.Patch,
		}
		if err := ctx.Git().StageChanges(ctx.Context, stagingOpts); err != nil {
			handler.OnStep(StepStaging, StatusFailed, err.Error())
			return Result{}, err
		}
		hasStaged = true
		handler.OnStep(StepStaging, StatusCompleted, "Changes staged")
	} else if !hasStaged && handler.IsInteractive() {
		hasUnstaged, err := eng.HasUnstagedChanges(ctx.Context)
		if err != nil {
			return Result{}, fmt.Errorf("failed to check unstaged changes: %w", err)
		}

		if hasUnstaged {
			confirmed, err := handler.PromptStageChanges()
			if err == nil && confirmed {
				handler.OnStep(StepStaging, StatusStarted, "Staging changes")
				if err := eng.StageAll(ctx.Context); err != nil {
					handler.OnStep(StepStaging, StatusFailed, err.Error())
					return Result{}, fmt.Errorf("failed to stage changes: %w", err)
				}
				hasStaged = true
				handler.OnStep(StepStaging, StatusCompleted, "Changes staged")
			}
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
	// Use provided scope if given, otherwise inherit from parent
	var scopeToUse string
	if opts.Scope != "" {
		scopeToUse = opts.Scope
	} else {
		parentScope := eng.GetScope(eng.GetBranch(currentBranch))
		scopeToUse = parentScope.String()
	}
	branch, err := determineBranch(ctx, &opts, commitMessage, scopeToUse)
	if err != nil {
		return Result{}, err
	}
	branchName := branch.GetName()

	// Check if branch already exists
	allBranches := eng.AllBranches()
	for _, existingBranch := range allBranches {
		if branch.Equal(existingBranch) {
			return Result{}, fmt.Errorf("branch %s already exists", branchName)
		}
	}

	// Create and checkout new branch
	handler.OnStep(StepBranchCreate, StatusStarted, fmt.Sprintf("Creating branch %s", branchName))
	if err := eng.CreateAndCheckoutBranch(ctx.Context, branch); err != nil {
		handler.OnStep(StepBranchCreate, StatusFailed, err.Error())
		return Result{}, fmt.Errorf("failed to create branch: %w", err)
	}
	handler.OnStep(StepBranchCreate, StatusCompleted, fmt.Sprintf("Created branch %s", branchName))

	// Commit if there are staged changes
	if hasStaged {
		handler.OnStep(StepCommit, StatusStarted, "Committing changes")
		if err := eng.Commit(ctx.Context, commitMessage, opts.Verbose, !ctx.Verify); err != nil {
			// Clean up branch on commit failure
			_ = eng.DeleteBranch(ctx.Context, branch)
			handler.OnStep(StepCommit, StatusFailed, err.Error())
			return Result{}, fmt.Errorf("failed to commit: %w", err)
		}
		handler.OnStep(StepCommit, StatusCompleted, "Changes committed")
	} else {
		handler.OnStep(StepCommit, StatusSkipped, "No staged changes")
		out.Info("No staged changes; created a branch with no commit.")
	}

	// Track the branch with current branch as parent
	handler.OnStep(StepTracking, StatusStarted, "Setting up branch tracking")
	if err := eng.TrackBranch(ctx.Context, branchName, currentBranch); err != nil {
		// Log error but don't fail - branch is created, just not tracked
		handler.OnStep(StepTracking, StatusFailed, err.Error())
		out.Info("Warning: failed to track branch: %v", err)
	} else {
		handler.OnStep(StepTracking, StatusCompleted, "Branch tracked")
	}

	ctx.Logger.Info("branch created", "name", branchName, "parent", currentBranch, "hasCommit", hasStaged)

	// Create worktree if requested
	var worktreePath string
	if opts.Worktree {
		handler.OnStep(StepWorktree, StatusStarted, "Creating worktree")
		// Checkout back to trunk first so we can create the worktree for the branch
		trunkBranch := eng.Trunk()
		if err := eng.CheckoutBranch(ctx.Context, trunkBranch); err != nil {
			out.Debug("Warning: failed to checkout trunk before creating worktree: %v", err)
		}

		var err error
		worktreePath, err = createWorktreeForStack(ctx, branchName)
		if err != nil {
			// Clean up branch on worktree failure
			_ = eng.DeleteBranch(ctx.Context, branch)
			handler.OnStep(StepWorktree, StatusFailed, err.Error())
			return Result{}, fmt.Errorf("failed to create worktree: %w", err)
		}
		handler.OnStep(StepWorktree, StatusCompleted, fmt.Sprintf("Created worktree at %s", worktreePath))
		out.Info("Created worktree at %s", worktreePath)
	}

	// Set scope: use provided scope if given, otherwise let it inherit from parent naturally
	if opts.Scope != "" {
		handler.OnStep(StepScope, StatusStarted, fmt.Sprintf("Setting scope to %s", opts.Scope))
		// Set explicit scope if provided
		newScope := engine.NewScope(opts.Scope)
		if err := eng.SetScope(branch, newScope); err != nil {
			handler.OnStep(StepScope, StatusFailed, err.Error())
			out.Info("Warning: failed to set scope: %v", err)
		} else {
			handler.OnStep(StepScope, StatusCompleted, fmt.Sprintf("Scope set to %s", opts.Scope))
		}
	}
	// If no scope provided, don't set anything - it will inherit from parent automatically

	// Handle insert logic
	if opts.Insert {
		handler.OnStep(StepInsert, StatusStarted, "Inserting branch into stack")
		if err := handleInsert(ctx.Context, branchName, currentBranch, ctx, &opts); err != nil {
			handler.OnStep(StepInsert, StatusFailed, err.Error())
			out.Info("Warning: failed to insert branch: %v", err)
		} else {
			handler.OnStep(StepInsert, StatusCompleted, "Branch inserted")
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
		ParentBranch: currentBranch,
		HasCommit:    hasStaged,
		WorktreePath: worktreePath,
	}
	handler.Complete(result)
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

// createWorktreeForStack creates a worktree for the given stack root and registers it
func createWorktreeForStack(ctx *app.Context, stackRoot string) (string, error) {
	eng := ctx.Engine

	// Get worktree base path from config, or use default
	cfg, _ := config.LoadConfig(ctx.RepoRoot)
	basePath := cfg.WorktreeBasePath()

	// Default: sibling directory named {repo}-stacks
	if basePath == "" {
		repoName := filepath.Base(ctx.RepoRoot)
		basePath = filepath.Join(filepath.Dir(ctx.RepoRoot), repoName+"-stacks")
	}

	// Worktree path: basePath/stackRoot
	worktreePath := filepath.Join(basePath, stackRoot)

	// Create the worktree (non-detached, pointing to the stack root branch)
	if err := eng.AddWorktree(ctx.Context, worktreePath, stackRoot, false); err != nil {
		return "", fmt.Errorf("failed to create worktree: %w", err)
	}

	// Register the worktree in local refs
	if err := eng.RegisterWorktree(stackRoot, worktreePath); err != nil {
		// Clean up worktree on registration failure
		_ = eng.RemoveWorktree(ctx.Context, worktreePath)
		return "", fmt.Errorf("failed to register worktree: %w", err)
	}

	return worktreePath, nil
}
