package actions

import (
	"fmt"
	"slices"
	"strconv"
	"sync"

	"github.com/getstackit/stackit/internal/actions/validation"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/handlers"
	"github.com/getstackit/stackit/internal/utils"
)

// GetPhase represents the current phase of the get operation
type GetPhase string

// Phases of the get operation
const (
	GetPhaseFetch    GetPhase = "fetch"    // Fetching branches from remote
	GetPhaseSync     GetPhase = "sync"     // Syncing branches (create/update)
	GetPhaseMetadata GetPhase = "metadata" // Fetching and applying metadata
	GetPhaseCheckout GetPhase = "checkout" // Checking out target branch
)

// GetEventType represents the type of get event
type GetEventType string

// Event types for get operations
const (
	GetEventStarted   GetEventType = "started"
	GetEventProgress  GetEventType = "progress"
	GetEventCompleted GetEventType = "completed"
	GetEventSkipped   GetEventType = "skipped"
)

// GetEvent represents a progress update during get
type GetEvent struct {
	Phase       GetPhase     // Current phase
	Type        GetEventType // Event type
	Branch      string       // Branch name (if applicable)
	PRNumber    *int         // PR number (if applicable)
	Message     string       // Human-readable description
	NewRevision string       // For position changes
	IsNew       bool         // Is this a new branch?
	Error       error        // If non-nil, this step had an error
}

// GetSummary holds aggregate results from a get operation
type GetSummary struct {
	TargetBranch    string // The branch that was retrieved
	BranchesCreated int    // Number of branches created
	BranchesUpdated int    // Number of branches updated
	Restacked       int    // Number of branches restacked
	IsFrozen        bool   // Was the target branch frozen?
	UpToDate        bool   // Everything was already current
}

// GetHandler abstracts TTY vs non-TTY output for get operations
// It embeds RestackHandler to provide consistent output for restack phase
type GetHandler interface {
	// Start is called at the beginning of get with target info
	Start(targetBranch string, prNumber *int)

	// EmitEvent is called for each progress update
	EmitEvent(event GetEvent)

	// Complete is called when get finishes with the summary
	Complete(summary GetSummary)

	// RestackHandler methods are available for restack phase output
	// This ensures consistent restack output between get, sync, and restack commands
	handlers.RestackHandler
}

// GetNullHandler is a no-op handler for testing or when output is not needed
type GetNullHandler struct{}

// Start implements GetHandler.
func (h *GetNullHandler) Start(_ string, _ *int) {}

// EmitEvent implements GetHandler.
func (h *GetNullHandler) EmitEvent(_ GetEvent) {}

// Complete implements GetHandler.
func (h *GetNullHandler) Complete(_ GetSummary) {}

// OnRestackStart implements RestackHandler.
func (h *GetNullHandler) OnRestackStart(_ int) {}

// OnRestackBranch implements RestackHandler.
func (h *GetNullHandler) OnRestackBranch(_ string, _ handlers.RestackResult, _ string, _ *int, _ engine.LockReason, _ bool, _ bool, _ string, _ bool, _, _ string, _ int) {
}

// OnRestackComplete implements RestackHandler.
func (h *GetNullHandler) OnRestackComplete(_, _ int, _ []string) {}

// GetOptions contains options for the get command
type GetOptions struct {
	Downstack bool // Don't sync upstack branches if branch exists locally
	Force     bool // Overwrite all fetched branches with remote source of truth
	Restack   bool // Restack after syncing (default true)
	Unfrozen  bool // Checkout new branches as unfrozen
}

// GetAction performs the get operation
func GetAction(ctx *app.Context, branchOrPR string, opts GetOptions, handler GetHandler) error {
	eng := ctx.Engine
	out := ctx.Output
	gctx := ctx.Context

	if err := validation.MustNotHaveUncommittedChanges(gctx, ctx.Git()).Validate(); err != nil {
		return err
	}

	targetBranch := ""
	var targetPRNumber *int
	if branchOrPR == "" {
		current := eng.CurrentBranch()
		if current == nil {
			return fmt.Errorf("not on a branch and no branch/PR specified")
		}
		targetBranch = current.GetName()
	} else {
		// Check if it's a PR number
		if prNum, err := strconv.Atoi(branchOrPR); err == nil {
			if _, err := ctx.RequireGitHub(); err != nil {
				return fmt.Errorf("cannot resolve PR #%d: %w", prNum, err)
			}
			owner, repo := ctx.GitHub().GetOwnerRepo()
			pr, err := ctx.GitHub().GetPullRequest(gctx, owner, repo, prNum)
			if err != nil {
				return fmt.Errorf("failed to get PR #%d: %w", prNum, err)
			}
			targetBranch = pr.Head
			targetPRNumber = &prNum
		} else {
			targetBranch = branchOrPR
		}
	}

	// Start the handler
	handler.Start(targetBranch, targetPRNumber)

	// Emit fetch phase started
	handler.EmitEvent(GetEvent{
		Phase: GetPhaseFetch,
		Type:  GetEventStarted,
	})

	remote := eng.GetRemote()
	trunkName := eng.Trunk().GetName()

	// Identify branches to sync (ancestors + descendants)
	branchesToSync := []string{targetBranch}
	parentMap := make(map[string]string)

	// Crawl ancestors using GitHub PR info if possible
	if ctx.GitHub() != nil {
		current := targetBranch
		owner, repo := ctx.GitHub().GetOwnerRepo()
		for {
			pr, err := ctx.GitHub().GetPullRequestByBranch(gctx, owner, repo, current)
			if err != nil || pr == nil {
				break
			}

			base := pr.Base
			if base == "" || base == eng.Trunk().GetName() {
				parentMap[current] = eng.Trunk().GetName()
				break
			}

			parentMap[current] = base
			if !slices.Contains(branchesToSync, base) {
				branchesToSync = append([]string{base}, branchesToSync...)
				current = base
			} else {
				break // Avoid cycles
			}
		}
	}

	// If target branch exists locally, identify local descendants
	targetBranchObj := eng.GetBranch(targetBranch)
	if !opts.Downstack && targetBranchObj.IsTracked() {
		graph := eng.Graph(engine.SortStrategyAlphabetical)
		upstack := graph.Range(targetBranchObj, engine.StackRange{RecursiveChildren: true})
		for _, b := range upstack {
			if !slices.Contains(branchesToSync, b.GetName()) {
				branchesToSync = append(branchesToSync, b.GetName())
			}
		}
	}

	branchesToFetch := make([]string, 0, len(branchesToSync))
	seenFetchBranch := make(map[string]bool, len(branchesToSync))
	for _, branchName := range branchesToSync {
		if branchName == trunkName && branchName != targetBranch {
			continue
		}
		if seenFetchBranch[branchName] {
			continue
		}
		seenFetchBranch[branchName] = true
		branchesToFetch = append(branchesToFetch, branchName)
	}

	if err := eng.FetchRemote(gctx, engine.RemoteFetchRequest{
		Remote:          remote,
		Branches:        branchesToFetch,
		IncludeMetadata: true,
	}); err != nil {
		return fmt.Errorf("failed to fetch branches from %s: %w", remote, err)
	}

	// Emit trunk status (main/master)
	handler.EmitEvent(GetEvent{
		Phase:  GetPhaseFetch,
		Type:   GetEventCompleted,
		Branch: trunkName,
	})

	// Emit sync phase started
	handler.EmitEvent(GetEvent{
		Phase: GetPhaseSync,
		Type:  GetEventStarted,
	})

	// Track statistics for summary
	var branchesCreated, branchesUpdated int
	branchPRInfo := make(map[string]*int)       // branch -> PR number
	branchFrozenStatus := make(map[string]bool) // branch -> is frozen

	// Fetch PR info for branches in parallel if possible
	if ctx.GitHub() != nil {
		owner, repo := ctx.GitHub().GetOwnerRepo()
		trunkName := eng.Trunk().GetName()

		// Filter out trunk before parallel fetch
		branchesToFetch := make([]string, 0, len(branchesToSync))
		for _, branchName := range branchesToSync {
			if branchName != trunkName {
				branchesToFetch = append(branchesToFetch, branchName)
			}
		}

		var mu sync.Mutex
		utils.Run(branchesToFetch, func(branchName string) {
			if pr, err := ctx.GitHub().GetPullRequestByBranch(gctx, owner, repo, branchName); err == nil && pr != nil {
				prNum := pr.Number
				mu.Lock()
				branchPRInfo[branchName] = &prNum
				mu.Unlock()
			}
		})
	}

	// Sync each branch
	for _, branchName := range branchesToSync {
		if branchName == eng.Trunk().GetName() {
			continue
		}

		branch := eng.GetBranch(branchName)
		isNew := !branch.IsTracked()

		if isNew {
			if err := eng.CreateBranch(gctx, branchName, fmt.Sprintf("%s/%s", remote, branchName)); err != nil {
				return fmt.Errorf("failed to create local branch %s: %w", branchName, err)
			}
			// Set initial metadata
			if parent, ok := parentMap[branchName]; ok {
				if err := eng.TrackBranch(gctx, branchName, parent); err != nil {
					out.Debug("Failed to track branch %s with parent %s: %v", branchName, parent, err)
				}
			}
			// New branches are frozen by default unless --unfrozen
			isFrozen := !opts.Unfrozen
			branchFrozenStatus[branchName] = isFrozen
			if isFrozen {
				if _, err := eng.SetFrozen(ctx, engine.BranchesOf(eng.GetBranch(branchName)), true); err != nil {
					out.Debug("Failed to freeze new branch %s: %v", branchName, err)
				}
			}
			branchesCreated++

			// Emit sync event
			handler.EmitEvent(GetEvent{
				Phase:    GetPhaseSync,
				Type:     GetEventCompleted,
				Branch:   branchName,
				PRNumber: branchPRInfo[branchName],
				IsNew:    true,
			})
		} else {
			if opts.Force {
				if err := eng.ResetHard(gctx, fmt.Sprintf("%s/%s", remote, branchName)); err != nil {
					return fmt.Errorf("failed to reset branch %s: %w", branchName, err)
				}
			} else {
				// Try to merge. If conflicts, this will error and we'll stop.
				if err := eng.Merge(gctx, fmt.Sprintf("%s/%s", remote, branchName), engine.MergeOptions{}); err != nil {
					return fmt.Errorf("conflict during sync of %s. Resolve conflicts and try again: %w", branchName, err)
				}
			}
			// Update parent if known, preserving the divergence point so that
			// restacking doesn't carry commits from the old parent.
			if parent, ok := parentMap[branchName]; ok {
				if err := eng.ReparentBranch(gctx, branch, eng.GetBranch(parent)); err != nil {
					out.Debug("Failed to update parent for %s: %v", branchName, err)
				}
			}
			branchesUpdated++

			// Emit sync event
			handler.EmitEvent(GetEvent{
				Phase:    GetPhaseSync,
				Type:     GetEventCompleted,
				Branch:   branchName,
				PRNumber: branchPRInfo[branchName],
				IsNew:    false,
			})
		}
	}

	// Fetch and apply remote metadata for all branches in the stack
	// Configure refspec so future git fetch commands also fetch metadata
	if err := eng.ConfigureRemoteMetadataSync(ctx.Context); err != nil {
		out.Debug("Failed to configure metadata refspec: %v", err)
	}
	if err := eng.LoadRemoteMetadataCache(); err != nil {
		out.Debug("Failed to load remote metadata cache: %v", err)
	} else {
		for _, branchName := range branchesToSync {
			if branchName == eng.Trunk().GetName() {
				continue
			}
			if err := eng.ApplyRemoteMetadataIfExists(branchName); err != nil {
				out.Debug("Failed to apply metadata for %s: %v", branchName, err)
			}
		}
	}

	// Checkout target branch
	if err := eng.CheckoutBranch(gctx, eng.GetBranch(targetBranch)); err != nil {
		return fmt.Errorf("failed to checkout target branch %s: %w", targetBranch, err)
	}

	// `get` may have created local branches via raw git fetch above; rebuild
	// so engine's branch list includes those new branches before we proceed.
	if err := eng.Rebuild(""); err != nil {
		return fmt.Errorf("failed to refresh engine: %w", err)
	}

	// Restack if requested
	var restacked, skipped int
	var conflicts []string
	if opts.Restack {
		uniqueBranches := engine.NewBranchesBuilder(len(branchesToSync))
		seen := make(map[string]bool)
		for _, name := range branchesToSync {
			if !seen[name] {
				seen[name] = true
				b := eng.GetBranch(name)
				if b.IsTracked() {
					uniqueBranches.Add(b)
				}
			}
		}
		sorted := eng.SortBranchesTopologically(uniqueBranches.Build())
		if len(sorted) > 0 {
			// Use RestackHandler for consistent output
			handler.OnRestackStart(len(sorted))

			if err := RestackBranchesWithHandler(ctx, sorted, func(p RestackProgress) {
				prNumber := getPRNumber(eng, p.Branch)

				parentName := ""
				br := eng.GetBranch(p.Branch)
				if br.GetName() != "" {
					if parent := br.GetParent(); parent != nil {
						parentName = parent.GetName()
					} else {
						parentName = eng.Trunk().GetName()
					}
				}

				switch p.Result {
				case engine.RestackDone:
					restacked++
					handler.OnRestackBranch(p.Branch, handlers.RestackDone, p.NewRev, prNumber, p.LockReason, p.Frozen, p.IsCurrent, parentName, p.Reparented, p.OldParent, p.NewParent, p.RerereResolvedCount)
				case engine.RestackUnneeded:
					handler.OnRestackBranch(p.Branch, handlers.RestackUnneeded, "", prNumber, p.LockReason, p.Frozen, p.IsCurrent, parentName, p.Reparented, p.OldParent, p.NewParent, p.RerereResolvedCount)
				case engine.RestackConflict:
					skipped++
					conflicts = append(conflicts, p.Branch)
					handler.OnRestackBranch(p.Branch, handlers.RestackConflict, "", prNumber, p.LockReason, p.Frozen, p.IsCurrent, parentName, p.Reparented, p.OldParent, p.NewParent, p.RerereResolvedCount)
				}
			}, ConflictModeEnterWorkflow); err != nil {
				handler.OnRestackComplete(restacked, skipped, conflicts)
				return fmt.Errorf("restack failed: %w", err)
			}

			handler.OnRestackComplete(restacked, skipped, conflicts)
		}
	}

	// Complete with summary
	targetBranchObj = eng.GetBranch(targetBranch)
	isFrozenFinal := targetBranchObj.IsFrozen()
	upToDate := branchesCreated == 0 && branchesUpdated == 0 && restacked == 0
	handler.Complete(GetSummary{
		TargetBranch:    targetBranch,
		BranchesCreated: branchesCreated,
		BranchesUpdated: branchesUpdated,
		Restacked:       restacked,
		IsFrozen:        isFrozenFinal,
		UpToDate:        upToDate,
	})

	return nil
}

// getPRNumber returns the PR number for a branch, or nil if not available
func getPRNumber(eng engine.Engine, branchName string) *int {
	branch := eng.GetBranch(branchName)
	prInfo, err := branch.GetPrInfo()
	if err != nil || prInfo == nil {
		return nil
	}
	return prInfo.Number()
}
