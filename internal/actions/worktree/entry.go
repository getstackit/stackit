package worktree

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/output"
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
	// OwnershipWarnings report branches whose physical checkout location does
	// not agree with Stackit's derived ownership model. They are warnings so
	// `worktree list` remains useful for diagnosing a damaged repository.
	OwnershipWarnings []string `json:"ownership_warnings,omitempty"`
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
		currentBranch, err := ctx.Engine.GetWorktreeCurrentBranch(ctx.Context, wt.Path)
		if err == nil && currentBranch != "" {
			entry.CurrentBranch = currentBranch
		}

		isDirty, err := ctx.Engine.WorktreeHasUncommittedChanges(ctx.Context, wt.Path)
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

	result.OwnershipWarnings = ownershipWarnings(ctx)

	return result, nil
}

func ownershipWarnings(ctx *app.Context) []string {
	gitWorktrees, err := ctx.Engine.ListWorktrees(ctx.Context)
	if err != nil {
		return []string{fmt.Sprintf("could not inspect Git worktrees for ownership conflicts: %v", err)}
	}

	mainRepoPath, err := canonicalPath(ctx.Engine.GetRepoRoot())
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

		actualPath, pathErr := canonicalPath(gitWorktree.Path)
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

		expectedPath, expectedPathErr := canonicalPath(owner.Path)
		if expectedPathErr != nil {
			warnings = append(warnings, fmt.Sprintf("could not canonicalize registered path %s for branch %s: %v", owner.Path, gitWorktree.Branch, expectedPathErr))
			continue
		}
		if actualPath != expectedPath {
			warnings = append(warnings, fmt.Sprintf("branch %s belongs to managed worktree %s (%s) but is checked out at %s", gitWorktree.Branch, worktreeDisplayName(owner), owner.Path, gitWorktree.Path))
		}
	}

	return slices.Compact(slices.Sorted(slices.Values(warnings)))
}

func canonicalPath(path string) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolvedPath, err := filepath.EvalSymlinks(absPath)
	if err == nil {
		return filepath.Clean(resolvedPath), nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	return filepath.Clean(absPath), nil
}

func worktreeDisplayName(worktree *engine.WorktreeInfo) string {
	if worktree.Name != "" {
		return worktree.Name
	}
	return worktree.AnchorBranch
}

// ListAction lists all managed worktrees
func ListAction(ctx *app.Context, _ ListOptions) (*ListResult, error) {
	return listEntries(ctx, ListOptions{})
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

	return nil, fmt.Errorf("no worktree found for %s", output.BranchName(nameOrBranch))
}

func getWorktreeEntry(ctx *app.Context, nameOrBranch string) (*Entry, error) {
	result, err := listEntries(ctx, ListOptions{NameOrBranch: nameOrBranch})
	if err != nil {
		return nil, err
	}
	if len(result.Worktrees) == 0 {
		return nil, fmt.Errorf("no worktree found for %s", output.BranchName(nameOrBranch))
	}
	entry := result.Worktrees[0]
	return &entry, nil
}

func repairHint(entry *Entry) string {
	return fmt.Sprintf("run 'stackit worktree repair %s' first", entry.displayName())
}
