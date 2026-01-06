// Package sync provides functionality for synchronizing stacked branches with remote repositories.
package sync

import (
	"fmt"
	"strings"

	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/handlers"
)

// Options contains options for the sync command
type Options struct {
	All     bool
	Force   bool
	Restack bool
	DryRun  bool
}

// Action performs the sync operation
func Action(ctx *app.Context, opts Options, handler Handler) error {
	eng := ctx.Engine
	out := ctx.Output
	gctx := ctx.Context
	summary := &Summary{}

	// Use null handler if none provided
	if handler == nil {
		handler = &NullHandler{}
	}

	// Ensure terminal cleanup happens even on error
	defer handler.Cleanup()

	// Handle --all flag (stub for now)
	if opts.All {
		// For now, just sync the current trunk
		// In the future, this would sync across all configured trunks
		out.Info("Syncing branches across all configured trunks...")
	}

	// Check for uncommitted changes
	if ctx.Reader().HasUncommittedChanges(gctx) {
		return fmt.Errorf("you have uncommitted changes. Please commit or stash them before syncing")
	}

	// Calculate total operations for progress (rough estimate)
	totalOps := 1 // trunk sync
	if opts.Restack {
		// Estimate based on tracked branches
		totalOps += len(ctx.Navigator().AllBranches())
	}
	handler.Start(totalOps)

	// Phase 1: Pull trunk
	handler.EmitEvent(Event{Phase: PhaseTrunk, Type: EventStarted})
	if err := syncTrunk(ctx, &opts, handler, summary); err != nil {
		return err
	}

	// Clean branches (delete merged/closed)
	branchesToRestack := []string{}

	// Phase 2: Sync PR info from GitHub
	handler.EmitEvent(Event{Phase: PhaseGitHub, Type: EventStarted})
	if err := syncGitHubInfo(ctx, &branchesToRestack, handler, summary); err != nil {
		return err
	}

	// Sync remote metadata (internal, not a visible phase unless conflicts)
	if err := syncRemoteMetadata(ctx, &opts); err != nil {
		return err
	}

	// Phase 3: Clean branches (delete merged/closed)
	cleanResult, err := cleanBranches(ctx, &opts, handler, summary)
	if err != nil {
		return fmt.Errorf("failed to clean branches: %w", err)
	}

	// Clean orphaned worktrees (after branch cleanup so we know what's been deleted)
	worktreeResult := cleanOrphanedWorktrees(ctx)
	if len(worktreeResult.RemovedWorktrees) > 0 {
		summary.WorktreesCleaned = len(worktreeResult.RemovedWorktrees)
	}
	// Surface any worktree cleanup errors as warnings (non-fatal)
	for _, errMsg := range worktreeResult.Errors {
		ctx.Output.Warn("Worktree cleanup: %s", errMsg)
	}

	graph := engine.BuildStackGraph(eng, engine.SortStrategyAlphabetical, nil)

	// Add branches with new parents to restack list
	for _, branchName := range cleanResult.BranchesWithNewParents {
		branch := eng.GetBranch(branchName)
		upstack := graph.Range(branch, engine.StackRange{
			RecursiveChildren: true,
		})
		for _, b := range upstack {
			branchesToRestack = append(branchesToRestack, b.GetName())
		}
		branchesToRestack = append(branchesToRestack, branchName)
	}

	// Restack if requested
	if !opts.Restack {
		out.Tip("Try the --restack flag to automatically restack the current stack.")
		// Check if everything was up to date
		if !summary.HasChanges() {
			summary.UpToDate = true
		}
		handler.Complete(*summary)
		return nil
	}

	// Phase 4: Restack branches
	handler.EmitEvent(Event{Phase: PhaseRestack, Type: EventStarted})
	if err := restackBranches(ctx, branchesToRestack, handler, summary); err != nil {
		// Even on error, complete with summary
		handler.Complete(*summary)
		return err
	}

	// Check if everything was up to date
	if !summary.HasChanges() {
		summary.UpToDate = true
	}

	handler.Complete(*summary)
	return nil
}

// Re-export RestackResult constants from handlers package for convenience
const (
	RestackDone     = handlers.RestackDone
	RestackUnneeded = handlers.RestackUnneeded
	RestackConflict = handlers.RestackConflict
)

// RestackResult is an alias for handlers.RestackResult
type RestackResult = handlers.RestackResult

// RestackHandler is an alias for handlers.RestackHandler
type RestackHandler = handlers.RestackHandler

// Phase represents the current phase of the sync operation
type Phase string

// Phases of the sync operation
const (
	PhaseTrunk   Phase = "trunk"
	PhaseGitHub  Phase = "github"
	PhaseClean   Phase = "clean"
	PhaseRestack Phase = "restack"
)

// EventType represents the type of sync event
type EventType string

// Event types for sync operations
const (
	EventStarted   EventType = "started"
	EventProgress  EventType = "progress"
	EventCompleted EventType = "completed"
	EventSkipped   EventType = "skipped"
)

// Event represents a progress update during sync
type Event struct {
	Phase       Phase             // Current phase
	Type        EventType         // Event type
	Branch      string            // Branch name (if applicable)
	PRNumber    *int              // PR number (if applicable)
	Message     string            // Human-readable description
	OldRevision string            // For position changes
	NewRevision string            // For position changes
	Conflict    bool              // Is this a conflict?
	LockReason  engine.LockReason // Why the branch is locked (empty if not locked)
	Frozen      bool              // Is the branch frozen?
	IsCurrent   bool              // Is this the current branch?
	Parent      string            // Parent branch name (if applicable)
	Error       error             // If non-nil, this step had an error
}

// IsLocked returns true if the event associated branch is locked
func (e Event) IsLocked() bool {
	return e.LockReason.IsLocked()
}

// Summary holds aggregate results from a sync operation
type Summary struct {
	TrunkUpdated      bool     // Was trunk updated?
	TrunkRevision     string   // New trunk revision (short hash)
	BranchesSynced    int      // Number of branches synced from remote
	BranchesRestacked int      // Number of branches restacked
	BranchesDeleted   int      // Number of branches deleted
	BranchesSkipped   int      // Number of branches skipped (due to conflicts)
	ConflictBranches  []string // Names of branches that conflicted
	UpToDate          bool     // Everything was already current
	WorktreesCleaned  int      // Number of orphaned worktrees cleaned up
}

// ParentsResult contains the result of synchronizing parents from GitHub
type ParentsResult struct {
	BranchesReparented []string
}

// HasChanges returns true if any operations were performed
func (s *Summary) HasChanges() bool {
	return s.TrunkUpdated || s.BranchesSynced > 0 || s.BranchesRestacked > 0 ||
		s.BranchesDeleted > 0 || s.BranchesSkipped > 0 || s.WorktreesCleaned > 0
}

// Handler abstracts TTY vs non-TTY output for sync operations
// It embeds RestackHandler to provide a unified interface for operations that include restacking
type Handler interface {
	// Start is called at the beginning of sync with the total operation count
	Start(totalOps int)

	// EmitEvent is called for each progress update
	EmitEvent(event Event)

	// Complete is called when sync finishes with the summary
	Complete(summary Summary)

	// Cleanup ensures terminal is restored on error (may be no-op for non-TTY handlers)
	Cleanup()

	// RestackHandler methods are available for restack-specific output
	// This allows the same handler to be used for standalone restack operations
	RestackHandler
}

// NullHandler is a no-op handler for testing or when output is not needed
type NullHandler struct{}

// Start implements Handler.
func (h *NullHandler) Start(_ int) {}

// EmitEvent implements Handler.
func (h *NullHandler) EmitEvent(_ Event) {}

// Complete implements Handler.
func (h *NullHandler) Complete(_ Summary) {}

// OnRestackStart implements RestackHandler.
func (h *NullHandler) OnRestackStart(_ int) {}

// OnRestackBranch implements RestackHandler.
func (h *NullHandler) OnRestackBranch(_ string, _ RestackResult, _ string, _ *int, _ engine.LockReason, _ bool, _ bool, _ string, _ bool, _, _ string) {
}

// OnRestackComplete implements RestackHandler.
func (h *NullHandler) OnRestackComplete(_, _ int, _ []string) {}

// Cleanup implements Handler.
func (h *NullHandler) Cleanup() {}

// FormatSummaryParts returns the summary parts as a slice of strings
// This is shared between SimpleSyncHandler and InteractiveSyncHandler
func FormatSummaryParts(summary Summary) []string {
	parts := []string{}

	if summary.TrunkUpdated {
		parts = append(parts, "pulled trunk")
	}
	if summary.BranchesSynced > 0 {
		parts = append(parts, fmt.Sprintf("synced %d branch%s", summary.BranchesSynced, pluralES(summary.BranchesSynced)))
	}
	if summary.BranchesRestacked > 0 {
		parts = append(parts, fmt.Sprintf("restacked %d", summary.BranchesRestacked))
	}
	if summary.BranchesDeleted > 0 {
		parts = append(parts, fmt.Sprintf("deleted %d", summary.BranchesDeleted))
	}
	if summary.WorktreesCleaned > 0 {
		parts = append(parts, fmt.Sprintf("cleaned %d worktree%s", summary.WorktreesCleaned, plural(summary.WorktreesCleaned)))
	}
	if summary.BranchesSkipped > 0 {
		parts = append(parts, fmt.Sprintf("skipped %d (conflict)", summary.BranchesSkipped))
	}

	return parts
}

// plural returns "s" if count != 1, otherwise empty string
func plural(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

// FormatSummaryString returns the full summary as a string
func FormatSummaryString(summary Summary) string {
	if summary.UpToDate {
		return "Everything is up to date!"
	}

	parts := FormatSummaryParts(summary)
	if len(parts) == 0 {
		return ""
	}

	result := "Summary: " + strings.Join(parts, ", ")

	// Add actionable advice for conflicts
	if len(summary.ConflictBranches) > 0 {
		result += fmt.Sprintf("\n   Run 'st restack %s' to resolve and continue", summary.ConflictBranches[0])
	}

	return result
}

// pluralES returns "es" if count != 1, otherwise empty string (for "branch" -> "branches")
func pluralES(count int) string {
	if count == 1 {
		return ""
	}
	return "es"
}
