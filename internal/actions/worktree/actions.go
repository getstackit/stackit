// Package worktree provides actions for managing stackit-managed worktrees.
package worktree

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/config"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/output"
	worktreeutil "github.com/getstackit/stackit/internal/worktree"
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

// worktreeMutation records completed lifecycle steps so failures can restore
// exactly the state that existed before a remove or detach operation.
type worktreeMutation struct {
	snapshot      *worktreeSnapshot
	pathRemoved   bool
	reparented    bool
	anchorDeleted bool
	unregistered  bool
}

func (m *worktreeMutation) rollback(ctx *app.Context, cause error, restoreChildren bool) error {
	return rollbackWorktree(cause,
		func() error {
			if !m.anchorDeleted {
				return nil
			}
			return restoreAnchorBranch(ctx, m.snapshot)
		},
		func() error {
			if !restoreChildren || !m.reparented || len(m.snapshot.ChildNames) == 0 {
				return nil
			}
			return ctx.Engine.ReparentBranches(ctx.Context, m.snapshot.ChildNames, ctx.Engine.GetBranch(m.snapshot.Info.AnchorBranch))
		},
		func() error {
			if !m.unregistered {
				return nil
			}
			return restoreWorktreeRegistration(ctx, m.snapshot)
		},
		func() error {
			if !m.pathRemoved {
				return nil
			}
			return restoreWorktreePath(ctx, m.snapshot)
		},
	)
}

// RemoveOptions contains options for the remove action
type RemoveOptions struct {
	Selector WorktreeSelector // Worktree name or anchor branch
	Policy   RemovalPolicy    // Whether removal may discard uncommitted changes
}

// RemovalPolicy makes destructive worktree operations explicit at the action
// boundary while sharing the low-level path-removal policy.
type RemovalPolicy = worktreeutil.RemovalPolicy

const (
	RemovalRespectChanges = worktreeutil.RemovalRespectChanges
	RemovalDiscardChanges = worktreeutil.RemovalDiscardChanges
)

// RemovalPolicyForForce converts the CLI's --force flag at the boundary where
// it is parsed; lifecycle code only receives an explicit policy.
func RemovalPolicyForForce(force bool) RemovalPolicy {
	if force {
		return RemovalDiscardChanges
	}
	return RemovalRespectChanges
}

func snapshotWorktree(ctx *app.Context, entry *Entry) (*worktreeSnapshot, error) {
	checkoutBranch := entry.CurrentBranch
	if checkoutBranch == "" {
		if root := entry.primaryRootBranch(); root != "" {
			checkoutBranch = root
		} else {
			checkoutBranch = entry.AnchorBranch
		}
	}

	snapshot := &worktreeSnapshot{
		Info:           entry.registration,
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
	return ctx.Engine.RegisterWorktree(ctx.Context, snapshot.Info.Registration())
}

func restoreWorktreePath(ctx *app.Context, snapshot *worktreeSnapshot) error {
	if snapshot.CheckoutBranch == "" {
		return nil
	}
	return ctx.Engine.AddWorktree(ctx.Context, snapshot.Info.Path.String(), snapshot.CheckoutBranch, git.WorktreeAttached)
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

func rollbackWorktree(cause error, restorations ...func() error) error {
	var rollbackErrs []string
	for _, restore := range restorations {
		if err := restore(); err != nil {
			rollbackErrs = append(rollbackErrs, err.Error())
		}
	}
	if len(rollbackErrs) == 0 {
		return cause
	}
	return fmt.Errorf("%w (rollback failed: %s)", cause, strings.Join(rollbackErrs, "; "))
}

// removeWorktreeOrPruneMissing removes a registered Git worktree. A manually
// deleted directory still needs its Git administrative entry pruned, but a
// prune failure is non-fatal: the lifecycle operation can safely continue and
// a later creation will retry the prune.
func removeWorktreeOrPruneMissing(ctx *app.Context, path string, policy RemovalPolicy) (bool, error) {
	removed, err := worktreeutil.RemovePath(ctx.Context, ctx.Engine, path, policy)
	if err != nil {
		return false, err
	}
	if removed {
		return true, nil
	}

	ctx.Output.Debug("Worktree path %s does not exist, skipping removal", path)
	if err := ctx.Engine.PruneWorktrees(ctx.Context); err != nil {
		ctx.Output.Debug("Failed to prune worktrees: %v", err)
	}
	return false, nil
}

// RemoveAction removes a worktree for a stack
func RemoveAction(ctx *app.Context, opts RemoveOptions) error {
	out := ctx.Output

	entry, err := getWorktreeEntry(ctx, opts.Selector)
	if err != nil {
		return err
	}
	if refusal := entry.removeRefusal(opts.Policy); refusal != "" {
		return errors.New(refusal)
	}

	snapshot, err := snapshotWorktree(ctx, entry)
	if err != nil {
		return err
	}

	mutation := worktreeMutation{snapshot: snapshot}
	var removeErr error
	mutation.pathRemoved, removeErr = removeWorktreeOrPruneMissing(ctx, snapshot.Info.Path.String(), opts.Policy)
	if removeErr != nil {
		if opts.Policy.DiscardsChanges() {
			return fmt.Errorf("failed to force remove worktree at %s: %w", snapshot.Info.Path, removeErr)
		}
		return fmt.Errorf("failed to remove worktree at %s: %w (use --force to discard uncommitted changes)", snapshot.Info.Path, removeErr)
	}

	// Delete the anchor before unregistering, the same order detach uses.
	// Unregistering first means an interruption in between leaves an anchor
	// branch with no registration — invisible to every worktree command and
	// unreachable by repair. This order fails the other way: a leftover
	// registration is visible in `worktree list` and can be repaired or removed.
	if snapshot.AnchorExists {
		if err := ctx.Engine.DeleteBranch(ctx.Context, ctx.Engine.GetBranch(snapshot.Info.AnchorBranch)); err != nil {
			return mutation.rollback(ctx, fmt.Errorf("failed to delete anchor branch %s: %w", snapshot.Info.AnchorBranch, err), false)
		}
		mutation.anchorDeleted = true
		out.Debug("Deleted anchor branch %s", snapshot.Info.AnchorBranch)
	}

	if unregErr := ctx.Engine.UnregisterWorktree(ctx.Context, snapshot.Info.AnchorBranch); unregErr != nil {
		return mutation.rollback(ctx, fmt.Errorf("failed to unregister worktree: %w", unregErr), false)
	}
	mutation.unregistered = true

	out.Success("Removed worktree for stack %s", output.BranchName(snapshot.Info.AnchorBranch))
	return nil
}

// OpenOptions contains options for the open action
type OpenOptions struct {
	Selector WorktreeSelector // Worktree name or anchor branch
}

// OpenAction returns the path to a worktree for a stack
func OpenAction(ctx *app.Context, opts OpenOptions) (WorktreePath, error) {
	wtInfo, err := findWorktreeByNameOrBranch(ctx, opts.Selector)
	if err != nil {
		return "", err
	}

	if _, statErr := os.Stat(wtInfo.Path.String()); statErr != nil {
		if os.IsNotExist(statErr) {
			return "", fmt.Errorf("worktree path %s does not exist (worktree may have been manually deleted)", wtInfo.Path)
		}
		return "", fmt.Errorf("inspect worktree path %s: %w", wtInfo.Path, statErr)
	}

	return wtInfo.Path, nil
}

// CreateOptions contains options for the create action
type CreateOptions struct {
	Name  string // User-provided name for the worktree
	Scope string // Optional scope to set on the anchor branch
}

// WorktreeResult describes a created or attached managed worktree.
type WorktreeResult struct {
	Name         string       // The name of the worktree
	AnchorBranch string       // The name of the anchor branch
	Path         WorktreePath // The path to the worktree
}

// CreateAction creates a new worktree with an anchor branch
func CreateAction(ctx *app.Context, opts CreateOptions) (*WorktreeResult, error) {
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
			LinearStacks:      mainCfg.StackShape() == config.StackShapeLinear,
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

	created, err := createAnchoredWorktree(ctx, eng, repoRoot, anchoredWorktreeOptions{
		Name:         opts.Name,
		Scope:        opts.Scope,
		AnchorParent: trunk.GetName(),
	})
	if err != nil {
		return nil, err
	}

	out.Success("Created worktree %s", output.BranchName(opts.Name))
	reportCreatedWorktree(ctx, created, opts.Scope)

	return created, nil
}

type anchoredWorktreeOptions struct {
	Name           string
	Scope          string
	AnchorParent   string
	RootBranch     string
	OriginalParent string
}

func reportCreatedWorktree(ctx *app.Context, created *WorktreeResult, scope string) {
	ctx.Output.Info("  Anchor branch: %s", output.BranchName(created.AnchorBranch))
	ctx.Output.Info("  Path: %s", output.Dim(created.Path.String()))
	if scope != "" {
		ctx.Output.Info("  Scope: %s", output.Dim(scope))
	}
	ctx.Output.Newline()
	if err := RunPostCreateHooks(ctx, created.Path.String()); err != nil {
		ctx.Output.Warn("Post-create hooks failed: %v", err)
	}
}

type worktreeTarget struct {
	Path       WorktreePath
	AnchorName string
}

func resolveWorktreeTarget(ctx *app.Context, eng engine.Engine, repoRoot, name, scope string) (worktreeTarget, error) {
	if name == "" {
		return worktreeTarget{}, fmt.Errorf("worktree name is required")
	}
	if strings.ContainsAny(name, "/\\:*?\"<>|") {
		return worktreeTarget{}, fmt.Errorf("worktree name cannot contain path separators or special characters: /\\:*?\"<>|")
	}

	existingWorktrees, err := eng.ListManagedWorktrees()
	if err != nil {
		return worktreeTarget{}, fmt.Errorf("failed to check existing worktrees: %w", err)
	}
	for _, wt := range existingWorktrees {
		if wt.Name == engine.WorktreeName(name) {
			return worktreeTarget{}, fmt.Errorf("worktree with name %q already exists", name)
		}
	}

	cfg, err := config.LoadConfig(repoRoot)
	if err != nil {
		return worktreeTarget{}, fmt.Errorf("load worktree configuration: %w", err)
	}
	path := WorktreePath(worktreePathForName(cfg.WorktreeBasePath(), repoRoot, name))
	if _, err := os.Stat(path.String()); err == nil {
		return worktreeTarget{}, fmt.Errorf("worktree path %s already exists; remove it first or choose a different name", path)
	} else if !os.IsNotExist(err) {
		return worktreeTarget{}, fmt.Errorf("inspect worktree path %s: %w", path, err)
	}

	anchorName, err := generateAnchorBranchName(ctx, cfg.BranchNamePattern(), name, scope)
	if err != nil {
		return worktreeTarget{}, err
	}
	return worktreeTarget{Path: path, AnchorName: anchorName}, nil
}

// createWorktreeAnchor creates and configures the hidden branch that owns a
// managed worktree. Call the returned cleanup function if a later lifecycle
// step fails.
func createWorktreeAnchor(ctx *app.Context, eng engine.Engine, anchorName, parentName, scope string) (engine.Branch, func(), error) {
	if eng.BranchNames().Contains(anchorName) {
		return engine.Branch{}, nil, fmt.Errorf("branch %s already exists", anchorName)
	}

	parentBranch := eng.GetBranch(parentName)
	parentSHA, err := parentBranch.GetRevision()
	if err != nil {
		return engine.Branch{}, nil, fmt.Errorf("failed to get anchor parent revision: %w", err)
	}
	if err := eng.CreateBranch(ctx.Context, anchorName, parentSHA); err != nil {
		return engine.Branch{}, nil, fmt.Errorf("failed to create anchor branch: %w", err)
	}

	anchorBranch := eng.GetBranch(anchorName)
	cleanup := func() {
		cleanupAnchorBranch(ctx.Context, eng, anchorName, ctx.Output)
	}
	if err := eng.SetParent(ctx.Context, anchorBranch, parentBranch, engine.DivergenceRecompute); err != nil {
		cleanup()
		return engine.Branch{}, nil, fmt.Errorf("failed to set anchor parent: %w", err)
	}
	if err := eng.SetBranchType(anchorBranch, git.BranchTypeWorktreeAnchor); err != nil {
		cleanup()
		return engine.Branch{}, nil, fmt.Errorf("failed to set anchor branch type: %w", err)
	}
	if scope != "" {
		if err := eng.SetScope(ctx.Context, anchorBranch, engine.NewScope(scope)); err != nil {
			cleanup()
			return engine.Branch{}, nil, fmt.Errorf("failed to set anchor scope: %w", err)
		}
	}

	return anchorBranch, cleanup, nil
}

// CreateAnchoredWorktreeForBranch creates a hidden anchor for branchName, moves branchName
// under it without rebasing, and opens a worktree checked out on branchName.
func CreateAnchoredWorktreeForBranch(ctx *app.Context, branchName string, name string, scope string) (*WorktreeResult, error) {
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
	return created, nil
}

func createAnchoredWorktree(ctx *app.Context, eng engine.Engine, repoRoot string, opts anchoredWorktreeOptions) (*WorktreeResult, error) {
	out := ctx.Output
	if opts.AnchorParent == "" {
		opts.AnchorParent = eng.Trunk().GetName()
	}
	if opts.OriginalParent == "" {
		opts.OriginalParent = opts.AnchorParent
	}
	target, err := resolveWorktreeTarget(ctx, eng, repoRoot, opts.Name, opts.Scope)
	if err != nil {
		return nil, err
	}
	worktreePath := target.Path.String()
	anchorBranchName := target.AnchorName
	anchorBranch, cleanupAnchor, err := createWorktreeAnchor(ctx, eng, anchorBranchName, opts.AnchorParent, opts.Scope)
	if err != nil {
		return nil, err
	}
	rootReparented := false
	worktreeCreated := false
	registered := false

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
		cleanupAnchor()
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

	if err := eng.AddWorktree(ctx.Context, worktreePath, checkoutBranch, git.WorktreeAttached); err != nil {
		cleanup()
		return nil, fmt.Errorf("failed to create worktree: %w", err)
	}
	worktreeCreated = true

	warmStart, err := warmStartWorktree(ctx.Context, eng, repoRoot, worktreePath)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("failed to warm-start worktree: %w", err)
	}
	if warmStart.Enabled && len(warmStart.Copied) > 0 {
		out.Info("Warm-started worktree with %d ignored file(s)", len(warmStart.Copied))
	}
	for _, skip := range warmStart.SkippedUnsafe {
		out.Warn("Did not warm-start %s: %s (selected by %s)", skip.Path, skip.Reason, WorktreeIncludeFile)
	}

	if err := eng.RegisterWorktree(ctx.Context, engine.WorktreeRegistration{
		AnchorBranch: anchorBranchName,
		Path:         WorktreePath(worktreePath),
		Name:         engine.WorktreeName(opts.Name),
	}); err != nil {
		cleanup()
		return nil, fmt.Errorf("failed to register worktree: %w", err)
	}
	registered = true

	return &WorktreeResult{
		Name:         opts.Name,
		AnchorBranch: anchorBranchName,
		Path:         WorktreePath(worktreePath),
	}, nil
}

func generateAnchorBranchName(ctx *app.Context, patternStr, name, scope string) (string, error) {
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

func worktreePathForName(basePath, repoRoot, name string) string {
	// Default: sibling directory named {repo}-stacks
	if basePath == "" {
		repoName := filepath.Base(repoRoot)
		basePath = filepath.Join(filepath.Dir(repoRoot), repoName+"-stacks")
	} else if !filepath.IsAbs(basePath) {
		// Configuration lives with the repository, so relative worktree bases
		// must not depend on the directory from which stackit was invoked.
		basePath = filepath.Join(repoRoot, basePath)
	}

	return filepath.Join(basePath, name)
}

// cleanupAnchorBranch cleans up an anchor branch on failure and logs any cleanup errors
func cleanupAnchorBranch(ctx context.Context, eng engine.Engine, branchName string, out output.Output) {
	if err := eng.DeleteBranch(ctx, eng.GetBranch(branchName)); err != nil {
		out.Warn("Failed to clean up anchor branch %s: %v", branchName, err)
	}
}
