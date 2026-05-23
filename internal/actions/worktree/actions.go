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
	NameOrBranch string
}

type RegistrationState string

const (
	RegistrationStateOK      RegistrationState = "ok"
	RegistrationStateLegacy  RegistrationState = "legacy"
	RegistrationStateInvalid RegistrationState = "invalid"
)

// ListResult contains the results of listing worktrees
type ListResult struct {
	Worktrees     []Entry
	CurrentAnchor string // Anchor branch of the worktree we're currently in (if any)
}

// Entry represents a single managed worktree
type Entry struct {
	Name              string            `json:"name"`          // User-provided name
	AnchorBranch      string            `json:"anchor_branch"` // Registered anchor branch name
	Path              string            `json:"path"`
	Exists            bool              `json:"exists"`                   // Whether the path still exists on disk
	StackSize         int               `json:"stack_size"`               // Number of real branches in the stack
	CurrentBranch     string            `json:"current_branch,omitempty"` // Branch currently checked out in this worktree
	IsDirty           bool              `json:"is_dirty"`
	RootBranches      []string          `json:"root_branches,omitempty"`  // Real stack roots visible to the user
	RegistrationState RegistrationState `json:"registration_state"`       // Whether the registration is anchored, legacy, or invalid
	StatusMessage     string            `json:"status_message,omitempty"` // Human-readable summary of current state
	CanRemove         bool              `json:"can_remove"`               // Empty anchored worktree can be removed
	CanDetach         bool              `json:"can_detach"`               // Anchored worktree can be detached
	NeedsRepair       bool              `json:"needs_repair"`             // Registration requires repair before lifecycle actions
	IsCurrent         bool              `json:"is_current,omitempty"`     // Whether this is the current managed worktree
}

type worktreeSnapshot struct {
	Info           engine.WorktreeInfo
	CheckoutBranch string
	AnchorExists   bool
	AnchorParent   string
	AnchorScope    string
	AnchorRev      string
	ChildNames     []string
}

func (e Entry) displayName() string {
	if e.Name != "" {
		return e.Name
	}
	return e.AnchorBranch
}

func (e Entry) primaryRootBranch() string {
	if len(e.RootBranches) == 0 {
		return ""
	}
	return e.RootBranches[0]
}

func inspectWorktreeEntry(ctx *app.Context, wt engine.WorktreeInfo, graph *engine.StackGraph, currentAnchor string) Entry {
	entry := Entry{
		Name:              wt.Name,
		AnchorBranch:      wt.AnchorBranch,
		Path:              wt.Path,
		Exists:            true,
		RegistrationState: RegistrationStateInvalid,
		StatusMessage:     "registration is invalid",
		IsCurrent:         currentAnchor != "" && wt.AnchorBranch == currentAnchor,
	}

	if _, statErr := os.Stat(wt.Path); os.IsNotExist(statErr) {
		entry.Exists = false
	}

	if entry.Exists {
		currentBranch, err := ctx.Git().GetWorktreeCurrentBranch(ctx.Context, wt.Path)
		if err == nil && currentBranch != "" {
			entry.CurrentBranch = currentBranch
		}

		isDirty, err := ctx.Git().WorktreeHasUncommittedChanges(ctx.Context, wt.Path)
		if err == nil {
			entry.IsDirty = isDirty
		}
	}

	anchorBranch := ctx.Engine.GetBranch(wt.AnchorBranch)
	anchorExists := ctx.Engine.BranchNames().Contains(wt.AnchorBranch)

	switch {
	case anchorExists && ctx.Engine.IsWorktreeAnchor(anchorBranch):
		entry.RegistrationState = RegistrationStateOK
		entry.StatusMessage = "healthy"
		children := graph.Children(anchorBranch)
		entry.RootBranches = append(entry.RootBranches, children...)

		descendants := graph.Range(anchorBranch, engine.StackRange{
			RecursiveChildren: true,
			IncludeCurrent:    false,
		})
		entry.StackSize = len(descendants)
		entry.CanDetach = !entry.IsCurrent
		entry.CanRemove = !entry.IsCurrent && len(children) == 0

		switch {
		case !entry.Exists:
			entry.StatusMessage = "worktree directory is missing"
		case entry.IsDirty:
			entry.StatusMessage = "has uncommitted changes"
		case len(children) == 0:
			entry.StatusMessage = "empty anchor"
		}

	case anchorExists:
		entry.RegistrationState = RegistrationStateLegacy
		entry.StatusMessage = "legacy registration points at a real branch"
		entry.RootBranches = []string{wt.AnchorBranch}
		entry.NeedsRepair = true

		descendants := graph.Range(anchorBranch, engine.StackRange{
			RecursiveChildren: true,
			IncludeCurrent:    true,
		})
		entry.StackSize = len(descendants)

		if !entry.Exists {
			entry.StatusMessage = "legacy registration points at a real branch and the directory is missing"
		}

	default:
		entry.NeedsRepair = true
		entry.StatusMessage = "registered anchor branch is missing"

		if entry.CurrentBranch != "" {
			currentBranch := ctx.Engine.GetBranch(entry.CurrentBranch)
			if currentBranch.IsTracked() {
				stackRoot := ctx.Engine.GetStackRootForBranch(currentBranch)
				if stackRoot != "" {
					entry.RootBranches = []string{stackRoot}
				} else {
					entry.RootBranches = []string{entry.CurrentBranch}
				}
			}
		}

		if !entry.Exists {
			entry.StatusMessage = "registration is stale and the directory is missing"
			entry.CanRemove = !entry.IsCurrent
		}
	}

	return entry
}

func listEntries(ctx *app.Context, opts ListOptions) (*ListResult, error) {
	worktrees, err := ctx.Engine.ListManagedWorktrees()
	if err != nil {
		return nil, fmt.Errorf("failed to list managed worktrees: %w", err)
	}

	result := &ListResult{
		Worktrees: make([]Entry, 0, len(worktrees)),
	}

	if ctx.InManagedWorktree && ctx.WorktreeInfo != nil {
		result.CurrentAnchor = ctx.WorktreeInfo.AnchorBranch
	}

	graph := ctx.Engine.Graph(engine.SortStrategyAlphabetical)

	for _, wt := range worktrees {
		entry := inspectWorktreeEntry(ctx, wt, graph, result.CurrentAnchor)
		if opts.NameOrBranch != "" && entry.AnchorBranch != opts.NameOrBranch && entry.Name != opts.NameOrBranch {
			continue
		}
		result.Worktrees = append(result.Worktrees, entry)
	}

	return result, nil
}

// ListAction lists all managed worktrees
func ListAction(ctx *app.Context, _ ListOptions) (*ListResult, error) {
	return listEntries(ctx, ListOptions{})
}

// RemoveOptions contains options for the remove action
type RemoveOptions struct {
	AnchorBranch string // Anchor branch name to remove worktree for
	Force        bool   // Force removal even if worktree has uncommitted changes
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

func getWorktreeEntry(ctx *app.Context, nameOrBranch string) (*Entry, error) {
	result, err := listEntries(ctx, ListOptions{NameOrBranch: nameOrBranch})
	if err != nil {
		return nil, err
	}
	if len(result.Worktrees) == 0 {
		return nil, fmt.Errorf("no worktree found for %s", style.ColorBranchName(nameOrBranch, false))
	}
	entry := result.Worktrees[0]
	return &entry, nil
}

func repairHint(entry *Entry) string {
	return fmt.Sprintf("run 'stackit worktree repair %s' first", entry.displayName())
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
	if err := ctx.Engine.SetParent(ctx.Context, anchorBranch, ctx.Engine.GetBranch(snapshot.AnchorParent)); err != nil {
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
		return fmt.Errorf("managed worktree %s cannot be removed because %s; %s", style.ColorBranchName(entry.displayName(), false), style.ColorDim(entry.StatusMessage), repairHint(entry))
	}
	if entry.IsCurrent {
		return fmt.Errorf("cannot remove the current worktree; cd to the main repo first")
	}
	if len(entry.RootBranches) > 0 {
		return fmt.Errorf("worktree %s has %d branch(es); use 'stackit worktree detach %s' to remove the worktree while keeping branches", style.ColorBranchName(entry.displayName(), false), entry.StackSize, entry.displayName())
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

	if unregErr := ctx.Engine.UnregisterWorktree(snapshot.Info.AnchorBranch); unregErr != nil {
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

	out.Success("Removed worktree for stack %s", style.ColorBranchName(snapshot.Info.AnchorBranch, false))
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

	created, err := createAnchoredWorktree(ctx, eng, repoRoot, anchoredWorktreeOptions{
		Name:         opts.Name,
		Scope:        opts.Scope,
		AnchorParent: trunk.GetName(),
	})
	if err != nil {
		return nil, err
	}

	out.Success("Created worktree %s", style.ColorBranchName(opts.Name, false))
	out.Info("  Anchor branch: %s", style.ColorBranchName(created.AnchorBranch, false))
	out.Info("  Path: %s", style.ColorDim(created.Path))
	if opts.Scope != "" {
		out.Info("  Scope: %s", style.ColorDim(opts.Scope))
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
			if err := eng.UnregisterWorktree(anchorBranchName); err != nil {
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

	if err := eng.SetParent(ctx.Context, anchorBranch, parentBranch); err != nil {
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
	originalParent := eng.Trunk().GetName()
	if parent := stackRoot.GetParent(); parent != nil {
		originalParent = parent.GetName()
	}

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

	out.Success("Attached stack %s to worktree", style.ColorBranchName(stackRootName, false))
	out.Info("  Name: %s", style.ColorBranchName(name, false))
	out.Info("  Anchor branch: %s", style.ColorBranchName(created.AnchorBranch, false))
	out.Info("  Path: %s", style.ColorDim(created.Path))
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

type RepairOptions struct {
	NameOrBranch string
}

type RepairEntry struct {
	Name         string `json:"name"`
	Action       string `json:"action"`
	AnchorBranch string `json:"anchor_branch,omitempty"`
}

type RepairResult struct {
	Repaired []RepairEntry  `json:"repaired"`
	Skipped  []SkippedEntry `json:"skipped,omitempty"`
}

// DetachAction removes a worktree while preserving all branches
func DetachAction(ctx *app.Context, opts DetachOptions) error {
	out := ctx.Output

	entry, err := getWorktreeEntry(ctx, opts.NameOrBranch)
	if err != nil {
		return err
	}
	if entry.NeedsRepair {
		return fmt.Errorf("managed worktree %s cannot be detached because %s; %s", style.ColorBranchName(entry.displayName(), false), style.ColorDim(entry.StatusMessage), repairHint(entry))
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

	if unregErr := ctx.Engine.UnregisterWorktree(snapshot.Info.AnchorBranch); unregErr != nil {
		return rollback(fmt.Errorf("failed to unregister worktree: %w", unregErr))
	}
	unregistered = true

	out.Success("Detached worktree %s", style.ColorBranchName(snapshot.Info.Name, false))
	return nil
}

func RepairAction(ctx *app.Context, opts RepairOptions) (*RepairResult, error) {
	listResult, err := listEntries(ctx, ListOptions(opts))
	if err != nil {
		return nil, err
	}
	if opts.NameOrBranch != "" && len(listResult.Worktrees) == 0 {
		return nil, fmt.Errorf("no worktree found for %s", style.ColorBranchName(opts.NameOrBranch, false))
	}

	result := &RepairResult{
		Repaired: []RepairEntry{},
		Skipped:  []SkippedEntry{},
	}

	for _, entry := range listResult.Worktrees {
		if !entry.NeedsRepair {
			result.Skipped = append(result.Skipped, SkippedEntry{
				Name:   entry.displayName(),
				Reason: "registration is already healthy",
			})
			continue
		}

		repaired, err := repairEntry(ctx, entry)
		if err != nil {
			result.Skipped = append(result.Skipped, SkippedEntry{
				Name:   entry.displayName(),
				Reason: err.Error(),
			})
			continue
		}
		result.Repaired = append(result.Repaired, repaired)
	}

	return result, nil
}

func repairEntry(ctx *app.Context, entry Entry) (RepairEntry, error) {
	switch entry.RegistrationState {
	case RegistrationStateLegacy:
		wtInfo, err := findWorktreeByNameOrBranch(ctx, entry.AnchorBranch)
		if err != nil {
			return RepairEntry{}, err
		}
		anchorName, err := convertLegacyRegistration(ctx, *wtInfo, entry.AnchorBranch)
		if err != nil {
			return RepairEntry{}, err
		}
		return RepairEntry{
			Name:         entry.displayName(),
			Action:       "converted legacy registration to hidden anchor",
			AnchorBranch: anchorName,
		}, nil

	case RegistrationStateInvalid:
		wtInfo, err := findWorktreeByNameOrBranch(ctx, entry.AnchorBranch)
		if err != nil {
			return RepairEntry{}, err
		}

		if !entry.Exists {
			if err := ctx.Engine.UnregisterWorktree(wtInfo.AnchorBranch); err != nil {
				return RepairEntry{}, fmt.Errorf("failed to remove stale registration: %w", err)
			}
			return RepairEntry{
				Name:   entry.displayName(),
				Action: "removed stale registration",
			}, nil
		}

		if entry.CurrentBranch == "" {
			return RepairEntry{}, fmt.Errorf("cannot repair %s automatically because the worktree has no branch checked out", style.ColorBranchName(entry.displayName(), false))
		}

		currentBranch := ctx.Engine.GetBranch(entry.CurrentBranch)
		if !currentBranch.IsTracked() {
			return RepairEntry{}, fmt.Errorf("cannot repair %s automatically because %s is not tracked by stackit", style.ColorBranchName(entry.displayName(), false), style.ColorBranchName(entry.CurrentBranch, false))
		}

		stackRootName := ctx.Engine.GetStackRootForBranch(currentBranch)
		if stackRootName == "" {
			stackRootName = entry.CurrentBranch
		}
		stackRoot := ctx.Engine.GetBranch(stackRootName)
		if !stackRoot.IsTracked() {
			return RepairEntry{}, fmt.Errorf("cannot repair %s automatically because no tracked stack root could be determined", style.ColorBranchName(entry.displayName(), false))
		}

		if ctx.Engine.IsWorktreeAnchor(stackRoot) {
			existing, err := ctx.Engine.GetWorktreeForStack(stackRootName)
			if err != nil {
				return RepairEntry{}, fmt.Errorf("failed to inspect anchor registration: %w", err)
			}
			if existing != nil && existing.Path != wtInfo.Path {
				return RepairEntry{}, fmt.Errorf("cannot repair %s automatically because anchor %s is already registered to %s", style.ColorBranchName(entry.displayName(), false), style.ColorBranchName(stackRootName, false), existing.Path)
			}
			if err := ctx.Engine.RegisterWorktreeWithName(stackRootName, wtInfo.Path, wtInfo.Name); err != nil {
				return RepairEntry{}, fmt.Errorf("failed to register worktree under anchor %s: %w", stackRootName, err)
			}
			if err := ctx.Engine.UnregisterWorktree(wtInfo.AnchorBranch); err != nil {
				_ = ctx.Engine.UnregisterWorktree(stackRootName)
				return RepairEntry{}, fmt.Errorf("failed to remove stale registration %s: %w", style.ColorBranchName(wtInfo.AnchorBranch, false), err)
			}
			return RepairEntry{
				Name:         entry.displayName(),
				Action:       "moved registration to existing anchor",
				AnchorBranch: stackRootName,
			}, nil
		}

		anchorName, err := convertLegacyRegistration(ctx, *wtInfo, stackRootName)
		if err != nil {
			return RepairEntry{}, err
		}
		return RepairEntry{
			Name:         entry.displayName(),
			Action:       "recovered worktree by inserting hidden anchor",
			AnchorBranch: anchorName,
		}, nil
	}

	return RepairEntry{}, fmt.Errorf("registration is already healthy")
}

func convertLegacyRegistration(ctx *app.Context, wtInfo engine.WorktreeInfo, rootBranchName string) (string, error) {
	rootBranch := ctx.Engine.GetBranch(rootBranchName)
	if !rootBranch.IsTracked() {
		return "", fmt.Errorf("branch %s is not tracked by stackit", style.ColorBranchName(rootBranchName, false))
	}

	originalParent := ctx.Engine.Trunk().GetName()
	if parent := rootBranch.GetParent(); parent != nil {
		originalParent = parent.GetName()
	}
	scope := rootBranch.GetScope().String()
	name := wtInfo.Name
	if name == "" {
		name = rootBranchName
	}

	anchorBranchName, err := generateAnchorBranchName(ctx, wtInfo.MainRepoDir, name, scope)
	if err != nil {
		return "", err
	}
	if ctx.Engine.BranchNames().Contains(anchorBranchName) {
		return "", fmt.Errorf("generated anchor branch %s already exists", style.ColorBranchName(anchorBranchName, false))
	}

	parentBranch := ctx.Engine.GetBranch(originalParent)
	parentSHA, err := parentBranch.GetRevision()
	if err != nil {
		return "", fmt.Errorf("failed to get parent revision for %s: %w", style.ColorBranchName(originalParent, false), err)
	}
	if err := ctx.Engine.CreateBranch(ctx.Context, anchorBranchName, parentSHA); err != nil {
		return "", fmt.Errorf("failed to create anchor branch %s: %w", style.ColorBranchName(anchorBranchName, false), err)
	}

	anchorBranch := ctx.Engine.GetBranch(anchorBranchName)
	anchorCreated := true
	rootReparented := false
	registered := false
	cleanup := func() {
		if registered {
			_ = ctx.Engine.UnregisterWorktree(anchorBranchName)
		}
		if rootReparented {
			_ = ctx.Engine.ReparentBranch(ctx.Context, ctx.Engine.GetBranch(rootBranchName), ctx.Engine.GetBranch(originalParent))
		}
		if anchorCreated {
			cleanupAnchorBranch(ctx.Context, ctx.Engine, anchorBranchName, ctx.Output)
		}
	}

	if err := ctx.Engine.SetParent(ctx.Context, anchorBranch, parentBranch); err != nil {
		cleanup()
		return "", fmt.Errorf("failed to set parent on anchor branch %s: %w", style.ColorBranchName(anchorBranchName, false), err)
	}
	if err := ctx.Engine.SetBranchType(anchorBranch, git.BranchTypeWorktreeAnchor); err != nil {
		cleanup()
		return "", fmt.Errorf("failed to mark %s as a worktree anchor: %w", style.ColorBranchName(anchorBranchName, false), err)
	}
	if scope != "" {
		if err := ctx.Engine.SetScope(ctx.Context, anchorBranch, engine.NewScope(scope)); err != nil {
			cleanup()
			return "", fmt.Errorf("failed to set scope on anchor branch %s: %w", style.ColorBranchName(anchorBranchName, false), err)
		}
	}
	if err := ctx.Engine.ReparentBranch(ctx.Context, ctx.Engine.GetBranch(rootBranchName), anchorBranch); err != nil {
		cleanup()
		return "", fmt.Errorf("failed to reparent %s under anchor %s: %w", style.ColorBranchName(rootBranchName, false), style.ColorBranchName(anchorBranchName, false), err)
	}
	rootReparented = true
	if err := ctx.Engine.RegisterWorktreeWithName(anchorBranchName, wtInfo.Path, wtInfo.Name); err != nil {
		cleanup()
		return "", fmt.Errorf("failed to register worktree under anchor %s: %w", style.ColorBranchName(anchorBranchName, false), err)
	}
	registered = true
	if err := ctx.Engine.UnregisterWorktree(wtInfo.AnchorBranch); err != nil {
		cleanup()
		return "", fmt.Errorf("failed to remove legacy registration %s: %w", style.ColorBranchName(wtInfo.AnchorBranch, false), err)
	}
	return anchorBranchName, nil
}

func removeWorktreePath(ctx *app.Context, path string, force bool) error {
	if force {
		return ctx.Engine.ForceRemoveWorktree(ctx.Context, path)
	}
	return ctx.Engine.RemoveWorktree(ctx.Context, path)
}
