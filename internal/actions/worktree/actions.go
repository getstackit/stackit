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
)

type worktreeSnapshot struct {
	Info           engine.WorktreeInfo
	CheckoutBranch string
	AnchorExists   bool
	AnchorParent   string
	AnchorScope    string
	AnchorRev      string
	ChildNames     []string
}

// RemoveOptions contains options for the remove action
type RemoveOptions struct {
	AnchorBranch string // Anchor branch name to remove worktree for
	Force        bool   // Force removal even if worktree has uncommitted changes
}

func snapshotWorktree(ctx *app.Context, entry *Entry) (*worktreeSnapshot, error) {
	wtInfo, err := findWorktreeByNameOrBranch(ctx, entry.AnchorBranch)
	if err != nil {
		return nil, err
	}

	checkoutBranch := entry.CurrentBranch
	if checkoutBranch == "" {
		if root := entry.primaryRootBranch(); root != "" {
			checkoutBranch = root
		} else {
			checkoutBranch = entry.AnchorBranch
		}
	}

	snapshot := &worktreeSnapshot{
		Info:           *wtInfo,
		CheckoutBranch: checkoutBranch,
	}

	anchorBranch := ctx.Engine.GetBranch(entry.AnchorBranch)
	if !ctx.Engine.BranchNames().Contains(entry.AnchorBranch) {
		return snapshot, nil
	}

	snapshot.AnchorExists = true
	snapshot.AnchorScope = ctx.Engine.GetScope(anchorBranch).String()
	if parent := anchorBranch.GetParent(); parent != nil {
		snapshot.AnchorParent = parent.GetName()
	} else {
		snapshot.AnchorParent = ctx.Engine.Trunk().GetName()
	}
	anchorRev, err := anchorBranch.GetRevision()
	if err != nil {
		return nil, fmt.Errorf("failed to get revision for %s: %w", entry.AnchorBranch, err)
	}
	snapshot.AnchorRev = anchorRev

	graph := ctx.Engine.Graph(engine.SortStrategyAlphabetical)
	snapshot.ChildNames = append(snapshot.ChildNames, graph.Children(anchorBranch)...)
	return snapshot, nil
}

func restoreWorktreeRegistration(ctx *app.Context, snapshot *worktreeSnapshot) error {
	return ctx.Engine.RegisterWorktreeWithName(snapshot.Info.AnchorBranch, snapshot.Info.Path, snapshot.Info.Name)
}

func restoreWorktreePath(ctx *app.Context, snapshot *worktreeSnapshot) error {
	if snapshot.CheckoutBranch == "" {
		return nil
	}
	return ctx.Engine.AddWorktree(ctx.Context, snapshot.Info.Path, snapshot.CheckoutBranch, false)
}

func restoreAnchorBranch(ctx *app.Context, snapshot *worktreeSnapshot) error {
	if !snapshot.AnchorExists || snapshot.AnchorRev == "" {
		return nil
	}
	anchorName := snapshot.Info.AnchorBranch
	if ctx.Engine.BranchNames().Contains(anchorName) {
		return nil
	}
	if err := ctx.Engine.CreateBranch(ctx.Context, anchorName, snapshot.AnchorRev); err != nil {
		return fmt.Errorf("failed to recreate anchor branch %s: %w", anchorName, err)
	}
	anchorBranch := ctx.Engine.GetBranch(anchorName)
	if err := ctx.Engine.SetParent(ctx.Context, anchorBranch, ctx.Engine.GetBranch(snapshot.AnchorParent), engine.DivergenceRecompute); err != nil {
		return fmt.Errorf("failed to restore anchor parent for %s: %w", anchorName, err)
	}
	if err := ctx.Engine.SetBranchType(anchorBranch, git.BranchTypeWorktreeAnchor); err != nil {
		return fmt.Errorf("failed to restore anchor branch type for %s: %w", anchorName, err)
	}
	if snapshot.AnchorScope != "" {
		if err := ctx.Engine.SetScope(ctx.Context, anchorBranch, engine.NewScope(snapshot.AnchorScope)); err != nil {
			return fmt.Errorf("failed to restore anchor scope for %s: %w", anchorName, err)
		}
	}
	return nil
}

// RemoveAction removes a worktree for a stack
func RemoveAction(ctx *app.Context, opts RemoveOptions) error {
	out := ctx.Output

	entry, err := getWorktreeEntry(ctx, opts.AnchorBranch)
	if err != nil {
		return err
	}
	if entry.NeedsRepair && (entry.RegistrationState != RegistrationStateInvalid || entry.Exists) {
		return fmt.Errorf("managed worktree %s cannot be removed because %s; %s", output.Branch(entry.displayName(), false), output.Dim(entry.StatusMessage), repairHint(entry))
	}
	if entry.IsCurrent {
		return fmt.Errorf("cannot remove the current worktree; cd to the main repo first")
	}
	if len(entry.RootBranches) > 0 {
		return fmt.Errorf("worktree %s has %d branch(es); use 'stackit worktree detach %s' to remove the worktree while keeping branches", output.Branch(entry.displayName(), false), entry.StackSize, entry.displayName())
	}
	if entry.Exists && entry.IsDirty && !opts.Force {
		return fmt.Errorf("worktree has uncommitted changes; use --force to discard them")
	}

	snapshot, err := snapshotWorktree(ctx, entry)
	if err != nil {
		return err
	}

	pathRemoved := false
	unregistered := false
	rollback := func(cause error) error {
		var rollbackErrs []string
		if unregistered {
			if err := restoreWorktreeRegistration(ctx, snapshot); err != nil {
				rollbackErrs = append(rollbackErrs, err.Error())
			}
		}
		if pathRemoved {
			if err := restoreWorktreePath(ctx, snapshot); err != nil {
				rollbackErrs = append(rollbackErrs, err.Error())
			}
		}
		if len(rollbackErrs) == 0 {
			return cause
		}
		return fmt.Errorf("%w (rollback failed: %s)", cause, strings.Join(rollbackErrs, "; "))
	}

	if entry.Exists {
		if removeErr := removeWorktreePath(ctx, snapshot.Info.Path, opts.Force); removeErr != nil {
			if opts.Force {
				return fmt.Errorf("failed to force remove worktree at %s: %w", snapshot.Info.Path, removeErr)
			}
			return fmt.Errorf("failed to remove worktree at %s: %w (use --force to discard uncommitted changes)", snapshot.Info.Path, removeErr)
		}
		pathRemoved = true
	} else {
		out.Debug("Worktree path %s does not exist, skipping removal", snapshot.Info.Path)
		if pruneErr := ctx.Engine.PruneWorktrees(ctx.Context); pruneErr != nil {
			out.Debug("Failed to prune worktrees: %v", pruneErr)
		}
	}

	if unregErr := ctx.Engine.UnregisterWorktree(ctx.Context, snapshot.Info.AnchorBranch); unregErr != nil {
		if pathRemoved {
			return rollback(fmt.Errorf("failed to unregister worktree: %w", unregErr))
		}
		return fmt.Errorf("failed to unregister worktree: %w", unregErr)
	}
	unregistered = true

	if snapshot.AnchorExists {
		if err := ctx.Engine.DeleteBranch(ctx.Context, ctx.Engine.GetBranch(snapshot.Info.AnchorBranch)); err != nil {
			return rollback(fmt.Errorf("failed to delete anchor branch %s: %w", snapshot.Info.AnchorBranch, err))
		}
		out.Debug("Deleted anchor branch %s", snapshot.Info.AnchorBranch)
	}

	out.Success("Removed worktree for stack %s", output.Branch(snapshot.Info.AnchorBranch, false))
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
	remoteCtx, cancelRemote := ctx.RemoteOperationContext()
	trunkStatus := eng.ReadBranchRemoteStatuses(remoteCtx, engine.BranchesOf(trunk)).ForBranch(trunk)
	cancelRemote()
	if trunkStatus.Behind() {
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

	created, err := createAnchoredWorktree(ctx, eng, repoRoot, anchoredWorktreeOptions{
		Name:         opts.Name,
		Scope:        opts.Scope,
		AnchorParent: trunk.GetName(),
	})
	if err != nil {
		return nil, err
	}

	out.Success("Created worktree %s", output.Branch(opts.Name, false))
	out.Info("  Anchor branch: %s", output.Branch(created.AnchorBranch, false))
	out.Info("  Path: %s", output.Dim(created.Path))
	if opts.Scope != "" {
		out.Info("  Scope: %s", output.Dim(opts.Scope))
	}
	out.Newline()

	// Run post-create hooks
	if err := RunPostCreateHooks(ctx, created.Path); err != nil {
		out.Warn("Post-create hooks failed: %v", err)
	}

	return &CreateResult{
		Name:         opts.Name,
		AnchorBranch: created.AnchorBranch,
		Path:         created.Path,
	}, nil
}

type anchoredWorktreeOptions struct {
	Name           string
	Scope          string
	AnchorParent   string
	RootBranch     string
	OriginalParent string
}

type anchoredWorktreeResult struct {
	Name         string
	AnchorBranch string
	Path         string
}

// CreateAnchoredWorktreeForBranch creates a hidden anchor for branchName, moves branchName
// under it without rebasing, and opens a worktree checked out on branchName.
func CreateAnchoredWorktreeForBranch(ctx *app.Context, branchName string, name string, scope string) (*CreateResult, error) {
	if name == "" {
		name = branchName
	}
	parent := ctx.Engine.Trunk().GetName()
	if branch := ctx.Engine.GetBranch(branchName); branch.IsTracked() {
		if currentParent := branch.GetParent(); currentParent != nil {
			parent = currentParent.GetName()
		}
	}

	created, err := createAnchoredWorktree(ctx, ctx.Engine, ctx.RepoRoot, anchoredWorktreeOptions{
		Name:           name,
		Scope:          scope,
		AnchorParent:   parent,
		RootBranch:     branchName,
		OriginalParent: parent,
	})
	if err != nil {
		return nil, err
	}
	return &CreateResult{
		Name:         created.Name,
		AnchorBranch: created.AnchorBranch,
		Path:         created.Path,
	}, nil
}

func createAnchoredWorktree(ctx *app.Context, eng engine.Engine, repoRoot string, opts anchoredWorktreeOptions) (*anchoredWorktreeResult, error) {
	out := ctx.Output
	if opts.AnchorParent == "" {
		opts.AnchorParent = eng.Trunk().GetName()
	}
	if opts.OriginalParent == "" {
		opts.OriginalParent = opts.AnchorParent
	}

	anchorBranchName, err := generateAnchorBranchName(ctx, repoRoot, opts.Name, opts.Scope)
	if err != nil {
		return nil, err
	}
	if eng.BranchNames().Contains(anchorBranchName) {
		return nil, fmt.Errorf("branch %s already exists", anchorBranchName)
	}

	parentBranch := eng.GetBranch(opts.AnchorParent)
	parentSHA, err := parentBranch.GetRevision()
	if err != nil {
		return nil, fmt.Errorf("failed to get anchor parent revision: %w", err)
	}

	if err := eng.CreateBranch(ctx.Context, anchorBranchName, parentSHA); err != nil {
		return nil, fmt.Errorf("failed to create anchor branch: %w", err)
	}

	anchorBranch := eng.GetBranch(anchorBranchName)
	anchorCreated := true
	rootReparented := false
	worktreeCreated := false
	registered := false
	worktreePath := ""

	cleanup := func() {
		if registered {
			if err := eng.UnregisterWorktree(ctx.Context, anchorBranchName); err != nil {
				out.Debug("Failed to unregister worktree during rollback: %v", err)
			}
		}
		if worktreeCreated && worktreePath != "" {
			if err := eng.ForceRemoveWorktree(ctx.Context, worktreePath); err != nil {
				out.Debug("Failed to remove worktree during rollback: %v", err)
			}
		}
		if rootReparented && opts.RootBranch != "" {
			if err := eng.ReparentBranch(ctx.Context, eng.GetBranch(opts.RootBranch), eng.GetBranch(opts.OriginalParent)); err != nil {
				out.Debug("Failed to restore parent for %s during rollback: %v", opts.RootBranch, err)
			}
		}
		if anchorCreated {
			cleanupAnchorBranch(ctx.Context, eng, anchorBranchName, out)
		}
	}

	if err := eng.SetParent(ctx.Context, anchorBranch, parentBranch, engine.DivergenceRecompute); err != nil {
		cleanup()
		return nil, fmt.Errorf("failed to set parent: %w", err)
	}
	if err := eng.SetBranchType(anchorBranch, git.BranchTypeWorktreeAnchor); err != nil {
		cleanup()
		return nil, fmt.Errorf("failed to set branch type: %w", err)
	}
	if opts.Scope != "" {
		if err := eng.SetScope(ctx.Context, anchorBranch, engine.NewScope(opts.Scope)); err != nil {
			cleanup()
			return nil, fmt.Errorf("failed to set scope: %w", err)
		}
	}

	checkoutBranch := anchorBranchName
	if opts.RootBranch != "" {
		if err := eng.ReparentBranch(ctx.Context, eng.GetBranch(opts.RootBranch), anchorBranch); err != nil {
			cleanup()
			return nil, fmt.Errorf("failed to reparent %s under worktree anchor: %w", opts.RootBranch, err)
		}
		rootReparented = true
		checkoutBranch = opts.RootBranch
	}

	worktreePath = worktreePathForName(repoRoot, opts.Name)
	if _, err := os.Stat(worktreePath); err == nil {
		cleanup()
		return nil, fmt.Errorf("worktree path %s already exists; remove it first or choose a different name", worktreePath)
	}

	if err := eng.AddWorktree(ctx.Context, worktreePath, checkoutBranch, false); err != nil {
		cleanup()
		return nil, fmt.Errorf("failed to create worktree: %w", err)
	}
	worktreeCreated = true

	if err := eng.RegisterWorktreeWithName(anchorBranchName, worktreePath, opts.Name); err != nil {
		cleanup()
		return nil, fmt.Errorf("failed to register worktree: %w", err)
	}
	registered = true

	return &anchoredWorktreeResult{
		Name:         opts.Name,
		AnchorBranch: anchorBranchName,
		Path:         worktreePath,
	}, nil
}

func generateAnchorBranchName(ctx *app.Context, repoRoot string, name string, scope string) (string, error) {
	cfg, _ := config.LoadConfig(repoRoot)
	patternStr := cfg.BranchNamePattern()
	pattern, err := config.NewBranchPattern(patternStr)
	if err != nil {
		return "", fmt.Errorf("invalid branch pattern: %w", err)
	}

	anchorBranchName, err := pattern.GetBranchName(ctx, name+"-wt", scope)
	if err != nil {
		return "", fmt.Errorf("failed to generate anchor branch name: %w", err)
	}
	return anchorBranchName, nil
}

func worktreePathForName(repoRoot string, name string) string {
	// Get worktree base path from config, or use default
	cfg, _ := config.LoadConfig(repoRoot)
	basePath := cfg.WorktreeBasePath()

	// Default: sibling directory named {repo}-stacks
	if basePath == "" {
		repoName := filepath.Base(repoRoot)
		basePath = filepath.Join(filepath.Dir(repoRoot), repoName+"-stacks")
	}

	return filepath.Join(basePath, name)
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
		name := wt.displayName()

		// Clean up missing worktrees (directory deleted but registration remains)
		if !wt.Exists {
			if wt.NeedsRepair {
				if opts.DryRun && wt.CanRemove {
					result.Pruned = append(result.Pruned, name)
					continue
				}
				result.Skipped = append(result.Skipped, SkippedEntry{
					Name:   name,
					Reason: wt.StatusMessage + "; " + repairHint(&wt),
				})
				continue
			}
			if opts.DryRun {
				result.Pruned = append(result.Pruned, name)
				continue
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

		if wt.NeedsRepair {
			result.Skipped = append(result.Skipped, SkippedEntry{
				Name:   name,
				Reason: wt.StatusMessage + "; " + repairHint(&wt),
			})
			continue
		}

		// Skip worktrees with stacked branches
		if len(wt.RootBranches) > 0 {
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
	AnchorBranch string // Hidden worktree anchor branch
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
			return nil, fmt.Errorf("branch %s does not exist", output.Branch(opts.Branch, false))
		}
		return nil, fmt.Errorf("branch %s is not tracked by stackit", output.Branch(opts.Branch, false))
	}

	// Find the stack root
	stackRootName := eng.GetStackRootForBranch(branch)
	if stackRootName == "" {
		return nil, fmt.Errorf("branch %s is not part of a stack (its parent must be trunk)", output.Branch(opts.Branch, false))
	}
	stackRoot := eng.GetBranch(stackRootName)
	originalParent := eng.Trunk().GetName()
	if parent := stackRoot.GetParent(); parent != nil {
		originalParent = parent.GetName()
	}

	// Validate: stack root is not already a worktree anchor
	if eng.IsWorktreeAnchor(stackRoot) {
		return nil, fmt.Errorf("branch %s is already a worktree anchor", output.Branch(stackRootName, false))
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

	worktreePath := worktreePathForName(repoRoot, name)
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

	created, err := createAnchoredWorktree(ctx, eng, repoRoot, anchoredWorktreeOptions{
		Name:           name,
		AnchorParent:   originalParent,
		RootBranch:     stackRootName,
		OriginalParent: originalParent,
	})
	if err != nil {
		if needsCheckout && currentBranch != nil {
			_ = eng.CheckoutBranch(ctx.Context, *currentBranch)
		}
		return nil, err
	}

	out.Success("Attached stack %s to worktree", output.Branch(stackRootName, false))
	out.Info("  Name: %s", output.Branch(name, false))
	out.Info("  Anchor branch: %s", output.Branch(created.AnchorBranch, false))
	out.Info("  Path: %s", output.Dim(created.Path))
	out.Newline()

	// Run post-create hooks
	if err := RunPostCreateHooks(ctx, created.Path); err != nil {
		out.Warn("Post-create hooks failed: %v", err)
	}

	return &AttachResult{
		Name:         name,
		AnchorBranch: created.AnchorBranch,
		Path:         created.Path,
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

	entry, err := getWorktreeEntry(ctx, opts.NameOrBranch)
	if err != nil {
		return err
	}
	if entry.NeedsRepair {
		return fmt.Errorf("managed worktree %s cannot be detached because %s; %s", output.Branch(entry.displayName(), false), output.Dim(entry.StatusMessage), repairHint(entry))
	}

	// Check if we're currently in this worktree
	if ctx.InManagedWorktree && ctx.WorktreeInfo != nil {
		if ctx.WorktreeInfo.AnchorBranch == entry.AnchorBranch {
			return fmt.Errorf("cannot detach from inside the worktree; cd to main repo first")
		}
	}

	if entry.IsDirty && !opts.Force {
		return fmt.Errorf("worktree has uncommitted changes; use --force to override")
	}

	snapshot, err := snapshotWorktree(ctx, entry)
	if err != nil {
		return err
	}

	pathRemoved := false
	reparented := false
	anchorDeleted := false
	unregistered := false
	rollback := func(cause error) error {
		var rollbackErrs []string
		if anchorDeleted {
			if err := restoreAnchorBranch(ctx, snapshot); err != nil {
				rollbackErrs = append(rollbackErrs, err.Error())
			}
		}
		if reparented && len(snapshot.ChildNames) > 0 {
			if err := ctx.Engine.ReparentBranches(ctx.Context, snapshot.ChildNames, ctx.Engine.GetBranch(snapshot.Info.AnchorBranch)); err != nil {
				rollbackErrs = append(rollbackErrs, err.Error())
			}
		}
		if unregistered {
			if err := restoreWorktreeRegistration(ctx, snapshot); err != nil {
				rollbackErrs = append(rollbackErrs, err.Error())
			}
		}
		if pathRemoved {
			if err := restoreWorktreePath(ctx, snapshot); err != nil {
				rollbackErrs = append(rollbackErrs, err.Error())
			}
		}
		if len(rollbackErrs) == 0 {
			return cause
		}
		return fmt.Errorf("%w (rollback failed: %s)", cause, strings.Join(rollbackErrs, "; "))
	}

	// Remove the git worktree directory
	if entry.Exists {
		if removeErr := removeWorktreePath(ctx, snapshot.Info.Path, opts.Force); removeErr != nil {
			if opts.Force {
				return fmt.Errorf("failed to force remove worktree at %s: %w", snapshot.Info.Path, removeErr)
			}
			return fmt.Errorf("failed to remove worktree at %s: %w (use --force to discard uncommitted changes)", snapshot.Info.Path, removeErr)
		}
		pathRemoved = true
	} else {
		out.Debug("Worktree path %s does not exist, skipping removal", snapshot.Info.Path)
		// Prune stale git worktree references
		if pruneErr := ctx.Engine.PruneWorktrees(ctx.Context); pruneErr != nil {
			out.Debug("Failed to prune worktrees: %v", pruneErr)
		}
	}

	if snapshot.AnchorExists && len(snapshot.ChildNames) > 0 {
		if err := ctx.Engine.ReparentBranches(ctx.Context, snapshot.ChildNames, ctx.Engine.GetBranch(snapshot.AnchorParent)); err != nil {
			return rollback(fmt.Errorf("failed to reparent children to %s: %w", snapshot.AnchorParent, err))
		}
		reparented = true
	}

	if snapshot.AnchorExists {
		if err := ctx.Engine.DeleteBranch(ctx.Context, ctx.Engine.GetBranch(snapshot.Info.AnchorBranch)); err != nil {
			return rollback(fmt.Errorf("failed to delete anchor branch %s: %w", snapshot.Info.AnchorBranch, err))
		}
		anchorDeleted = true
		out.Debug("Deleted anchor branch %s", snapshot.Info.AnchorBranch)
	}

	if unregErr := ctx.Engine.UnregisterWorktree(ctx.Context, snapshot.Info.AnchorBranch); unregErr != nil {
		return rollback(fmt.Errorf("failed to unregister worktree: %w", unregErr))
	}
	unregistered = true

	out.Success("Detached worktree %s", output.Branch(snapshot.Info.Name, false))
	return nil
}
func removeWorktreePath(ctx *app.Context, path string, force bool) error {
	if force {
		return ctx.Engine.ForceRemoveWorktree(ctx.Context, path)
	}
	return ctx.Engine.RemoveWorktree(ctx.Context, path)
}
