package actions

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"sync"

	"github.com/getstackit/stackit/internal/actions/validation"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/github"
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
type GetNullHandler struct {
	handlers.NullRestackHandler
}

// Start implements GetHandler.
func (h *GetNullHandler) Start(_ string, _ *int) {}

// EmitEvent implements GetHandler.
func (h *GetNullHandler) EmitEvent(_ GetEvent) {}

// Complete implements GetHandler.
func (h *GetNullHandler) Complete(_ GetSummary) {}

// GetOptions contains options for the get command
type GetOptions struct {
	Downstack bool // Don't sync upstack branches if branch exists locally
	Force     bool // Overwrite all fetched branches with remote source of truth
	Restack   bool // Restack after syncing (default true)
	Unfrozen  bool // Checkout new branches as unfrozen
}

// syncTargets is the evolving set of branches fetched by get, along with the
// parent and PR metadata discovered while walking their ancestry.
type syncTargets struct {
	branches       []string
	parentByBranch map[string]string
	prByBranch     map[string]*int
}

func newSyncTargets(targetBranch string) *syncTargets {
	return &syncTargets{
		branches:       []string{targetBranch},
		parentByBranch: make(map[string]string),
		prByBranch:     make(map[string]*int),
	}
}

// GetAction performs the get operation
func GetAction(ctx *app.Context, branchOrPR string, opts GetOptions, handler GetHandler) error {
	eng := ctx.Engine
	out := ctx.Output
	gctx := ctx.Context

	if err := validation.MustNotHaveUncommittedChanges(gctx, eng).Validate(); err != nil {
		return err
	}

	remoteCtx, cancelRemote := ctx.RemoteOperationContext()
	defer cancelRemote()

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
			pr, err := ctx.GitHub().GetPullRequest(remoteCtx, prNum)
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
	targets := newSyncTargets(targetBranch)

	// First fetch: the target's head plus all stack metadata. The metadata refspec is a
	// wildcard independent of which branch heads we request, so this single round trip
	// brings down the entire stack's parent chain — letting us discover ancestors from
	// local metadata instead of a serial, blocking GitHub crawl.
	if err := eng.FetchRemote(remoteCtx, engine.RemoteFetchRequest{
		Remote:          remote,
		Branches:        []string{targetBranch},
		IncludeMetadata: true,
	}); err != nil {
		return fmt.Errorf("failed to fetch %s from %s: %w", targetBranch, remote, err)
	}

	// Discover ancestors. Prefer the fetched metadata (no network); fall back to a GitHub
	// crawl only when the target has no stackit metadata (e.g. a branch never submitted
	// via stackit, or when the metadata cache fails to load).
	usedMetadata := false
	if err := eng.LoadRemoteMetadataCache(); err != nil {
		out.Debug("failed to load remote metadata cache: %v", err)
	} else {
		usedMetadata = targets.crawlAncestorsViaMetadata(eng, targetBranch)
	}
	if !usedMetadata {
		targets.crawlAncestorsViaGitHub(remoteCtx, ctx.GitHub(), eng, targetBranch)
	}
	branchesToSync := targets.branches
	parentMap := targets.parentByBranch
	branchPRInfo := targets.prByBranch

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

	// Second fetch: heads of any ancestors/descendants not already fetched. The metadata
	// wildcard already came down in the first fetch, so this only needs branch heads. The
	// target was fetched above, so it is pre-seeded as seen and skipped here. For a single
	// branch whose parent is trunk this list is empty, leaving exactly one fetch total.
	branchesToFetch := make([]string, 0, len(branchesToSync))
	seenFetchBranch := map[string]bool{targetBranch: true}
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

	if len(branchesToFetch) > 0 {
		if err := eng.FetchRemote(remoteCtx, engine.RemoteFetchRequest{
			Remote:   remote,
			Branches: branchesToFetch,
		}); err != nil {
			return fmt.Errorf("failed to fetch branches from %s: %w", remote, err)
		}
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
	branchFrozenStatus := make(map[string]bool) // branch -> is frozen

	// Fetch PR info for branches in parallel if possible
	if ctx.GitHub() != nil {
		trunkName := eng.Trunk().GetName()

		// Filter out trunk before parallel fetch
		branchesToFetch := make([]string, 0, len(branchesToSync))
		for _, branchName := range branchesToSync {
			if _, ok := branchPRInfo[branchName]; ok {
				continue
			}
			if branchName != trunkName {
				branchesToFetch = append(branchesToFetch, branchName)
			}
		}

		var mu sync.Mutex
		utils.Run(branchesToFetch, func(branchName string) {
			if pr, err := ctx.GitHub().GetPullRequestByBranch(remoteCtx, branchName); err == nil && pr != nil {
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

	// FetchRemote above already brought metadata refs local; apply any matching
	// remote metadata to the branches we synced.
	if err := eng.ApplyRemoteMetadataForBranches(ctx.Context, branchesToSync); err != nil {
		out.Debug("Failed to apply remote metadata: %v", err)
	}

	// Checkout target branch
	if err := eng.CheckoutBranch(gctx, eng.GetBranch(targetBranch)); err != nil {
		return fmt.Errorf("failed to checkout target branch %s: %w", targetBranch, err)
	}

	// Restack if requested
	var restacked, skipped int
	var conflicts, blocked []string
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
				// Prefer the PR number discovered during sync (which may have come
				// from remote metadata not copied into local metadata); fall back to
				// local metadata otherwise.
				prNumber := branchPRInfo[p.Branch]
				if prNumber == nil {
					prNumber = getPRNumber(eng, p.Branch)
				}

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
				case engine.RestackBlocked:
					blocked = append(blocked, p.Branch)
					handler.OnRestackBranch(p.Branch, handlers.RestackBlocked, "", prNumber, p.LockReason, p.Frozen, p.IsCurrent, parentName, p.Reparented, p.OldParent, p.NewParent, p.RerereResolvedCount)
				}
			}, ConflictModeEnterWorkflow); err != nil {
				handler.OnRestackComplete(restacked, skipped, conflicts, blocked)
				return fmt.Errorf("restack failed: %w", err)
			}

			handler.OnRestackComplete(restacked, skipped, conflicts, blocked)
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

// crawlAncestorsViaMetadata walks the parent chain of targetBranch using the fetched
// remote metadata cache (refs/stackit/remote-metadata/*), avoiding GitHub calls. It
// prepends discovered ancestors to branchesToSync (trunk-first) and records each branch's
// parent in parentMap and any known PR number in branchPRInfo. The second return value
// reports whether the target had usable metadata; when false the caller should fall back
// to a GitHub crawl. Callers must have populated the cache via LoadRemoteMetadataCache.
func (targets *syncTargets) crawlAncestorsViaMetadata(eng engine.Engine, targetBranch string) bool {
	view := eng.GetRemoteMetadataCache()
	trunkName := eng.Trunk().GetName()

	// No usable metadata for the target: signal a fallback to the GitHub crawl.
	if meta := view.Get(targetBranch); meta == nil || meta.GetParentBranchName() == nil {
		return false
	}

	discoveredBranches := slices.Clone(targets.branches)
	discoveredParents := make(map[string]string)
	discoveredPRs := make(map[string]*int)

	current := targetBranch
	for {
		meta := view.Get(current)
		if meta == nil {
			// A partial metadata chain is not safe to use: falling back lets GitHub
			// recover the full ancestry instead of syncing only part of the stack.
			return false
		}
		if pr := meta.GetPrInfo(); pr != nil && pr.Number != nil {
			discoveredPRs[current] = pr.Number
		}

		parent := meta.GetParentBranchName()
		if parent == nil || *parent == "" || *parent == trunkName {
			discoveredParents[current] = trunkName
			break
		}

		discoveredParents[current] = *parent
		if slices.Contains(discoveredBranches, *parent) {
			break // Avoid cycles
		}
		discoveredBranches = append([]string{*parent}, discoveredBranches...)
		current = *parent
	}

	for branch, parent := range discoveredParents {
		targets.parentByBranch[branch] = parent
	}
	for branch, prNumber := range discoveredPRs {
		targets.prByBranch[branch] = prNumber
	}
	targets.branches = discoveredBranches
	return true
}

// crawlAncestorsViaGitHub walks the parent chain of targetBranch using GitHub PR
// information. It prepends discovered ancestors to branchesToSync (trunk-first) and
// records each branch's parent in parentMap and PR number in branchPRInfo. It is a
// no-op when no GitHub client is configured. The (possibly grown) branchesToSync slice
// is returned because ancestors are prepended. The context bounds the GitHub reads; it
// takes the narrow github.Client/engine.Engine it needs rather than the full app
// context, so the unbounded command context is not reachable here by mistake.
func (targets *syncTargets) crawlAncestorsViaGitHub(ctx context.Context, gh github.Client, eng engine.Engine, targetBranch string) {
	if gh == nil {
		return
	}
	current := targetBranch
	for {
		pr, err := gh.GetPullRequestByBranch(ctx, current)
		if err != nil || pr == nil {
			break
		}
		prNum := pr.Number
		targets.prByBranch[current] = &prNum

		base := pr.Base
		if base == "" || base == eng.Trunk().GetName() {
			targets.parentByBranch[current] = eng.Trunk().GetName()
			break
		}

		targets.parentByBranch[current] = base
		if !slices.Contains(targets.branches, base) {
			targets.branches = append([]string{base}, targets.branches...)
			current = base
		} else {
			break // Avoid cycles
		}
	}
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
