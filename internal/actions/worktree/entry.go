package worktree

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/output"
)

// ListOptions contains options for the list action
type ListOptions struct {
	Selector WorktreeSelector
}

// WorktreeSelector identifies a managed worktree by either its display name or
// hidden anchor branch. It keeps user-facing selectors distinct from branch
// names used for stack graph operations.
type WorktreeSelector string

func (s WorktreeSelector) String() string { return string(s) }

// WorktreePath is the engine-level identity of a managed checkout.
type WorktreePath = engine.WorktreePath

type RegistrationState string

const (
	RegistrationStateOK      RegistrationState = "ok"
	RegistrationStateLegacy  RegistrationState = "legacy"
	RegistrationStateInvalid RegistrationState = "invalid"
)

// WorktreePresence describes whether the registered directory still exists.
type WorktreePresence string

const (
	WorktreePresent WorktreePresence = "present"
	WorktreeMissing WorktreePresence = "missing"
)

// WorktreeCheckout describes whether a managed worktree is the current one.
type WorktreeCheckout string

const (
	WorktreeCheckoutElsewhere WorktreeCheckout = "elsewhere"
	WorktreeCheckoutCurrent   WorktreeCheckout = "current"
)

// WorktreeChanges describes the inspected state of the worktree directory.
type WorktreeChanges string

const (
	WorktreeChangesClean WorktreeChanges = "clean"
	WorktreeChangesDirty WorktreeChanges = "dirty"
)

// LifecycleCapability describes whether a lifecycle operation is structurally
// available. Uncommitted changes are deliberately not represented here: their
// handling is an explicit RemovalPolicy chosen by the caller.
type LifecycleCapability string

const (
	LifecycleAllowed     LifecycleCapability = "allowed"
	LifecycleNeedsRepair LifecycleCapability = "needs_repair"
	LifecycleCurrent     LifecycleCapability = "current"
	LifecycleHasBranches LifecycleCapability = "has_branches"
)

// WorktreeLifecycle is the inspected lifecycle state of a managed worktree.
// Keeping these dimensions typed makes a worktree's mutability explicit without
// requiring callers to infer it from a combination of booleans.
type WorktreeLifecycle struct {
	Registration RegistrationState   `json:"registration_state"`
	Presence     WorktreePresence    `json:"presence"`
	Checkout     WorktreeCheckout    `json:"checkout"`
	Changes      WorktreeChanges     `json:"changes"`
	Remove       LifecycleCapability `json:"remove"`
	Detach       LifecycleCapability `json:"detach"`
}

func (l WorktreeLifecycle) Exists() bool { return l.Presence == WorktreePresent }

func (l WorktreeLifecycle) NeedsRepair() bool { return l.Registration != RegistrationStateOK }

func (l WorktreeLifecycle) IsCurrent() bool { return l.Checkout == WorktreeCheckoutCurrent }

func (l WorktreeLifecycle) IsDirty() bool { return l.Changes == WorktreeChangesDirty }

func (l WorktreeLifecycle) CanRemove() bool { return l.Remove == LifecycleAllowed }

func (l WorktreeLifecycle) CanDetach() bool { return l.Detach == LifecycleAllowed }

// ListResult contains the results of listing worktrees
type ListResult struct {
	Worktrees     []Entry
	CurrentAnchor string // Anchor branch of the worktree we're currently in (if any)
	// OwnershipWarnings report branches whose physical checkout location does
	// not agree with Stackit's derived ownership model. They are warnings so
	// `worktree list` remains useful for diagnosing a damaged repository.
	OwnershipWarnings []string `json:"ownership_warnings,omitempty"`
}

// Entry represents a single managed worktree
type Entry struct {
	Name          engine.WorktreeName `json:"name"`          // User-provided name
	AnchorBranch  string              `json:"anchor_branch"` // Registered anchor branch name
	Path          WorktreePath        `json:"path"`
	StackSize     int                 `json:"stack_size"`               // Number of real branches in the stack
	CurrentBranch string              `json:"current_branch,omitempty"` // Branch currently checked out in this worktree
	RootBranches  []string            `json:"root_branches,omitempty"`  // Real stack roots visible to the user
	Lifecycle     WorktreeLifecycle   `json:"lifecycle"`
	StatusMessage string              `json:"status_message,omitempty"` // Human-readable summary of current state
	registration  engine.WorktreeInfo
}

// MarshalJSON preserves the established flat lifecycle fields while exposing
// the typed lifecycle object to newer clients. The compatibility projection can
// be removed only in a deliberately versioned output change.
func (e Entry) MarshalJSON() ([]byte, error) {
	type entryJSON struct {
		Name              engine.WorktreeName `json:"name"`
		AnchorBranch      string              `json:"anchor_branch"`
		Path              WorktreePath        `json:"path"`
		Exists            bool                `json:"exists"`
		StackSize         int                 `json:"stack_size"`
		CurrentBranch     string              `json:"current_branch,omitempty"`
		IsDirty           bool                `json:"is_dirty"`
		RootBranches      []string            `json:"root_branches,omitempty"`
		RegistrationState RegistrationState   `json:"registration_state"`
		Lifecycle         WorktreeLifecycle   `json:"lifecycle"`
		StatusMessage     string              `json:"status_message,omitempty"`
		CanRemove         bool                `json:"can_remove"`
		CanDetach         bool                `json:"can_detach"`
		NeedsRepair       bool                `json:"needs_repair"`
		IsCurrent         bool                `json:"is_current,omitempty"`
	}

	return json.Marshal(entryJSON{
		Name:              e.Name,
		AnchorBranch:      e.AnchorBranch,
		Path:              e.Path,
		Exists:            e.Lifecycle.Exists(),
		StackSize:         e.StackSize,
		CurrentBranch:     e.CurrentBranch,
		IsDirty:           e.Lifecycle.IsDirty(),
		RootBranches:      e.RootBranches,
		RegistrationState: e.Lifecycle.Registration,
		Lifecycle:         e.Lifecycle,
		StatusMessage:     e.StatusMessage,
		CanRemove:         e.Lifecycle.CanRemove(),
		CanDetach:         e.Lifecycle.CanDetach(),
		NeedsRepair:       e.Lifecycle.NeedsRepair(),
		IsCurrent:         e.Lifecycle.IsCurrent(),
	})
}

func (e Entry) displayName() string {
	if e.Name != "" {
		return e.Name.String()
	}
	return e.AnchorBranch
}

func (e Entry) primaryRootBranch() string {
	if len(e.RootBranches) == 0 {
		return ""
	}
	return e.RootBranches[0]
}

func (e Entry) removeRefusal(policy RemovalPolicy) string {
	switch e.Lifecycle.Remove {
	case LifecycleNeedsRepair:
		return fmt.Sprintf("managed worktree %s cannot be removed because %s; %s", output.BranchName(e.displayName()), output.Dim(e.StatusMessage), repairHint(&e))
	case LifecycleCurrent:
		return "cannot remove the current worktree; cd to the main repo first"
	case LifecycleHasBranches:
		return fmt.Sprintf("worktree %s has %d branch(es); use 'stackit worktree detach %s' to remove the worktree while keeping branches", output.BranchName(e.displayName()), e.StackSize, e.displayName())
	}
	if e.Lifecycle.Exists() && e.Lifecycle.IsDirty() && !policy.DiscardsChanges() {
		return "worktree has uncommitted changes; use --force to discard them"
	}
	return ""
}

func (e Entry) detachRefusal(policy RemovalPolicy) string {
	switch e.Lifecycle.Detach {
	case LifecycleNeedsRepair:
		return fmt.Sprintf("managed worktree %s cannot be detached because %s; %s", output.BranchName(e.displayName()), output.Dim(e.StatusMessage), repairHint(&e))
	case LifecycleCurrent:
		return "cannot detach from inside the worktree; cd to main repo first"
	}
	if e.Lifecycle.Exists() && e.Lifecycle.IsDirty() && !policy.DiscardsChanges() {
		return "worktree has uncommitted changes; use --force to override"
	}
	return ""
}

func inspectWorktreeEntry(ctx *app.Context, wt engine.WorktreeInfo, graph *engine.StackGraph, currentAnchor string, branchesByPath map[string]string) Entry {
	entry := Entry{
		Name:         wt.Name,
		AnchorBranch: wt.AnchorBranch,
		Path:         wt.Path,
		Lifecycle: WorktreeLifecycle{
			Registration: RegistrationStateInvalid,
			Presence:     WorktreePresent,
			Checkout:     WorktreeCheckoutElsewhere,
			Changes:      WorktreeChangesClean,
			Remove:       LifecycleNeedsRepair,
			Detach:       LifecycleNeedsRepair,
		},
		StatusMessage: "registration is invalid",
		registration:  wt,
	}
	if currentAnchor != "" && wt.AnchorBranch == currentAnchor {
		entry.Lifecycle.Checkout = WorktreeCheckoutCurrent
	}

	if _, statErr := os.Stat(wt.Path.String()); os.IsNotExist(statErr) {
		entry.Lifecycle.Presence = WorktreeMissing
	}

	if entry.Lifecycle.Exists() {
		canonicalPath, err := git.CanonicalWorktreePath(wt.Path.String())
		if err == nil {
			entry.CurrentBranch = branchesByPath[canonicalPath]
		}

		isDirty, err := ctx.Engine.WorktreeHasUncommittedChanges(ctx.Context, wt.Path.String())
		if err == nil {
			if isDirty {
				entry.Lifecycle.Changes = WorktreeChangesDirty
			}
		}
	}

	anchorBranch := ctx.Engine.GetBranch(wt.AnchorBranch)
	anchorExists := ctx.Engine.BranchNames().Contains(wt.AnchorBranch)

	switch {
	case anchorExists && ctx.Engine.IsWorktreeAnchor(anchorBranch):
		entry.Lifecycle.Registration = RegistrationStateOK
		entry.StatusMessage = "healthy"
		children := graph.Children(anchorBranch)
		entry.RootBranches = append(entry.RootBranches, children...)

		descendants := graph.Range(anchorBranch, engine.StackRange{
			RecursiveChildren: true,
			IncludeCurrent:    false,
		})
		entry.StackSize = len(descendants)
		entry.Lifecycle.Detach = LifecycleAllowed
		if len(children) == 0 {
			entry.Lifecycle.Remove = LifecycleAllowed
		} else {
			entry.Lifecycle.Remove = LifecycleHasBranches
		}
		if entry.Lifecycle.IsCurrent() {
			entry.Lifecycle.Remove = LifecycleCurrent
			entry.Lifecycle.Detach = LifecycleCurrent
		}

		switch {
		case !entry.Lifecycle.Exists():
			entry.StatusMessage = "worktree directory is missing"
		case entry.Lifecycle.IsDirty():
			entry.StatusMessage = "has uncommitted changes"
		case len(children) == 0:
			entry.StatusMessage = "empty anchor"
		}

	case anchorExists:
		entry.Lifecycle.Registration = RegistrationStateLegacy
		entry.StatusMessage = "legacy registration points at a real branch"
		entry.RootBranches = []string{wt.AnchorBranch}

		descendants := graph.Range(anchorBranch, engine.StackRange{
			RecursiveChildren: true,
			IncludeCurrent:    true,
		})
		entry.StackSize = len(descendants)

		if !entry.Lifecycle.Exists() {
			entry.StatusMessage = "legacy registration points at a real branch and the directory is missing"
		}

	default:
		entry.StatusMessage = "registered anchor branch is missing"
		if !entry.Lifecycle.IsCurrent() {
			entry.Lifecycle.Detach = LifecycleAllowed
		}

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

		switch {
		case !entry.Lifecycle.Exists():
			entry.StatusMessage = "registration is stale and the directory is missing"
			if !entry.Lifecycle.IsCurrent() {
				entry.Lifecycle.Remove = LifecycleAllowed
			}
		case entry.CurrentBranch == "":
			entry.StatusMessage = "registration is invalid and worktree is detached"
			if !entry.Lifecycle.IsCurrent() {
				entry.Lifecycle.Remove = LifecycleAllowed
			}
		case len(entry.RootBranches) == 0:
			entry.StatusMessage = "registration is invalid and worktree branch is untracked"
			if !entry.Lifecycle.IsCurrent() {
				entry.Lifecycle.Remove = LifecycleAllowed
			}
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
	gitWorktrees, gitErr := ctx.Engine.ListWorktrees(ctx.Context)
	branchesByPath := make(map[string]string, len(gitWorktrees))
	if gitErr == nil {
		for _, worktree := range gitWorktrees {
			canonicalPath, err := git.CanonicalWorktreePath(worktree.Path)
			if err == nil {
				branchesByPath[canonicalPath] = worktree.Branch
			}
		}
	}

	for _, wt := range worktrees {
		entry := inspectWorktreeEntry(ctx, wt, graph, result.CurrentAnchor, branchesByPath)
		if opts.Selector != "" && entry.AnchorBranch != opts.Selector.String() && entry.Name != engine.WorktreeName(opts.Selector) {
			continue
		}
		result.Worktrees = append(result.Worktrees, entry)
	}

	if gitErr != nil {
		result.OwnershipWarnings = []string{fmt.Sprintf("could not inspect Git worktrees for ownership conflicts: %v", gitErr)}
	} else {
		result.OwnershipWarnings = ownershipWarnings(ctx, gitWorktrees)
	}

	return result, nil
}

// OwnershipWarnings reports branches whose physical checkout location does not
// match the worktree that owns their stack. Exported so commands that mutate
// across the whole repository — sync in particular — can surface divergence at
// the time it matters, not only when someone happens to run `worktree list`.
func OwnershipWarnings(ctx *app.Context) []string {
	gitWorktrees, err := ctx.Engine.ListWorktrees(ctx.Context)
	if err != nil {
		return []string{fmt.Sprintf("could not inspect Git worktrees for ownership conflicts: %v", err)}
	}
	return ownershipWarnings(ctx, gitWorktrees)
}

func ownershipWarnings(ctx *app.Context, gitWorktrees git.WorktreeList) []string {
	// Ask git which checkout is the main one rather than trusting the engine's
	// repo root: an engine constructed inside a managed worktree reports that
	// worktree as its root, so the real main repository never matches and every
	// branch checked out there gets reported as an unmanaged worktree. A warning
	// channel that cries wolf on every sync is one nobody reads.
	mainRepoPath, err := git.CanonicalWorktreePath(gitWorktrees.MainPath())
	if err != nil {
		return []string{fmt.Sprintf("could not canonicalize the main repository path: %v", err)}
	}

	warnings := make([]string, 0)
	for _, gitWorktree := range gitWorktrees {
		if gitWorktree.Branch == "" {
			// Porcelain does not identify the branch for detached worktrees, so
			// it cannot establish stack ownership without a more expensive
			// commit-membership scan.
			continue
		}

		actualPath, pathErr := git.CanonicalWorktreePath(gitWorktree.Path)
		if pathErr != nil {
			warnings = append(warnings, fmt.Sprintf("could not canonicalize Git worktree path %s: %v", gitWorktree.Path, pathErr))
			continue
		}

		owner, ownerErr := ctx.Engine.OwningWorktree(ctx.Engine.GetBranch(gitWorktree.Branch))
		if ownerErr != nil {
			warnings = append(warnings, fmt.Sprintf("could not determine owner for branch %s checked out at %s: %v", gitWorktree.Branch, gitWorktree.Path, ownerErr))
			continue
		}
		if owner == nil {
			if actualPath != mainRepoPath {
				warnings = append(warnings, fmt.Sprintf("branch %s is checked out in unmanaged worktree %s", gitWorktree.Branch, gitWorktree.Path))
			}
			continue
		}

		expectedPath, expectedPathErr := git.CanonicalWorktreePath(owner.Path.String())
		if expectedPathErr != nil {
			warnings = append(warnings, fmt.Sprintf("could not canonicalize registered path %s for branch %s: %v", owner.Path, gitWorktree.Branch, expectedPathErr))
			continue
		}
		if actualPath != expectedPath {
			warnings = append(warnings, fmt.Sprintf("branch %s belongs to managed worktree %s (%s) but is checked out at %s", gitWorktree.Branch, owner.DisplayName(), owner.Path, gitWorktree.Path))
		}
	}

	return slices.Compact(slices.Sorted(slices.Values(warnings)))
}

// ListAction lists all managed worktrees
func ListAction(ctx *app.Context, opts ListOptions) (*ListResult, error) {
	return listEntries(ctx, opts)
}

// findWorktreeByNameOrBranch looks up a worktree by name or anchor branch
func findWorktreeByNameOrBranch(ctx *app.Context, selector WorktreeSelector) (*engine.WorktreeInfo, error) {
	nameOrBranch := selector.String()
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
		if wt.Name == engine.WorktreeName(nameOrBranch) {
			return &wt, nil
		}
	}

	return nil, fmt.Errorf("no worktree found for %s", output.BranchName(nameOrBranch))
}

func getWorktreeEntry(ctx *app.Context, selector WorktreeSelector) (*Entry, error) {
	result, err := listEntries(ctx, ListOptions{Selector: selector})
	if err != nil {
		return nil, err
	}
	if len(result.Worktrees) == 0 {
		return nil, fmt.Errorf("no worktree found for %s", output.BranchName(selector.String()))
	}
	entry := result.Worktrees[0]
	return &entry, nil
}

func repairHint(entry *Entry) string {
	return fmt.Sprintf("run 'stackit worktree repair %s' first", entry.displayName())
}
