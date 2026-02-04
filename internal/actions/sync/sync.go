// Package sync provides functionality for synchronizing stacked branches with remote repositories.
package sync

import (
	"fmt"
	"strings"
	"time"

	"stackit.dev/stackit/internal/actions"
	"stackit.dev/stackit/internal/actions/handler"
	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/handlers"

	"golang.org/x/sync/errgroup"
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

	ctx.Logger.Info("sync started", "restack", opts.Restack, "force", opts.Force)

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

	// Check for uncommitted changes in main repo (not worktrees - those are handled below)
	// If we're in a worktree with uncommitted changes, the worktree dirty check will skip that stack
	uncommittedStart := time.Now()
	hasUncommitted := ctx.Reader().HasUncommittedChanges(gctx)
	ctx.Logger.Info("check uncommitted changes completed", "durationMs", time.Since(uncommittedStart).Milliseconds())
	if hasUncommitted && !ctx.InManagedWorktree {
		return fmt.Errorf("you have uncommitted changes. Please commit or stash them before syncing")
	}

	// Collect dirty worktree anchors (stacks to skip entirely)
	// Rather than failing on dirty worktrees, we skip their entire stack to allow
	// parallel work in other worktrees while preserving consistency.
	var dirtyAnchors map[string]bool
	managedWorktrees, err := eng.ListManagedWorktrees()
	if err == nil {
		for _, wt := range managedWorktrees {
			if hasChanges, _ := eng.Git().WorktreeHasUncommittedChanges(gctx, wt.Path); hasChanges {
				if dirtyAnchors == nil {
					dirtyAnchors = make(map[string]bool)
				}
				dirtyAnchors[wt.AnchorBranch] = true
				out.Warn("Skipping stack rooted at %s (worktree has uncommitted changes)", wt.AnchorBranch)
			}
		}
	}

	// Calculate total operations for progress (rough estimate)
	totalOps := 1 // trunk sync
	if opts.Restack {
		// Estimate based on tracked branches
		progressCountStart := time.Now()
		branchCount := len(ctx.Navigator().AllBranches())
		ctx.Logger.Info("count branches for progress completed", "durationMs", time.Since(progressCountStart).Milliseconds(), "branchCount", branchCount)
		totalOps += branchCount
	}
	handler.Start(totalOps)

	// Phase 1: Parallel network operations
	// Run trunk pull, metadata fetch, and GitHub PR info sync concurrently
	handler.EmitEvent(Event{Phase: PhaseTrunk, Type: EventStarted})
	handler.EmitEvent(Event{Phase: PhaseGitHub, Type: EventStarted})

	var trunkSummary Summary
	var githubSyncResult *GitHubSyncResult
	var metadataFetchErr error
	var trunkErr error
	var githubErr error

	parallelStart := time.Now()
	ctx.Logger.Info("starting parallel phase")

	g, _ := errgroup.WithContext(gctx)

	// Goroutine 1: Pull trunk
	g.Go(func() error {
		ctx.Logger.Info("goroutine trunk started", "delayMs", time.Since(parallelStart).Milliseconds())
		trunkErr = syncTrunk(ctx, &opts, handler, &trunkSummary)
		return nil // Don't fail the group, handle error after Wait
	})

	// Goroutine 2: Fetch remote metadata refs (network operation only)
	g.Go(func() error {
		ctx.Logger.Info("goroutine metadata started", "delayMs", time.Since(parallelStart).Milliseconds())
		fetchStart := time.Now()
		metadataFetchErr = ctx.RemoteMetadata().FetchRemoteMetadata(gctx)
		ctx.Logger.Info("fetch remote metadata refs completed", "durationMs", time.Since(fetchStart).Milliseconds())

		// Also fetch stack metadata refs (stack-level descriptions, etc.)
		stackFetchStart := time.Now()
		if err := ctx.Git().FetchStackMetaRefs(gctx); err != nil {
			ctx.Logger.Debug("fetch stack metadata refs failed (non-fatal)", "error", err)
		}
		ctx.Logger.Info("fetch stack metadata refs completed", "durationMs", time.Since(stackFetchStart).Milliseconds())
		return nil
	})

	// Goroutine 3: Sync PR info from GitHub (network operation only)
	g.Go(func() error {
		ctx.Logger.Info("goroutine github started", "delayMs", time.Since(parallelStart).Milliseconds())
		var err error
		githubSyncResult, err = syncGitHubPRInfo(ctx)
		githubErr = err
		return nil
	})

	_ = g.Wait()
	ctx.Logger.Info("parallel phase completed", "durationMs", time.Since(parallelStart).Milliseconds())

	// Handle errors from parallel operations
	if trunkErr != nil {
		return trunkErr
	}

	// Merge trunk summary
	summary.TrunkUpdated = trunkSummary.TrunkUpdated
	summary.TrunkRevision = trunkSummary.TrunkRevision

	// GitHub failure aborts sync (per spec)
	if githubErr != nil {
		return githubErr
	}

	// Populate skipped stacks in summary
	for anchor := range dirtyAnchors {
		summary.SkippedStacks = append(summary.SkippedStacks, anchor)
	}

	// Process GitHub PR info results (sequential - depends on PR info)
	if githubSyncResult != nil {
		if err := processGitHubSyncResult(ctx, githubSyncResult, dirtyAnchors, handler); err != nil {
			return err
		}
	}

	branchesToRestack := []string{}

	// Process remote metadata (sequential - depends on fetch)
	if metadataFetchErr != nil {
		out.Debug("No remote metadata to fetch: %v", metadataFetchErr)
	}
	if err := processRemoteMetadata(ctx, &opts, handler); err != nil {
		return err
	}

	// Phase 2.5: Sync stack branches from remote
	if err := syncStackBranches(ctx, dirtyAnchors, handler, summary); err != nil {
		return err
	}

	// Phase 3: Clean branches (delete merged/closed)
	cleanResult, err := cleanBranches(ctx, &opts, dirtyAnchors, handler, summary)
	if err != nil {
		return fmt.Errorf("failed to clean branches: %w", err)
	}

	// Clean orphaned worktrees (after branch cleanup so we know what's been deleted)
	worktreeResult := cleanOrphanedWorktrees(ctx, dirtyAnchors)
	if len(worktreeResult.RemovedWorktrees) > 0 {
		summary.WorktreesCleaned = len(worktreeResult.RemovedWorktrees)
	}
	// Surface any worktree cleanup errors as warnings (non-fatal)
	for _, errMsg := range worktreeResult.Errors {
		ctx.Output.Warn("Worktree cleanup: %s", errMsg)
	}

	// GC orphaned stack metadata refs (after branch cleanup so we know what's been deleted)
	gcResult := gcOrphanedStackMetadata(ctx)
	if len(gcResult.DeletedStackIDs) > 0 {
		ctx.Logger.Info("cleaned orphaned stack metadata", "count", len(gcResult.DeletedStackIDs))
	}
	// Surface any GC errors as warnings (non-fatal)
	for _, errMsg := range gcResult.Errors {
		ctx.Output.Warn("Stack metadata cleanup: %s", errMsg)
	}

	graph := engine.BuildStackGraph(eng, engine.SortStrategyAlphabetical, nil)

	// Add branches with new parents to restack list (skip dirty stacks)
	for _, branchName := range cleanResult.BranchesWithNewParents {
		if isInDirtyStack(ctx, branchName, dirtyAnchors) {
			continue
		}
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

	if err := restackBranches(ctx, branchesToRestack, dirtyAnchors, handler, summary); err != nil {
		// Even on error, complete with summary
		handler.Complete(*summary)
		return err
	}

	// Check for conflicts and prompt user if interactive
	if len(summary.ConflictBranches) > 0 && handler.IsInteractive() {
		resolve, err := handler.PromptResolveConflicts(summary.ConflictBranches)
		if err != nil {
			handler.Complete(*summary)
			return fmt.Errorf("failed to prompt for conflict resolution: %w", err)
		}

		if resolve {
			// User wants to resolve conflicts - enter conflict workflow for the first conflict
			firstConflict := summary.ConflictBranches[0]
			allBranches := ctx.Navigator().AllBranches()
			return actions.EnterConflictWorkflow(ctx, firstConflict, allBranches)
		}
		// User chose to skip conflicts - continue with summary
	}

	// Check if everything was up to date
	if !summary.HasChanges() {
		summary.UpToDate = true
	}

	ctx.Logger.Info("sync completed",
		"trunkUpdated", summary.TrunkUpdated,
		"branchesRestacked", summary.BranchesRestacked,
		"branchesDeleted", summary.BranchesDeleted,
		"branchesSkipped", summary.BranchesSkipped,
	)

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
	PhaseTrunk    Phase = "trunk"
	PhaseBranches Phase = "branches"
	PhaseGitHub   Phase = "github"
	PhaseClean    Phase = "clean"
	PhaseRestack  Phase = "restack"
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
	SkippedStacks     []string // Stacks skipped due to dirty worktrees
}

// HasChanges returns true if any operations were performed
func (s *Summary) HasChanges() bool {
	return s.TrunkUpdated || s.BranchesSynced > 0 || s.BranchesRestacked > 0 ||
		s.BranchesDeleted > 0 || s.BranchesSkipped > 0 || s.WorktreesCleaned > 0 ||
		len(s.SkippedStacks) > 0
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

	// IsInteractive returns true if this handler supports interactive prompts.
	// Non-interactive handlers return false and their prompt methods return defaults.
	IsInteractive() bool

	// PromptMetadataConflict displays a metadata conflict and asks user to accept remote.
	// Returns true to accept remote metadata, false to keep local.
	// In non-interactive mode, returns (false, nil) to preserve local changes.
	PromptMetadataConflict(diff *engine.MetadataDiff) (acceptRemote bool, err error)

	// PromptOrphanedMetadata asks what to do when remote metadata was deleted but local has changes.
	// Returns true to push local metadata to remote, false to accept deletion.
	// In non-interactive mode, returns (false, nil) to accept the remote deletion.
	PromptOrphanedMetadata(info engine.OrphanedMetadataInfo) (pushLocal bool, err error)

	// PromptBranchDeletions displays planned branch deletions and asks user to confirm each one.
	// Returns a map of branch names that the user confirmed for deletion.
	// In non-interactive mode, returns all branches (auto-confirm).
	PromptBranchDeletions(branches map[string]string) (confirmed map[string]bool, err error)

	// PromptResolveConflicts asks user if they want to resolve restack conflicts now or skip them.
	// Returns true to start conflict resolution workflow, false to skip and continue.
	// In non-interactive mode, returns (false, nil) to skip conflicts.
	PromptResolveConflicts(conflictBranches []string) (resolve bool, err error)

	// RestackHandler methods are available for restack-specific output
	// This allows the same handler to be used for standalone restack operations
	RestackHandler
}

// NullHandler is a no-op handler for testing or when output is not needed
// NullHandler is a no-op handler for when nil is passed.
// It embeds handler.NullBase for Cleanup() and IsInteractive().
type NullHandler struct {
	handler.NullBase
}

// Start implements Handler.
func (h *NullHandler) Start(int) {}

// EmitEvent implements Handler.
func (h *NullHandler) EmitEvent(Event) {}

// Complete implements Handler.
func (h *NullHandler) Complete(Summary) {}

// OnRestackStart implements RestackHandler.
func (h *NullHandler) OnRestackStart(int) {}

// OnRestackBranch implements RestackHandler.
func (h *NullHandler) OnRestackBranch(string, RestackResult, string, *int, engine.LockReason, bool, bool, string, bool, string, string) {
}

// OnRestackComplete implements RestackHandler.
func (h *NullHandler) OnRestackComplete(int, int, []string) {}

// PromptMetadataConflict implements Handler. Returns false (keep local) in non-interactive mode.
func (h *NullHandler) PromptMetadataConflict(_ *engine.MetadataDiff) (bool, error) {
	return false, nil
}

// PromptOrphanedMetadata implements Handler. Returns false (accept deletion) in non-interactive mode.
func (h *NullHandler) PromptOrphanedMetadata(_ engine.OrphanedMetadataInfo) (bool, error) {
	return false, nil
}

// PromptResolveConflicts implements Handler. Returns false (skip) in non-interactive mode.
func (h *NullHandler) PromptResolveConflicts(_ []string) (bool, error) {
	return false, nil
}

// PromptBranchDeletions implements Handler. Returns all branches (auto-confirm) in non-interactive mode.
func (h *NullHandler) PromptBranchDeletions(branches map[string]string) (map[string]bool, error) {
	confirmed := make(map[string]bool)
	for name := range branches {
		confirmed[name] = true
	}
	return confirmed, nil
}

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
	if len(summary.SkippedStacks) > 0 {
		parts = append(parts, fmt.Sprintf("skipped %d stack%s (dirty worktree)", len(summary.SkippedStacks), plural(len(summary.SkippedStacks))))
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

// isInDirtyStack returns true if the branch belongs to a dirty worktree's stack.
// A branch is in a dirty stack if its stack root (first ancestor whose parent is trunk)
// matches the anchor branch of a dirty worktree.
func isInDirtyStack(ctx *app.Context, branchName string, dirtyAnchors map[string]bool) bool {
	if len(dirtyAnchors) == 0 {
		return false
	}
	branch := ctx.Engine.GetBranch(branchName)
	stackRoot := ctx.Engine.GetStackRootForBranch(branch)
	return dirtyAnchors[stackRoot]
}
