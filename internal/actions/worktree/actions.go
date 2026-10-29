// Package worktree provides actions for managing stackit-managed worktrees.
package worktree

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/actions/handler"
	"github.com/getstackit/stackit/internal/config"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/output"
	worktreeutil "github.com/getstackit/stackit/internal/worktree"
)

// Deps are the collaborators the worktree lifecycle actions need. Adapters
// build them from their own runtime state; the actions never see bootstrap
// types.
type Deps struct {
	// Engine is the full engine. Worktree lifecycle spans registrations, branch
	// creation and deletion, metadata, reparenting, and physical checkouts, and
	// CreateAction swaps in a freshly built main-repository engine of the same
	// type, so a narrow interface would restate most of engine.Engine.
	Engine engine.Engine
	Out    output.Output
	// Log is handed to the git runner of the main-repository engine CreateAction
	// builds when invoked from inside a managed worktree.
	Log      output.Logger
	RepoRoot string
	// Here is where the command runs: the main repository or a managed worktree.
	Here actions.Here
	// Config is the repository configuration already loaded at bootstrap. Nil
	// means none was loaded. Lifecycle actions read resolved values from it and
	// post-create hooks use it as their approval store.
	Config config.Configurer
	// Prompts asks the user to approve unseen post-worktree-create hooks.
	Prompts handler.Prompter
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

// worktreeMutation records completed lifecycle steps so failures can restore
// exactly the state that existed before a remove or detach operation.
type worktreeMutation struct {
	snapshot      *worktreeSnapshot
	pathRemoved   bool
	reparented    bool
	anchorDeleted bool
	unregistered  bool
}

// ChildRestoreMode controls whether rollback also reparents children back to
// the worktree's anchor branch. Remove never reparents children (they stay on
// the anchor), so it always skips this step; detach reparents them away from
// the anchor before deleting it, so it must restore them on failure.
type ChildRestoreMode int

const (
	SkipChildRestore ChildRestoreMode = iota
	RestoreChildren
)

func (m *worktreeMutation) rollback(ctx context.Context, deps Deps, cause error, childMode ChildRestoreMode) error {
	return rollbackWorktree(cause,
		func() error {
			if !m.anchorDeleted {
				return nil
			}
			return restoreAnchorBranch(ctx, deps.Engine, m.snapshot)
		},
		func() error {
			if childMode != RestoreChildren || !m.reparented || len(m.snapshot.ChildNames) == 0 {
				return nil
			}
			return deps.Engine.ReparentBranchesToParents(ctx, engine.MovesTo(m.snapshot.ChildNames, m.snapshot.Info.AnchorBranch), engine.ReparentOpts{})
		},
		func() error {
			if !m.unregistered {
				return nil
			}
			return restoreWorktreeRegistration(ctx, deps.Engine, m.snapshot)
		},
		func() error {
			if !m.pathRemoved {
				return nil
			}
			return restoreWorktreePath(ctx, deps.Engine, m.snapshot)
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

func snapshotWorktree(ctx context.Context, deps Deps, entry *Entry) (*worktreeSnapshot, error) {
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

	anchorBranch := deps.Engine.GetBranch(entry.AnchorBranch)
	if !deps.Engine.BranchNames().Contains(entry.AnchorBranch) {
		return snapshot, nil
	}

	snapshot.AnchorExists = true
	snapshot.AnchorScope = deps.Engine.GetScope(anchorBranch).String()
	if parent := anchorBranch.Parent(); parent != nil {
		snapshot.AnchorParent = parent.Name()
	} else {
		snapshot.AnchorParent = deps.Engine.Trunk().Name()
	}
	anchorRev, err := anchorBranch.Revision(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get revision for %s: %w", entry.AnchorBranch, err)
	}
	snapshot.AnchorRev = anchorRev

	graph := deps.Engine.Graph(ctx, engine.SortStrategyAlphabetical)
	snapshot.ChildNames = append(snapshot.ChildNames, graph.Children(anchorBranch)...)
	return snapshot, nil
}

func restoreWorktreeRegistration(ctx context.Context, eng engine.Engine, snapshot *worktreeSnapshot) error {
	return eng.RegisterWorktree(ctx, snapshot.Info.Registration())
}

func restoreWorktreePath(ctx context.Context, eng engine.Engine, snapshot *worktreeSnapshot) error {
	if snapshot.CheckoutBranch == "" {
		return nil
	}
	return eng.AddWorktree(ctx, snapshot.Info.Path, snapshot.CheckoutBranch, git.WorktreeAttached)
}

func restoreAnchorBranch(ctx context.Context, eng engine.Engine, snapshot *worktreeSnapshot) error {
	if !snapshot.AnchorExists || snapshot.AnchorRev == "" {
		return nil
	}
	anchorName := snapshot.Info.AnchorBranch
	if eng.BranchNames().Contains(anchorName) {
		return nil
	}
	if err := eng.CreateBranch(ctx, anchorName, snapshot.AnchorRev); err != nil {
		return fmt.Errorf("failed to recreate anchor branch %s: %w", anchorName, err)
	}
	anchorBranch := eng.GetBranch(anchorName)
	if err := eng.SetParent(ctx, anchorBranch, eng.GetBranch(snapshot.AnchorParent), engine.DivergenceRecompute); err != nil {
		return fmt.Errorf("failed to restore anchor parent for %s: %w", anchorName, err)
	}
	if err := eng.SetBranchType(ctx, anchorBranch, git.BranchTypeWorktreeAnchor); err != nil {
		return fmt.Errorf("failed to restore anchor branch type for %s: %w", anchorName, err)
	}
	if snapshot.AnchorScope != "" {
		if err := eng.SetScope(ctx, anchorBranch, engine.NewScope(snapshot.AnchorScope)); err != nil {
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
func removeWorktreeOrPruneMissing(ctx context.Context, deps Deps, path WorktreePath, policy RemovalPolicy) (bool, error) {
	removed, err := worktreeutil.RemovePath(ctx, deps.Engine, path, policy)
	if err != nil {
		return false, err
	}
	if removed {
		return true, nil
	}

	deps.Out.Debug("Worktree path %s does not exist, skipping removal", path)
	if err := deps.Engine.PruneWorktrees(ctx); err != nil {
		deps.Out.Debug("Failed to prune worktrees: %v", err)
	}
	return false, nil
}

// RemoveAction removes a worktree for a stack
func RemoveAction(ctx context.Context, deps Deps, opts RemoveOptions) error {
	out := deps.Out

	entry, err := getWorktreeEntry(ctx, deps, opts.Selector)
	if err != nil {
		return err
	}
	if refusal := entry.removeRefusal(opts.Policy); refusal != "" {
		return errors.New(refusal)
	}

	snapshot, err := snapshotWorktree(ctx, deps, entry)
	if err != nil {
		return err
	}

	mutation := worktreeMutation{snapshot: snapshot}
	var removeErr error
	mutation.pathRemoved, removeErr = removeWorktreeOrPruneMissing(ctx, deps, snapshot.Info.Path, opts.Policy)
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
		if err := deps.Engine.DeleteBranch(ctx, deps.Engine.GetBranch(snapshot.Info.AnchorBranch)); err != nil {
			return mutation.rollback(ctx, deps, fmt.Errorf("failed to delete anchor branch %s: %w", snapshot.Info.AnchorBranch, err), SkipChildRestore)
		}
		mutation.anchorDeleted = true
		out.Debug("Deleted anchor branch %s", snapshot.Info.AnchorBranch)
	}

	if unregErr := deps.Engine.UnregisterWorktree(ctx, snapshot.Info.AnchorBranch); unregErr != nil {
		return mutation.rollback(ctx, deps, fmt.Errorf("failed to unregister worktree: %w", unregErr), SkipChildRestore)
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
func OpenAction(ctx context.Context, deps Deps, opts OpenOptions) (WorktreePath, error) {
	wtInfo, err := findWorktreeByNameOrBranch(ctx, deps, opts.Selector)
	if err != nil {
		return "", err
	}

	if _, statErr := os.Stat(wtInfo.Path.String()); statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
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
	// Config is the main repository's resolved configuration. Worktrees are
	// always created from the main repository, so when the command runs inside
	// a managed worktree the caller resolves the main repository's config.
	// Nil falls back to Deps.Config.
	Config RepoConfig
}

// RepoConfig is the resolved repository configuration worktree lifecycle
// actions read. The loaded repository config satisfies it; actions never load
// config from disk themselves.
type RepoConfig interface {
	Trunk() string
	UndoStackDepth() int
	LinearStacks() bool
	WorktreeBasePath() string
	BranchNamePattern() string
}

// resolveRepoConfig returns cfg, or the loaded config when cfg is nil.
func resolveRepoConfig(cfg RepoConfig, loaded config.Configurer) (RepoConfig, error) {
	if cfg != nil {
		return cfg, nil
	}
	if loaded != nil {
		return loaded, nil
	}
	return nil, errors.New("repository config not loaded")
}

// WorktreeResult describes a created or attached managed worktree.
type WorktreeResult struct {
	Name         string       // The name of the worktree
	AnchorBranch string       // The name of the anchor branch
	Path         WorktreePath // The path to the worktree
}

// CreateAction creates a new worktree with an anchor branch
func CreateAction(ctx context.Context, deps Deps, opts CreateOptions) (*WorktreeResult, error) {
	eng := deps.Engine
	out := deps.Out
	repoRoot := deps.RepoRoot

	cfg, err := resolveRepoConfig(opts.Config, deps.Config)
	if err != nil {
		return nil, err
	}

	// If we're in a managed worktree, we need to create the new worktree from the main repo
	if deps.Here.InManagedWorktree && deps.Here.Worktree != nil {
		out.Info("Creating worktree from main repository (currently in worktree: %s)", deps.Here.Worktree.Name)

		// Create a temporary engine for the main repository
		mainRepoRoot := deps.Here.Worktree.MainRepoDir
		mainGit := git.NewRunnerWithPath(mainRepoRoot, deps.Log)

		// Trunk and undo settings come from the main repo's config (opts.Config).
		mainEng, err := engine.NewEngine(ctx, engine.Options{
			RepoRoot:          mainRepoRoot,
			Trunk:             cfg.Trunk(),
			MaxUndoStackDepth: cfg.UndoStackDepth(),
			LinearStacks:      cfg.LinearStacks(),
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
	remoteCtx, cancelRemote := git.WithRemoteOperationTimeout(ctx)
	trunkStatus := eng.ReadBranchRemoteStatuses(remoteCtx, engine.BranchesOf(trunk)).ForBranch(trunk)
	cancelRemote()
	if trunkStatus.Behind() {
		out.Warn("Local %s is behind remote. Consider running 'st sync' first.", trunk.Name())
	}

	// Inform user if not on trunk (worktree is always created from trunk)
	currentBranch := eng.CurrentBranch(ctx)
	if currentBranch == nil {
		out.Info("Creating worktree from %s (currently in detached HEAD state)", trunk.Name())
	} else if currentBranch.Name() != trunk.Name() {
		out.Info("Creating worktree from %s (current branch: %s)", trunk.Name(), currentBranch.Name())
	}

	created, err := createAnchoredWorktree(ctx, deps, eng, repoRoot, anchoredWorktreeOptions{
		Name:         opts.Name,
		Scope:        opts.Scope,
		AnchorParent: trunk.Name(),
		Config:       cfg,
	})
	if err != nil {
		return nil, err
	}

	out.Success("Created worktree %s", output.BranchName(opts.Name))
	reportCreatedWorktree(ctx, deps, created, opts.Scope)

	return created, nil
}

type anchoredWorktreeOptions struct {
	Name           string
	Scope          string
	AnchorParent   string
	RootBranch     string
	OriginalParent string
	Config         RepoConfig
}

func reportCreatedWorktree(ctx context.Context, deps Deps, created *WorktreeResult, scope string) {
	deps.Out.Info("  Anchor branch: %s", output.BranchName(created.AnchorBranch))
	deps.Out.Info("  Path: %s", output.Dim(created.Path.String()))
	if scope != "" {
		deps.Out.Info("  Scope: %s", output.Dim(scope))
	}
	deps.Out.Newline()
	if err := RunPostCreateHooks(ctx, deps, created.Path); err != nil {
		deps.Out.Warn("Post-create hooks failed: %v", err)
	}
}

type worktreeTarget struct {
	Path       WorktreePath
	AnchorName string
}

func resolveWorktreeTarget(ctx context.Context, deps Deps, eng engine.Engine, cfg RepoConfig, repoRoot, name, scope string) (worktreeTarget, error) {
	if name == "" {
		return worktreeTarget{}, errors.New("worktree name is required")
	}
	if strings.ContainsAny(name, "/\\:*?\"<>|") {
		return worktreeTarget{}, errors.New("worktree name cannot contain path separators or special characters: /\\:*?\"<>|")
	}

	existingWorktrees, err := eng.ListManagedWorktrees(ctx)
	if err != nil {
		return worktreeTarget{}, fmt.Errorf("failed to check existing worktrees: %w", err)
	}
	for _, wt := range existingWorktrees {
		if wt.Name == engine.WorktreeName(name) {
			return worktreeTarget{}, fmt.Errorf("worktree with name %q already exists", name)
		}
	}

	path := worktreePathForName(cfg.WorktreeBasePath(), repoRoot, name)
	if _, err := os.Stat(path.String()); err == nil {
		return worktreeTarget{}, fmt.Errorf("worktree path %s already exists; remove it first or choose a different name", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return worktreeTarget{}, fmt.Errorf("inspect worktree path %s: %w", path, err)
	}

	anchorName, err := generateAnchorBranchName(ctx, deps.Engine, cfg.BranchNamePattern(), name, scope)
	if err != nil {
		return worktreeTarget{}, err
	}
	return worktreeTarget{Path: path, AnchorName: anchorName}, nil
}

// createWorktreeAnchor creates and configures the hidden branch that owns a
// managed worktree. Call the returned cleanup function if a later lifecycle
// step fails.
func createWorktreeAnchor(ctx context.Context, eng engine.Engine, out output.Output, anchorName, parentName, scope string) (engine.Branch, func(), error) {
	if eng.BranchNames().Contains(anchorName) {
		return engine.Branch{}, nil, fmt.Errorf("branch %s already exists", anchorName)
	}

	parentBranch := eng.GetBranch(parentName)
	parentSHA, err := parentBranch.Revision(ctx)
	if err != nil {
		return engine.Branch{}, nil, fmt.Errorf("failed to get anchor parent revision: %w", err)
	}
	if err := eng.CreateBranch(ctx, anchorName, parentSHA); err != nil {
		return engine.Branch{}, nil, fmt.Errorf("failed to create anchor branch: %w", err)
	}

	anchorBranch := eng.GetBranch(anchorName)
	cleanup := func() {
		cleanupAnchorBranch(ctx, eng, anchorName, out)
	}
	if err := eng.SetParent(ctx, anchorBranch, parentBranch, engine.DivergenceRecompute); err != nil {
		cleanup()
		return engine.Branch{}, nil, fmt.Errorf("failed to set anchor parent: %w", err)
	}
	if err := eng.SetBranchType(ctx, anchorBranch, git.BranchTypeWorktreeAnchor); err != nil {
		cleanup()
		return engine.Branch{}, nil, fmt.Errorf("failed to set anchor branch type: %w", err)
	}
	if scope != "" {
		if err := eng.SetScope(ctx, anchorBranch, engine.NewScope(scope)); err != nil {
			cleanup()
			return engine.Branch{}, nil, fmt.Errorf("failed to set anchor scope: %w", err)
		}
	}

	return anchorBranch, cleanup, nil
}

// CreateAnchoredWorktreeForBranch creates a hidden anchor for branchName, moves branchName
// under it without rebasing, and opens a worktree checked out on branchName.
func CreateAnchoredWorktreeForBranch(ctx context.Context, deps Deps, branchName string, name string, scope string) (*WorktreeResult, error) {
	if name == "" {
		name = branchName
	}
	parent := deps.Engine.Trunk().Name()
	if branch := deps.Engine.GetBranch(branchName); branch.IsTracked() {
		if currentParent := branch.Parent(); currentParent != nil {
			parent = currentParent.Name()
		}
	}

	cfg, err := resolveRepoConfig(nil, deps.Config)
	if err != nil {
		return nil, err
	}
	created, err := createAnchoredWorktree(ctx, deps, deps.Engine, deps.RepoRoot, anchoredWorktreeOptions{
		Name:           name,
		Scope:          scope,
		AnchorParent:   parent,
		RootBranch:     branchName,
		OriginalParent: parent,
		Config:         cfg,
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

// BranchWorktrees backs create --worktree: it satisfies create.Worktrees with
// CreateAnchoredWorktreeForBranch and RunPostCreateHooks over Deps.
type BranchWorktrees struct{ Deps Deps }

// CreateAnchored creates a managed worktree anchored on branchName and returns
// its path.
func (w BranchWorktrees) CreateAnchored(ctx context.Context, branchName, name, scope string) (WorktreePath, error) {
	created, err := CreateAnchoredWorktreeForBranch(ctx, w.Deps, branchName, name, scope)
	if err != nil {
		return "", err
	}
	return created.Path, nil
}

// RunPostCreateHooks runs the configured post-worktree-create hooks in path.
func (w BranchWorktrees) RunPostCreateHooks(ctx context.Context, path WorktreePath) error {
	return RunPostCreateHooks(ctx, w.Deps, path)
}

func createAnchoredWorktree(ctx context.Context, deps Deps, eng engine.Engine, repoRoot string, opts anchoredWorktreeOptions) (*WorktreeResult, error) {
	out := deps.Out
	if opts.AnchorParent == "" {
		opts.AnchorParent = eng.Trunk().Name()
	}
	if opts.OriginalParent == "" {
		opts.OriginalParent = opts.AnchorParent
	}
	target, err := resolveWorktreeTarget(ctx, deps, eng, opts.Config, repoRoot, opts.Name, opts.Scope)
	if err != nil {
		return nil, err
	}
	worktreePath := target.Path
	anchorBranchName := target.AnchorName
	anchorBranch, cleanupAnchor, err := createWorktreeAnchor(ctx, eng, deps.Out, anchorBranchName, opts.AnchorParent, opts.Scope)
	if err != nil {
		return nil, err
	}
	rootReparented := false
	worktreeCreated := false
	registered := false

	cleanup := func() {
		if registered {
			if err := eng.UnregisterWorktree(ctx, anchorBranchName); err != nil {
				out.Debug("Failed to unregister worktree during rollback: %v", err)
			}
		}
		if worktreeCreated && worktreePath != "" {
			if err := eng.ForceRemoveWorktree(ctx, worktreePath); err != nil {
				out.Debug("Failed to remove worktree during rollback: %v", err)
			}
		}
		if rootReparented && opts.RootBranch != "" {
			if err := eng.ReparentBranch(ctx, eng.GetBranch(opts.RootBranch), eng.GetBranch(opts.OriginalParent)); err != nil {
				out.Debug("Failed to restore parent for %s during rollback: %v", opts.RootBranch, err)
			}
		}
		cleanupAnchor()
	}

	checkoutBranch := anchorBranchName
	if opts.RootBranch != "" {
		if err := eng.ReparentBranch(ctx, eng.GetBranch(opts.RootBranch), anchorBranch); err != nil {
			cleanup()
			return nil, fmt.Errorf("failed to reparent %s under worktree anchor: %w", opts.RootBranch, err)
		}
		rootReparented = true
		checkoutBranch = opts.RootBranch
	}

	if err := eng.AddWorktree(ctx, worktreePath, checkoutBranch, git.WorktreeAttached); err != nil {
		cleanup()
		return nil, fmt.Errorf("failed to create worktree: %w", err)
	}
	worktreeCreated = true

	warmStart, err := warmStartWorktree(ctx, eng, WorktreePath(repoRoot), worktreePath)
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

	if err := eng.RegisterWorktree(ctx, engine.WorktreeRegistration{
		AnchorBranch: anchorBranchName,
		Path:         worktreePath,
		Name:         engine.WorktreeName(opts.Name),
	}); err != nil {
		cleanup()
		return nil, fmt.Errorf("failed to register worktree: %w", err)
	}
	registered = true

	return &WorktreeResult{
		Name:         opts.Name,
		AnchorBranch: anchorBranchName,
		Path:         worktreePath,
	}, nil
}

func generateAnchorBranchName(ctx context.Context, eng engine.Engine, patternStr, name, scope string) (string, error) {
	pattern, err := config.NewBranchPattern(patternStr)
	if err != nil {
		return "", fmt.Errorf("invalid branch pattern: %w", err)
	}

	anchorBranchName, err := pattern.GetBranchName(ctx, eng, name+"-wt", scope)
	if err != nil {
		return "", fmt.Errorf("failed to generate anchor branch name: %w", err)
	}
	return anchorBranchName, nil
}

func worktreePathForName(basePath, repoRoot, name string) WorktreePath {
	// Default: sibling directory named {repo}-stacks
	if basePath == "" {
		repoName := filepath.Base(repoRoot)
		basePath = filepath.Join(filepath.Dir(repoRoot), repoName+"-stacks")
	} else if !filepath.IsAbs(basePath) {
		// Configuration lives with the repository, so relative worktree bases
		// must not depend on the directory from which stackit was invoked.
		basePath = filepath.Join(repoRoot, basePath)
	}

	return WorktreePath(filepath.Join(basePath, name))
}

// cleanupAnchorBranch cleans up an anchor branch on failure and logs any cleanup errors
func cleanupAnchorBranch(ctx context.Context, eng engine.Engine, branchName string, out output.Output) {
	if err := eng.DeleteBranch(ctx, eng.GetBranch(branchName)); err != nil {
		out.Warn("Failed to clean up anchor branch %s: %v", branchName, err)
	}
}
