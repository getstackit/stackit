// Package sync provides functionality for synchronizing stacked branches with remote repositories.
package sync

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/actions/handler"
	"github.com/getstackit/stackit/internal/actions/worktree"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"

	"golang.org/x/sync/errgroup"
)

// DryRunResult represents the JSON output for sync --dry-run
type DryRunResult struct {
	WouldPull          string   `json:"would_pull,omitempty"`           // Trunk branch that would be pulled
	WouldClean         []string `json:"would_clean,omitempty"`          // Branches that would be deleted
	WouldRestack       []string `json:"would_restack,omitempty"`        // Branches that would be restacked
	WouldRestackStacks []string `json:"would_restack_stacks,omitempty"` // Deduped independent stack roots covering the current dry-run's would_restack set; recompute after sync before passing to `restack --stacks <roots>` because cleanup/reparenting can change roots
	SkippedStacks      []string `json:"skipped_stacks,omitempty"`       // Stacks skipped due to dirty worktrees
	TrunkStateUnknown  bool     `json:"trunk_state_unknown,omitempty"`  // Trunk's relationship to its remote could not be resolved (objects not fetched); an absent would_pull does NOT mean trunk is current
}

// Options contains options for the sync command
type Options struct {
	All          bool
	Force        bool
	Restack      bool // Explicitly restack the full current stack (opt-in)
	NoRestack    bool // Skip all restacking entirely
	DryRun       bool
	RestackScope []string // When non-nil, only restack these branches (skip current-branch expansion)
}

// Action performs the sync operation
func Action(ctx context.Context, deps Deps, opts Options, handler Handler) error {
	eng := deps.Engine
	out := deps.Out
	summary := &Summary{}

	deps.Log.Info("sync started restack=%v noRestack=%v force=%v", opts.Restack, opts.NoRestack, opts.Force)

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

	// Collect dirty worktree anchors (stacks to skip entirely)
	// Rather than failing on dirty worktrees, we skip their entire stack to allow
	// parallel work in other worktrees while preserving consistency. restack
	// holds the same stacks for the same reasons: both read ManagedWorktreeHolds.
	stackHolds, err := actions.ManagedWorktreeHolds(ctx, eng)
	if err != nil {
		deps.Log.Warn("could not inspect managed worktrees: %v", err)
	}
	var dirtyAnchors dirtyAnchorSet
	for _, hold := range stackHolds {
		if dirtyAnchors == nil {
			dirtyAnchors = make(dirtyAnchorSet)
		}
		dirtyAnchors[hold.StackRoot] = true
		out.Warn("%s", hold.Warning())
	}

	// Report branches checked out somewhere other than the worktree owning
	// their stack. Sync is the command licensed to touch every stack, so it is
	// the one place divergence reliably gets seen — previously it surfaced only
	// if someone happened to run `worktree list`, while the guards that refuse
	// mutations because of it said nothing about where to look.
	for _, warning := range worktree.OwnershipWarnings(ctx, eng) {
		out.Warn("%s", warning)
	}

	handler.Start()

	// Phase 1: Parallel network operations
	// Fetch trunk and metadata refs, and sync GitHub PR info concurrently.
	handler.EmitEvent(Event{Phase: PhaseTrunk, Type: EventStarted})
	handler.EmitEvent(Event{Phase: PhaseGitHub, Type: EventStarted})

	var trunkSummary Summary
	var githubSyncResult *GitHubSyncResult
	var trunkFetchErr error
	var trunkErr error
	var githubErr error
	var remoteStatuses engine.BranchRemoteStatuses

	// Snapshot the branch set for the remote-status probe below. The set is
	// stable through the parallel phase (no branches are created or deleted
	// until later), so reading it once here is safe.
	branchesForStatus := eng.AllBranches()

	remoteCtx, cancelRemote := git.WithRemoteOperationTimeout(ctx)
	defer cancelRemote()

	parallelStart := time.Now()
	deps.Log.Info("starting parallel phase")

	g, _ := errgroup.WithContext(remoteCtx)

	// Goroutine 1: Fetch trunk and Stackit metadata refs in one round trip.
	g.Go(func() error {
		deps.Log.Info("goroutine remote fetch started delayMs=%v", time.Since(parallelStart).Milliseconds())
		fetchStart := time.Now()
		trunkFetchErr = eng.FetchRemote(remoteCtx, engine.RemoteFetchRequest{
			Remote:               eng.GetRemote(ctx),
			Branches:             []string{eng.Trunk().Name()},
			IncludeMetadata:      true,
			IncludeStackMetadata: true,
		})
		deps.Log.Info("fetch trunk and metadata completed durationMs=%v", time.Since(fetchStart).Milliseconds())
		if trunkFetchErr != nil {
			deps.Log.Debug("fetch trunk and metadata failed error=%v", trunkFetchErr)
			return trunkFetchErr
		}
		return nil
	})

	// Goroutine 2: Sync PR info from GitHub (network operation only)
	g.Go(func() error {
		deps.Log.Info("goroutine github started delayMs=%v", time.Since(parallelStart).Milliseconds())
		var err error
		githubSyncResult, err = syncGitHubPRInfo(remoteCtx, deps)
		githubErr = err
		return nil
	})

	// Goroutine 3: Probe remote branch statuses (a single ls-remote) so this
	// network round trip overlaps the trunk fetch and GitHub call instead of
	// running serially later in syncStackBranches.
	g.Go(func() error {
		deps.Log.Info("goroutine remote statuses started delayMs=%v", time.Since(parallelStart).Milliseconds())
		statusStart := time.Now()
		remoteStatuses = eng.ReadBranchRemoteStatuses(remoteCtx, branchesForStatus)
		deps.Log.Info("prefetch remote branch statuses completed durationMs=%v branchCount=%v", time.Since(statusStart).Milliseconds(), len(remoteStatuses))
		return nil
	})

	parallelErr := g.Wait()
	deps.Log.Info("parallel phase completed durationMs=%v", time.Since(parallelStart).Milliseconds())

	if trunkFetchErr != nil {
		return fmt.Errorf("failed to fetch trunk from %s: %w", eng.GetRemote(ctx), trunkFetchErr)
	}
	if parallelErr != nil {
		return parallelErr
	}

	// Everything above this line is read-only — a fetch into remote-tracking
	// refs, a GitHub query, an ls-remote — and syncFetchedTrunk below is the
	// first thing to move a ref the user can see. Sync goes on to delete merged
	// branches and reparent their children before it reaches the restack phase
	// that can halt on a conflict, so a snapshot taken any later would let
	// `abort` claim it restored the state before sync while leaving every one
	// of those deletions in place.
	deps.Snapshots.Take(ctx, actions.NewSnapshot("sync",
		actions.WithFlag(opts.Restack, "--restack"),
		actions.WithFlag(opts.All, "--all"),
	))

	trunkErr = syncFetchedTrunk(ctx, deps, &opts, handler, &trunkSummary)
	if trunkErr != nil {
		return trunkErr
	}

	// Merge trunk summary
	summary.TrunkUpdated = trunkSummary.TrunkUpdated
	summary.TrunkRevision = trunkSummary.TrunkRevision
	summary.HeldBranches = append(summary.HeldBranches, trunkSummary.HeldBranches...)

	// GitHub failure aborts sync (per spec)
	if githubErr != nil {
		return githubErr
	}

	// Populate skipped stacks in summary
	summary.SkippedStacks = slices.AppendSeq(summary.SkippedStacks, maps.Keys(dirtyAnchors))

	// Process GitHub PR info results (sequential - depends on PR info)
	if githubSyncResult != nil {
		if err := processGitHubSyncResult(ctx, deps, githubSyncResult, dirtyAnchors, handler); err != nil {
			return err
		}
	}

	branchesToRestack := []string{}

	// Process remote metadata (sequential - depends on fetch)
	if err := processRemoteMetadata(ctx, deps, &opts, handler); err != nil {
		return err
	}

	// Phase 2.5: Sync stack branches from remote
	if err := syncStackBranches(ctx, deps, dirtyAnchors, remoteStatuses, handler, summary); err != nil {
		return err
	}

	// Phase 3: Clean branches (delete merged/closed)
	cleanResult, err := cleanBranches(ctx, deps, &opts, dirtyAnchors, remoteStatuses, handler, summary)
	if err != nil {
		return fmt.Errorf("failed to clean branches: %w", err)
	}

	// Clean orphaned worktrees (after branch cleanup so we know what's been deleted)
	worktreeResult := cleanOrphanedWorktrees(ctx, deps, dirtyAnchors)
	if len(worktreeResult.RemovedWorktrees) > 0 {
		summary.WorktreesCleaned = len(worktreeResult.RemovedWorktrees)
	}
	// Surface any worktree cleanup errors as warnings (non-fatal)
	for _, errMsg := range worktreeResult.Errors {
		out.Warn("Worktree cleanup: %s", errMsg)
	}

	// GC orphaned stack metadata refs (after branch cleanup so we know what's been deleted)
	gcResult := gcOrphanedStackMetadata(ctx, deps)
	if len(gcResult.DeletedStackIDs) > 0 {
		deps.Log.Info("cleaned orphaned stack metadata count=%v", len(gcResult.DeletedStackIDs))
	}
	// Surface any GC errors as warnings (non-fatal)
	for _, errMsg := range gcResult.Errors {
		out.Warn("Stack metadata cleanup: %s", errMsg)
	}

	graph := eng.Graph(ctx, engine.SortStrategyAlphabetical)

	// Add branches with new parents to restack list (skip dirty stacks)
	for _, branchName := range cleanResult.BranchesWithNewParents {
		if dirtyAnchors.includes(eng, branchName) {
			continue
		}
		branch := eng.GetBranch(branchName)
		upstack := graph.Range(branch, engine.StackRange{
			RecursiveChildren: true,
		})
		for _, b := range upstack {
			branchesToRestack = append(branchesToRestack, b.Name())
		}
		branchesToRestack = append(branchesToRestack, branchName)
	}

	// Phase 4: Restack branches
	// --no-restack: skip all restacking
	// --restack: expand to full current stack (old default behavior)
	// default (neither flag): only restack reparented branches
	if opts.NoRestack {
		// Check if everything was up to date
		if !summary.HasChanges() {
			summary.UpToDate = true
		}
		handler.Complete(*summary)
		return nil
	}

	expandScope := opts.Restack

	// Skip restack phase entirely if no branches need restacking and not expanding
	if !expandScope && len(branchesToRestack) == 0 && opts.RestackScope == nil {
		if !summary.HasChanges() {
			summary.UpToDate = true
		}
		handler.Complete(*summary)
		return nil
	}

	handler.EmitEvent(Event{Phase: PhaseRestack, Type: EventStarted})

	outcome, err := restackBranches(ctx, deps, branchesToRestack, opts.RestackScope, expandScope, dirtyAnchors, handler, summary)
	if outcome.EnteredConflictWorkflow {
		// The user chose to resolve a conflict now; sync hands off to the
		// conflict workflow and does not report a finished summary.
		return err
	}
	if err != nil {
		// Even on error, complete with summary
		handler.Complete(*summary)
		return fmt.Errorf("failed to restack branches: %w", err)
	}

	// Check if everything was up to date
	if !summary.HasChanges() {
		summary.UpToDate = true
	}

	deps.Log.Info("sync completed trunkUpdated=%v branchesRestacked=%v branchesDeleted=%v branchesSkipped=%v", summary.TrunkUpdated, summary.BranchesRestacked, summary.BranchesDeleted, summary.BranchesSkipped)

	handler.Complete(*summary)
	return nil
}

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
	Phase       Phase         // Current phase
	Type        EventType     // Event type
	Branch      string        // Branch name (if applicable)
	PRNumber    *git.PRNumber // PR number (if applicable)
	Message     string        // Human-readable description
	OldRevision string        // For position changes
	NewRevision string        // For position changes
	Conflict    bool          // Is this a conflict?
	HeldBy      string        // Why a worktree held this branch back (empty if it was not held)
	Error       error         // If non-nil, this step had an error
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
	BranchesBlocked   int      // Number of branches left untouched because their stack conflicted
	UpToDate          bool     // Everything was already current
	WorktreesCleaned  int      // Number of orphaned worktrees cleaned up
	SkippedStacks     []string // Stacks skipped due to dirty worktrees
	HeldBranches      []string // Branches (trunk included) a worktree held back
}

// HasChanges returns true if any operations were performed or anything was
// held back. A hold is not "up to date": the work is still outstanding.
func (s *Summary) HasChanges() bool {
	return s.TrunkUpdated || s.BranchesSynced > 0 || s.BranchesRestacked > 0 ||
		s.BranchesDeleted > 0 || s.BranchesSkipped > 0 || s.BranchesBlocked > 0 ||
		s.WorktreesCleaned > 0 || len(s.SkippedStacks) > 0 || len(s.HeldBranches) > 0
}

// Incomplete reports whether sync left work outstanding: conflicts, blocked or
// skipped stacks, or branches a worktree held back.
func (s *Summary) Incomplete() bool {
	return s.BranchesSkipped > 0 || len(s.ConflictBranches) > 0 || s.BranchesBlocked > 0 ||
		len(s.SkippedStacks) > 0 || len(s.HeldBranches) > 0
}

// recordHold notes a branch a worktree held back, once per branch.
func (s *Summary) recordHold(branch string) {
	if !slices.Contains(s.HeldBranches, branch) {
		s.HeldBranches = append(s.HeldBranches, branch)
	}
}

// Handler abstracts TTY vs non-TTY output for sync operations
// It embeds actions.RestackHandler to provide a unified interface for operations that include restacking
type Handler interface {
	// Start is called at the beginning of sync
	Start()

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
	// unpushedBranches identifies branches with local commits not yet pushed to remote.
	// Returns a map of branch names that the user confirmed for deletion.
	// In non-interactive mode, returns all branches except unpushed ones (which are skipped by default).
	PromptBranchDeletions(branches map[string]string, unpushedBranches map[string]bool) (confirmed map[string]bool, err error)

	// PromptResolveConflicts asks user if they want to resolve restack conflicts now or skip them.
	// Returns true to start conflict resolution workflow, false to skip and continue.
	// In non-interactive mode, returns (false, nil) to skip conflicts.
	PromptResolveConflicts(conflictBranches []string) (resolve bool, err error)

	// RestackHandler methods are available for restack-specific output
	// This allows the same handler to be used for standalone restack operations
	actions.RestackHandler
}

// NullHandler is a no-op handler for testing or when output is not needed
// NullHandler is a no-op handler for when nil is passed.
// It embeds handler.NullBase for Cleanup() and IsInteractive(), and
// actions.NullRestackHandler for the restack-specific no-op methods.
type NullHandler struct {
	handler.NullBase
	actions.NullRestackHandler
}

// Start implements Handler.
func (h *NullHandler) Start() {}

// EmitEvent implements Handler.
func (h *NullHandler) EmitEvent(Event) {}

// Complete implements Handler.
func (h *NullHandler) Complete(Summary) {}

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

// PromptBranchDeletions implements Handler. Skips unpushed branches, auto-confirms the rest.
func (h *NullHandler) PromptBranchDeletions(branches map[string]string, unpushedBranches map[string]bool) (map[string]bool, error) {
	confirmed := make(map[string]bool)
	for name := range branches {
		confirmed[name] = !unpushedBranches[name]
	}
	return confirmed, nil
}

// stackRootResolver resolves the stack a branch belongs to.
type stackRootResolver interface {
	GetBranch(name string) engine.Branch
	GetStackRootForBranch(branch engine.Branch) string
}

// dirtyAnchorSet is the set of stack-anchor branches whose worktrees have
// uncommitted changes; sync skips every branch in those stacks.
type dirtyAnchorSet map[string]bool

// includes returns true if the branch belongs to a dirty worktree's stack.
// A branch is in a dirty stack if its stack root (first ancestor whose parent is trunk)
// matches the anchor branch of a dirty worktree.
func (d dirtyAnchorSet) includes(eng stackRootResolver, branchName string) bool {
	if len(d) == 0 {
		return false
	}
	branch := eng.GetBranch(branchName)
	stackRoot := eng.GetStackRootForBranch(branch)
	return d[stackRoot]
}
