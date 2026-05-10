// Package worktree provides actions for managing stackit-managed worktrees.
package worktree

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/config"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/tui/style"
)

// ListOptions contains options for the list action
type ListOptions struct {
	// No options for now
}

// ListResult contains the results of listing worktrees
type ListResult struct {
	Worktrees     []Entry
	CurrentAnchor string // Anchor branch of the worktree we're currently in (if any)
}

// Entry represents a single managed worktree
type Entry struct {
	Name          string // User-provided name (empty for legacy worktrees)
	AnchorBranch  string // Anchor branch name
	Path          string
	Exists        bool   // Whether the path still exists on disk
	StackSize     int    // Number of branches in the stack (excluding anchor)
	CurrentBranch string // Branch currently checked out in this worktree
	IsDirty       bool   // Has uncommitted changes
}

// ListAction lists all managed worktrees
func ListAction(ctx *app.Context, _ ListOptions) (*ListResult, error) {
	worktrees, err := ctx.Engine.ListManagedWorktrees()
	if err != nil {
		return nil, fmt.Errorf("failed to list managed worktrees: %w", err)
	}

	result := &ListResult{
		Worktrees: make([]Entry, 0, len(worktrees)),
	}

	// Check if we're in a managed worktree
	if ctx.InManagedWorktree && ctx.WorktreeInfo != nil {
		result.CurrentAnchor = ctx.WorktreeInfo.AnchorBranch
	}

	// Build stack graph once to get stack sizes
	graph := ctx.Engine.Graph(engine.SortStrategyAlphabetical)

	for _, wt := range worktrees {
		entry := Entry{
			Name:         wt.Name,
			AnchorBranch: wt.AnchorBranch,
			Path:         wt.Path,
			Exists:       true,
		}

		// Check if path exists
		if _, statErr := os.Stat(wt.Path); os.IsNotExist(statErr) {
			entry.Exists = false
			result.Worktrees = append(result.Worktrees, entry)
			continue
		}

		// Get stack size (descendants of anchor branch)
		anchorBranch := ctx.Engine.GetBranch(wt.AnchorBranch)
		if anchorBranch.IsTracked() {
			descendants := graph.Range(anchorBranch, engine.StackRange{
				RecursiveChildren: true,
				IncludeCurrent:    false,
			})
			entry.StackSize = len(descendants)
		}

		// Get current branch in worktree
		currentBranch, err := ctx.Git().GetWorktreeCurrentBranch(ctx.Context, wt.Path)
		if err == nil && currentBranch != "" {
			entry.CurrentBranch = currentBranch
		}

		// Check for uncommitted changes
		isDirty, err := ctx.Git().WorktreeHasUncommittedChanges(ctx.Context, wt.Path)
		if err == nil {
			entry.IsDirty = isDirty
		}

		result.Worktrees = append(result.Worktrees, entry)
	}

	return result, nil
}

// RemoveOptions contains options for the remove action
type RemoveOptions struct {
	AnchorBranch string // Anchor branch name to remove worktree for
	Force        bool   // Force removal even if worktree has uncommitted changes
	KeepBranch   bool   // Keep the anchor branch instead of deleting it
}

// findWorktreeByNameOrBranch looks up a worktree by name or anchor branch
func findWorktreeByNameOrBranch(ctx *app.Context, nameOrBranch string) (*engine.WorktreeInfo, error) {
	// First try by anchor branch (original behavior)
	wtInfo, err := ctx.Engine.GetWorktreeForStack(nameOrBranch)
	if err != nil {
		return nil, fmt.Errorf("failed to get worktree info: %w", err)
	}
	if wtInfo != nil {
		return wtInfo, nil
	}

	// If not found, try to find by worktree name
	worktrees, err := ctx.Engine.ListManagedWorktrees()
	if err != nil {
		return nil, fmt.Errorf("failed to list worktrees: %w", err)
	}
	for _, wt := range worktrees {
		if wt.Name == nameOrBranch {
			return &wt, nil
		}
	}

	return nil, fmt.Errorf("no worktree found for %s", style.ColorBranchName(nameOrBranch, false))
}

// RemoveAction removes a worktree for a stack
func RemoveAction(ctx *app.Context, opts RemoveOptions) error {
	out := ctx.Output

	// Get worktree info (supports lookup by name or anchor branch)
	wtInfo, err := findWorktreeByNameOrBranch(ctx, opts.AnchorBranch)
	if err != nil {
		return err
	}

	// Check if path exists before trying to remove
	if _, statErr := os.Stat(wtInfo.Path); statErr == nil {
		// Try to remove the git worktree
		if removeErr := ctx.Engine.RemoveWorktree(ctx.Context, wtInfo.Path); removeErr != nil {
			if !opts.Force {
				return fmt.Errorf("failed to remove worktree at %s: %w (use --force to override)", wtInfo.Path, removeErr)
			}
			out.Warn("Failed to remove worktree directory, continuing with unregistration: %v", removeErr)
		}
	} else {
		out.Debug("Worktree path %s does not exist, skipping removal", wtInfo.Path)
		// Prune stale git worktree references to allow branch deletion later
		if pruneErr := ctx.Engine.PruneWorktrees(ctx.Context); pruneErr != nil {
			out.Debug("Failed to prune worktrees: %v", pruneErr)
		}
	}

	// Unregister from registry (use the anchor branch from worktree info)
	if unregErr := ctx.Engine.UnregisterWorktree(wtInfo.AnchorBranch); unregErr != nil {
		return fmt.Errorf("failed to unregister worktree: %w", unregErr)
	}

	// Delete anchor branch unless --keep-branch is set
	if !opts.KeepBranch {
		anchorBranch := ctx.Engine.GetBranch(wtInfo.AnchorBranch)
		if anchorBranch.IsTracked() {
			// Check for children using stack graph
			graph := ctx.Engine.Graph(engine.SortStrategyAlphabetical)
			children := graph.Children(anchorBranch)
			if len(children) > 0 {
				out.Warn("Anchor branch %s has children, not deleting", style.ColorBranchName(wtInfo.AnchorBranch, false))
			} else {
				if err := ctx.Engine.DeleteBranch(ctx.Context, anchorBranch); err != nil {
					out.Warn("Failed to delete anchor branch: %v", err)
				} else {
					out.Debug("Deleted anchor branch %s", wtInfo.AnchorBranch)
				}
			}
		}
	}

	out.Success("Removed worktree for stack %s", style.ColorBranchName(wtInfo.AnchorBranch, false))
	return nil
}

// OpenOptions contains options for the open action
type OpenOptions struct {
	AnchorBranch string // Anchor branch name to get path for
}

// OpenAction returns the path to a worktree for a stack
func OpenAction(ctx *app.Context, opts OpenOptions) (string, error) {
	wtInfo, err := findWorktreeByNameOrBranch(ctx, opts.AnchorBranch)
	if err != nil {
		return "", err
	}

	// Check if path exists
	if _, statErr := os.Stat(wtInfo.Path); os.IsNotExist(statErr) {
		return "", fmt.Errorf("worktree path %s does not exist (worktree may have been manually deleted)", wtInfo.Path)
	}

	return wtInfo.Path, nil
}

// CreateOptions contains options for the create action
type CreateOptions struct {
	Name  string // User-provided name for the worktree
	Scope string // Optional scope to set on the anchor branch
}

// CreateResult contains the results of creating a worktree
type CreateResult struct {
	Name         string // The name of the worktree
	AnchorBranch string // The name of the anchor branch
	Path         string // The path to the worktree
}

// CreateAction creates a new worktree with an anchor branch
func CreateAction(ctx *app.Context, opts CreateOptions) (*CreateResult, error) {
	eng := ctx.Engine
	out := ctx.Output
	repoRoot := ctx.RepoRoot

	// If we're in a managed worktree, we need to create the new worktree from the main repo
	if ctx.InManagedWorktree && ctx.WorktreeInfo != nil {
		out.Info("Creating worktree from main repository (currently in worktree: %s)", ctx.WorktreeInfo.Name)

		// Create a temporary engine for the main repository
		mainRepoRoot := ctx.WorktreeInfo.MainRepoDir
		mainGit := git.NewRunnerWithPath(mainRepoRoot, ctx.Logger)

		// Load config from main repo for trunk and undo settings
		mainCfg, err := config.LoadConfig(mainRepoRoot)
		if err != nil {
			return nil, fmt.Errorf("failed to load config from main repo: %w", err)
		}

		mainEng, err := engine.NewEngine(engine.Options{
			RepoRoot:          mainRepoRoot,
			Trunk:             mainCfg.Trunk(),
			MaxUndoStackDepth: mainCfg.UndoStackDepth(),
			Git:               mainGit,
			Writer:            os.Stderr,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create engine for main repo: %w", err)
		}

		// Use the main repo engine for the rest of the operation
		eng = mainEng
		repoRoot = mainRepoRoot
	}

	trunk := eng.Trunk()

	// Check if trunk is behind remote and warn
	if status, err := eng.GetBranchRemoteStatus(trunk); err == nil && status.Behind() {
		out.Warn("Local %s is behind remote. Consider running 'st sync' first.", trunk.GetName())
	}

	// Inform user if not on trunk (worktree is always created from trunk)
	currentBranch := eng.CurrentBranch()
	if currentBranch == nil {
		out.Info("Creating worktree from %s (currently in detached HEAD state)", trunk.GetName())
	} else if currentBranch.GetName() != trunk.GetName() {
		out.Info("Creating worktree from %s (current branch: %s)", trunk.GetName(), currentBranch.GetName())
	}

	// Validate name
	if opts.Name == "" {
		return nil, fmt.Errorf("worktree name is required")
	}

	// Validate name doesn't contain path separators or other problematic characters
	if strings.ContainsAny(opts.Name, "/\\:*?\"<>|") {
		return nil, fmt.Errorf("worktree name cannot contain path separators or special characters: /\\:*?\"<>|")
	}

	// Check for duplicate worktree names
	existingWorktrees, err := eng.ListManagedWorktrees()
	if err != nil {
		return nil, fmt.Errorf("failed to check existing worktrees: %w", err)
	}
	for _, wt := range existingWorktrees {
		if wt.Name == opts.Name {
			return nil, fmt.Errorf("worktree with name %q already exists", opts.Name)
		}
	}

	// Generate anchor branch name using the branch naming pattern
	cfg, _ := config.LoadConfig(repoRoot)
	patternStr := cfg.BranchNamePattern()
	pattern, err := config.NewBranchPattern(patternStr)
	if err != nil {
		return nil, fmt.Errorf("invalid branch pattern: %w", err)
	}

	// Use the name as the "message" for the branch pattern, with "-wt" suffix
	anchorBranchName, err := pattern.GetBranchName(ctx, opts.Name+"-wt", opts.Scope)
	if err != nil {
		return nil, fmt.Errorf("failed to generate anchor branch name: %w", err)
	}

	// Check if branch already exists
	if eng.BranchNames().Contains(anchorBranchName) {
		return nil, fmt.Errorf("branch %s already exists", anchorBranchName)
	}

	// Get trunk SHA for the anchor branch
	trunkSHA, err := trunk.GetRevision()
	if err != nil {
		return nil, fmt.Errorf("failed to get trunk revision: %w", err)
	}

	// Create the anchor branch at trunk HEAD
	if err := eng.CreateBranch(ctx.Context, anchorBranchName, trunkSHA); err != nil {
		return nil, fmt.Errorf("failed to create anchor branch: %w", err)
	}

	// Set up metadata: parent = trunk, branchType = worktree-anchor, scope = provided
	anchorBranch := eng.GetBranch(anchorBranchName)
	if err := eng.SetParent(ctx.Context, anchorBranch, trunk); err != nil {
		// Clean up branch on failure
		cleanupAnchorBranch(ctx.Context, eng, anchorBranchName, out)
		return nil, fmt.Errorf("failed to set parent: %w", err)
	}

	if err := eng.SetBranchType(anchorBranch, git.BranchTypeWorktreeAnchor); err != nil {
		cleanupAnchorBranch(ctx.Context, eng, anchorBranchName, out)
		return nil, fmt.Errorf("failed to set branch type: %w", err)
	}

	if opts.Scope != "" {
		if err := eng.SetScope(ctx.Context, anchorBranch, engine.NewScope(opts.Scope)); err != nil {
			cleanupAnchorBranch(ctx.Context, eng, anchorBranchName, out)
			return nil, fmt.Errorf("failed to set scope: %w", err)
		}
	}

	// Create the worktree
	worktreePath, err := createWorktreeForAnchor(ctx, eng, repoRoot, opts.Name, anchorBranchName)
	if err != nil {
		cleanupAnchorBranch(ctx.Context, eng, anchorBranchName, out)
		return nil, err
	}

	out.Success("Created worktree %s", style.ColorBranchName(opts.Name, false))
	out.Info("  Anchor branch: %s", style.ColorBranchName(anchorBranchName, false))
	out.Info("  Path: %s", style.ColorDim(worktreePath))
	if opts.Scope != "" {
		out.Info("  Scope: %s", style.ColorDim(opts.Scope))
	}
	out.Newline()

	// Run post-create hooks
	if err := RunPostCreateHooks(ctx, worktreePath); err != nil {
		out.Warn("Post-create hooks failed: %v", err)
	}

	return &CreateResult{
		Name:         opts.Name,
		AnchorBranch: anchorBranchName,
		Path:         worktreePath,
	}, nil
}

// createWorktreeForAnchor creates a worktree for the given anchor branch and registers it
func createWorktreeForAnchor(ctx *app.Context, eng engine.Engine, repoRoot string, name string, anchorBranch string) (string, error) {
	// Get worktree base path from config, or use default
	cfg, _ := config.LoadConfig(repoRoot)
	basePath := cfg.WorktreeBasePath()

	// Default: sibling directory named {repo}-stacks
	if basePath == "" {
		repoName := filepath.Base(repoRoot)
		basePath = filepath.Join(filepath.Dir(repoRoot), repoName+"-stacks")
	}

	// Worktree path: basePath/name
	worktreePath := filepath.Join(basePath, name)

	// Check if path already exists
	if _, err := os.Stat(worktreePath); err == nil {
		return "", fmt.Errorf("worktree path %s already exists; remove it first or choose a different name", worktreePath)
	}

	// Create the worktree (non-detached, pointing to the anchor branch)
	if err := eng.AddWorktree(ctx.Context, worktreePath, anchorBranch, false); err != nil {
		return "", fmt.Errorf("failed to create worktree: %w", err)
	}

	// Register the worktree in local refs with the name
	if err := eng.RegisterWorktreeWithName(anchorBranch, worktreePath, name); err != nil {
		// Clean up worktree on registration failure
		_ = eng.RemoveWorktree(ctx.Context, worktreePath)
		return "", fmt.Errorf("failed to register worktree: %w", err)
	}

	return worktreePath, nil
}

// cleanupAnchorBranch cleans up an anchor branch on failure and logs any cleanup errors
func cleanupAnchorBranch(ctx context.Context, eng engine.Engine, branchName string, out output.Output) {
	if err := eng.DeleteBranch(ctx, eng.GetBranch(branchName)); err != nil {
		out.Warn("Failed to clean up anchor branch %s: %v", branchName, err)
	}
}

// PruneOptions contains options for the prune action
type PruneOptions struct {
	DryRun bool // If true, only show what would be pruned
}

// PruneResult contains the results of pruning worktrees
type PruneResult struct {
	Pruned  []string       // Names of pruned worktrees
	Skipped []SkippedEntry // Worktrees that were skipped and why
}

// SkippedEntry represents a worktree that was skipped during pruning
type SkippedEntry struct {
	Name   string
	Reason string
}

// PruneAction removes all empty worktrees
func PruneAction(ctx *app.Context, opts PruneOptions) (*PruneResult, error) {
	// Get list of all worktrees with their details
	listResult, err := ListAction(ctx, ListOptions{})
	if err != nil {
		return nil, err
	}

	result := &PruneResult{
		Pruned:  make([]string, 0),
		Skipped: make([]SkippedEntry, 0),
	}

	for _, wt := range listResult.Worktrees {
		// Determine display name
		name := wt.Name
		if name == "" {
			name = wt.AnchorBranch
		}

		// Clean up missing worktrees (directory deleted but registration remains)
		if !wt.Exists {
			if opts.DryRun {
				result.Pruned = append(result.Pruned, name)
				continue
			}

			// Check if anchor branch has children before deleting
			anchorBranch := ctx.Engine.GetBranch(wt.AnchorBranch)
			if anchorBranch.IsTracked() {
				graph := ctx.Engine.Graph(engine.SortStrategyAlphabetical)
				children := graph.Children(anchorBranch)
				if len(children) > 0 {
					result.Skipped = append(result.Skipped, SkippedEntry{
						Name:   name,
						Reason: fmt.Sprintf("anchor branch has %d children", len(children)),
					})
					continue
				}
			}

			// Unregister and delete anchor branch
			if err := RemoveAction(ctx, RemoveOptions{
				AnchorBranch: wt.AnchorBranch,
				Force:        true, // Force since directory is missing
			}); err != nil {
				result.Skipped = append(result.Skipped, SkippedEntry{
					Name:   name,
					Reason: fmt.Sprintf("cleanup failed: %v", err),
				})
				continue
			}
			result.Pruned = append(result.Pruned, name)
			continue
		}

		// Skip worktrees with stacked branches
		if wt.StackSize > 0 {
			result.Skipped = append(result.Skipped, SkippedEntry{
				Name:   name,
				Reason: fmt.Sprintf("has %d stacked branches", wt.StackSize),
			})
			continue
		}

		// Skip worktrees with uncommitted changes
		if wt.IsDirty {
			result.Skipped = append(result.Skipped, SkippedEntry{
				Name:   name,
				Reason: "has uncommitted changes",
			})
			continue
		}

		// Skip if we're currently in this worktree
		if listResult.CurrentAnchor != "" && wt.AnchorBranch == listResult.CurrentAnchor {
			result.Skipped = append(result.Skipped, SkippedEntry{
				Name:   name,
				Reason: "currently in this worktree",
			})
			continue
		}

		// This worktree is empty and can be pruned
		if opts.DryRun {
			result.Pruned = append(result.Pruned, name)
			continue
		}

		// Actually remove the worktree
		if err := RemoveAction(ctx, RemoveOptions{
			AnchorBranch: wt.AnchorBranch,
			Force:        false,
		}); err != nil {
			result.Skipped = append(result.Skipped, SkippedEntry{
				Name:   name,
				Reason: fmt.Sprintf("removal failed: %v", err),
			})
			continue
		}

		result.Pruned = append(result.Pruned, name)
	}

	return result, nil
}

// AttachOptions contains options for the attach action
type AttachOptions struct {
	Branch string // Any branch in the stack (we find the stack root)
	Name   string // Optional worktree name (defaults to stack root name)
}

// AttachResult contains the results of attaching a worktree
type AttachResult struct {
	Name         string // The name of the worktree
	AnchorBranch string // The stack root branch (serves as anchor)
	Path         string // The path to the worktree
}

// AttachAction creates a worktree for an existing stack
func AttachAction(ctx *app.Context, opts AttachOptions) (*AttachResult, error) {
	eng := ctx.Engine
	out := ctx.Output
	repoRoot := ctx.RepoRoot

	// Cannot attach from inside a managed worktree
	if ctx.InManagedWorktree {
		return nil, fmt.Errorf("cannot attach from inside a managed worktree")
	}

	// Get the branch and validate it exists and is tracked
	branch := eng.GetBranch(opts.Branch)
	if !branch.IsTracked() {
		// Check if the branch exists at all
		if _, err := eng.GetRevision(branch); err != nil {
			return nil, fmt.Errorf("branch %s does not exist", style.ColorBranchName(opts.Branch, false))
		}
		return nil, fmt.Errorf("branch %s is not tracked by stackit", style.ColorBranchName(opts.Branch, false))
	}

	// Find the stack root
	stackRootName := eng.GetStackRootForBranch(branch)
	if stackRootName == "" {
		return nil, fmt.Errorf("branch %s is not part of a stack (its parent must be trunk)", style.ColorBranchName(opts.Branch, false))
	}
	stackRoot := eng.GetBranch(stackRootName)

	// Validate: stack root is not already a worktree anchor
	if eng.IsWorktreeAnchor(stackRoot) {
		return nil, fmt.Errorf("branch %s is already a worktree anchor", style.ColorBranchName(stackRootName, false))
	}

	// Validate: stack root doesn't already have a worktree
	existingWt, err := eng.GetWorktreeForStack(stackRootName)
	if err != nil {
		return nil, fmt.Errorf("failed to check existing worktree: %w", err)
	}
	if existingWt != nil {
		return nil, fmt.Errorf("stack already has a worktree at %s", existingWt.Path)
	}

	// Determine worktree name (default to stack root name)
	name := opts.Name
	if name == "" {
		name = stackRootName
	}

	// Validate name doesn't contain path separators or other problematic characters
	if strings.ContainsAny(name, "/\\:*?\"<>|") {
		return nil, fmt.Errorf("worktree name cannot contain path separators or special characters: /\\:*?\"<>|")
	}

	// Check for duplicate worktree names
	existingWorktrees, err := eng.ListManagedWorktrees()
	if err != nil {
		return nil, fmt.Errorf("failed to check existing worktrees: %w", err)
	}
	for _, wt := range existingWorktrees {
		if wt.Name == name {
			return nil, fmt.Errorf("worktree with name %q already exists", name)
		}
	}

	// Get worktree base path from config, or use default
	cfg, _ := config.LoadConfig(repoRoot)
	basePath := cfg.WorktreeBasePath()

	// Default: sibling directory named {repo}-stacks
	if basePath == "" {
		repoName := filepath.Base(repoRoot)
		basePath = filepath.Join(filepath.Dir(repoRoot), repoName+"-stacks")
	}

	// Worktree path: basePath/name
	worktreePath := filepath.Join(basePath, name)

	// Check if path already exists
	if _, err := os.Stat(worktreePath); err == nil {
		return nil, fmt.Errorf("worktree path %s already exists; remove it first or choose a different name", worktreePath)
	}

	// Remember current branch for restoration on failure
	currentBranch := eng.CurrentBranch()
	trunk := eng.Trunk()

	// If we're on a branch in the stack being attached, checkout trunk first
	// Git doesn't allow creating a worktree with a branch that's currently checked out
	needsCheckout := false
	if currentBranch != nil {
		currentStackRoot := eng.GetStackRootForBranch(*currentBranch)
		if currentStackRoot == stackRootName {
			needsCheckout = true
			if err := eng.CheckoutBranch(ctx.Context, trunk); err != nil {
				return nil, fmt.Errorf("failed to checkout trunk before creating worktree: %w", err)
			}
		}
	}

	// Create the worktree (non-detached, pointing to the stack root)
	if err := eng.AddWorktree(ctx.Context, worktreePath, stackRootName, false); err != nil {
		// Restore original branch on failure
		if needsCheckout && currentBranch != nil {
			_ = eng.CheckoutBranch(ctx.Context, *currentBranch)
		}
		return nil, fmt.Errorf("failed to create worktree: %w", err)
	}

	// Register the worktree in local refs with the name
	if err := eng.RegisterWorktreeWithName(stackRootName, worktreePath, name); err != nil {
		// Clean up worktree on registration failure
		_ = eng.RemoveWorktree(ctx.Context, worktreePath)
		if needsCheckout && currentBranch != nil {
			_ = eng.CheckoutBranch(ctx.Context, *currentBranch)
		}
		return nil, fmt.Errorf("failed to register worktree: %w", err)
	}

	// If we weren't already on trunk, checkout trunk now (stack now "belongs" to worktree)
	if !needsCheckout {
		if err := eng.CheckoutBranch(ctx.Context, trunk); err != nil {
			// Clean up on failure
			_ = eng.UnregisterWorktree(stackRootName)
			_ = eng.RemoveWorktree(ctx.Context, worktreePath)
			// Try to restore original branch
			if currentBranch != nil {
				_ = eng.CheckoutBranch(ctx.Context, *currentBranch)
			}
			return nil, fmt.Errorf("failed to checkout trunk: %w", err)
		}
	}

	out.Success("Attached stack %s to worktree", style.ColorBranchName(stackRootName, false))
	out.Info("  Name: %s", style.ColorBranchName(name, false))
	out.Info("  Path: %s", style.ColorDim(worktreePath))
	out.Newline()

	// Run post-create hooks
	if err := RunPostCreateHooks(ctx, worktreePath); err != nil {
		out.Warn("Post-create hooks failed: %v", err)
	}

	return &AttachResult{
		Name:         name,
		AnchorBranch: stackRootName,
		Path:         worktreePath,
	}, nil
}

// DetachOptions contains options for the detach action
type DetachOptions struct {
	NameOrBranch string // Worktree name or anchor branch
	Force        bool   // Allow detach even with uncommitted changes
}

// DetachAction removes a worktree while preserving all branches
func DetachAction(ctx *app.Context, opts DetachOptions) error {
	out := ctx.Output

	// Find worktree by name or anchor branch
	wtInfo, err := findWorktreeByNameOrBranch(ctx, opts.NameOrBranch)
	if err != nil {
		return err
	}

	// Check if we're currently in this worktree
	if ctx.InManagedWorktree && ctx.WorktreeInfo != nil {
		if ctx.WorktreeInfo.AnchorBranch == wtInfo.AnchorBranch {
			return fmt.Errorf("cannot detach from inside the worktree; cd to main repo first")
		}
	}

	// Check if path exists and has uncommitted changes
	if _, statErr := os.Stat(wtInfo.Path); statErr == nil {
		isDirty, err := ctx.Git().WorktreeHasUncommittedChanges(ctx.Context, wtInfo.Path)
		if err == nil && isDirty && !opts.Force {
			return fmt.Errorf("worktree has uncommitted changes; use --force to override")
		}
	}

	// Determine anchor branch type
	anchorBranch := ctx.Engine.GetBranch(wtInfo.AnchorBranch)
	isWorktreeAnchor := ctx.Engine.IsWorktreeAnchor(anchorBranch)

	// Remove the git worktree directory
	if _, statErr := os.Stat(wtInfo.Path); statErr == nil {
		if removeErr := ctx.Engine.RemoveWorktree(ctx.Context, wtInfo.Path); removeErr != nil {
			if !opts.Force {
				return fmt.Errorf("failed to remove worktree at %s: %w (use --force to override)", wtInfo.Path, removeErr)
			}
			out.Warn("Failed to remove worktree directory, continuing with unregistration: %v", removeErr)
		}
	} else {
		out.Debug("Worktree path %s does not exist, skipping removal", wtInfo.Path)
		// Prune stale git worktree references
		if pruneErr := ctx.Engine.PruneWorktrees(ctx.Context); pruneErr != nil {
			out.Debug("Failed to prune worktrees: %v", pruneErr)
		}
	}

	// Unregister from registry
	if unregErr := ctx.Engine.UnregisterWorktree(wtInfo.AnchorBranch); unregErr != nil {
		return fmt.Errorf("failed to unregister worktree: %w", unregErr)
	}

	// Handle anchor cleanup based on branch type
	if isWorktreeAnchor {
		// This was created by `wt create` - reparent children to trunk and delete anchor
		graph := ctx.Engine.Graph(engine.SortStrategyAlphabetical)
		childNames := graph.Children(anchorBranch)

		if len(childNames) > 0 {
			trunk := ctx.Engine.Trunk()
			// Reparent all children to trunk, preserving divergence points
			if err := ctx.Engine.ReparentBranches(ctx.Context, childNames, trunk); err != nil {
				out.Warn("Failed to reparent children to trunk: %v", err)
			}
		}

		// Delete the worktree-anchor branch
		if err := ctx.Engine.DeleteBranch(ctx.Context, anchorBranch); err != nil {
			out.Warn("Failed to delete anchor branch: %v", err)
		} else {
			out.Debug("Deleted anchor branch %s", wtInfo.AnchorBranch)
		}
	}
	// For BranchTypeUser (from `wt attach`): leave the branch as-is since it has real commits

	out.Success("Detached worktree %s", style.ColorBranchName(wtInfo.Name, false))
	if !isWorktreeAnchor {
		out.Info("  Stack branches preserved (branch %s has commits)", style.ColorBranchName(wtInfo.AnchorBranch, false))
	}

	return nil
}
