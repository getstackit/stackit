package actions

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"sync"

	"github.com/getstackit/stackit/internal/actions/validation"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
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

// Reanchored records a branch whose recorded parent has landed and been deleted
// on the remote, along with the surviving ancestor it was tracked against
// instead. The branch's content is untouched: it still carries the landed
// parent's commits until it is restacked.
type Reanchored struct {
	// Branch is the branch that was tracked against a substitute parent.
	Branch string
	// LandedParent is the parent recorded on the remote, now gone from it.
	LandedParent string
	// LandedPR is the landed parent's PR number, when metadata recorded one.
	LandedPR *int
	// NewParent is the nearest surviving ancestor, often trunk.
	NewParent string
	// Anchor is the landed parent's tip at the time this branch was pushed, as
	// recorded in the branch's own remote metadata. It is what lets a later
	// restack replay only the branch's own commits. Empty when nothing on the
	// remote recorded it, which makes the branch unsafe to rebase — see
	// LandedAncestorReport.Unanchored.
	Anchor string
}

// StaleBranch is a branch this run leaves sitting on a landed ancestor's
// commits, together with what get is able to do about it.
type StaleBranch struct {
	// Name is the branch.
	Name string
	// Frozen reports whether this run leaves the branch frozen. A frozen branch
	// mirrors the remote and restack resets rather than rebases it, so it stays
	// stale until something unfreezes it.
	Frozen bool
	// Anchored reports whether the re-anchored branch at the root of this
	// branch's subtree recorded a divergence anchor. Without one, rebasing
	// replays the landed commits instead of dropping them.
	Anchored bool
}

// LandedAncestorReport describes every branch get re-anchored past landed work
// in this run, and whether get is in a position to offer to rebase them now.
type LandedAncestorReport struct {
	// Reanchored lists the substitutions get made, trunk-first.
	Reanchored []Reanchored
	// Stale lists every branch left carrying the landed commits — the
	// re-anchored branches plus their descendants in this run — trunk-first.
	Stale []StaleBranch
	// CanRestack is false when restacking is disabled for this run, leaving
	// nothing to offer.
	CanRestack bool
}

// Unfreezable returns the stale branches get can offer to rebase: frozen, so
// restack skips them as things stand, and anchored, so a rebase drops the
// landed commits rather than replaying them. Trunk-first.
func (r LandedAncestorReport) Unfreezable() []string {
	return r.staleNames(func(b StaleBranch) bool { return b.Frozen && b.Anchored })
}

// Unanchored returns the stale branches with no recorded divergence anchor.
// get cannot offer to rebase these — the replay range would still include the
// landed parent's commits — so they go on mirroring the remote. Trunk-first.
func (r LandedAncestorReport) Unanchored() []string {
	return r.staleNames(func(b StaleBranch) bool { return !b.Anchored })
}

func (r LandedAncestorReport) staleNames(match func(StaleBranch) bool) []string {
	var names []string
	for _, b := range r.Stale {
		if match(b) {
			names = append(names, b.Name)
		}
	}
	return names
}

// LandedAncestorDecision is a handler's answer to ReportLandedAncestors.
type LandedAncestorDecision int

const (
	// LeaveFrozen keeps the re-anchored branches mirroring the remote. They go
	// on carrying the landed commits until something unfreezes and rebases
	// them, which is a valid answer rather than a failure.
	LeaveFrozen LandedAncestorDecision = iota
	// UnfreezeAndRestack unfreezes the branches this run can safely rebase, so
	// the restack phase drops the landed commits.
	UnfreezeAndRestack
)

// GetHandler abstracts TTY vs non-TTY output for get operations
// It embeds RestackHandler to provide consistent output for restack phase
type GetHandler interface {
	// Start is called at the beginning of get with target info
	Start(targetBranch string, prNumber *int)

	// EmitEvent is called for each progress update
	EmitEvent(event GetEvent)

	// ReportLandedAncestors explains that branches were re-anchored past an
	// ancestor that landed and left the remote, and asks whether to unfreeze
	// the ones get can safely rebase (report.Unfreezable) so this run rebases
	// them onto their new parent.
	ReportLandedAncestors(report LandedAncestorReport) (LandedAncestorDecision, error)

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

// ReportLandedAncestors implements GetHandler. Declines to unfreeze, which
// leaves the fetched branches mirroring the remote.
func (h *GetNullHandler) ReportLandedAncestors(_ LandedAncestorReport) (LandedAncestorDecision, error) {
	return LeaveFrozen, nil
}

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
//
// It is the single source of truth for a run. GetAction reads through it rather
// than aliasing its fields into locals, so that a rewrite — reanchorPastLanded
// replacing a landed parent — is visible everywhere at once.
type syncTargets struct {
	// branches is every branch to sync, trunk-first.
	branches []string
	// parentByBranch is the parent to record for each branch.
	parentByBranch map[string]string
	// prByBranch is each branch's PR number, where one is known.
	prByBranch map[string]*int
	// anchorByBranch is the parent tip each branch was pushed on top of, read
	// from its remote metadata's ParentBranchRevision. It is the divergence
	// anchor when a parent has to be substituted, and it stays reachable from
	// the branch itself even once the parent's ref is gone from the remote.
	anchorByBranch map[string]string
	// prInfoByBranch holds full PR records for branches resolved through
	// GitHub, so the details already paid for are recorded rather than reduced
	// to a number.
	prInfoByBranch map[string]*engine.PrInfo
}

func newSyncTargets(targetBranch string) *syncTargets {
	return &syncTargets{
		branches:       []string{targetBranch},
		parentByBranch: make(map[string]string),
		prByBranch:     make(map[string]*int),
		anchorByBranch: make(map[string]string),
		prInfoByBranch: make(map[string]*engine.PrInfo),
	}
}

// appendBranches adds branches to the end of the sync set, skipping any already
// present.
func (targets *syncTargets) appendBranches(names ...string) {
	for _, name := range names {
		if !slices.Contains(targets.branches, name) {
			targets.branches = append(targets.branches, name)
		}
	}
}

// remoteMetadataLookup reads fetched remote metadata by branch name.
// engine.RemoteMetadataView satisfies it.
type remoteMetadataLookup interface {
	Get(branch string) *git.Meta
}

// harvestAnchors records a divergence anchor for every branch whose remote
// metadata carries one and that does not have one already.
//
// crawlAncestorsViaMetadata collects these as it walks, but it abandons the
// whole walk the moment one ancestor has no metadata — which is exactly the
// state a merged parent is left in, since branch cleanup deletes a merged
// branch's remote metadata ref during the author's own sync. The GitHub crawl
// that takes over records parents but knows no revisions, so without this pass
// the branch below the landed one is re-anchored with no anchor at all and a
// later restack replays the landed commits into it.
func (targets *syncTargets) harvestAnchors(view remoteMetadataLookup) {
	for _, name := range targets.branches {
		if _, ok := targets.anchorByBranch[name]; ok {
			continue
		}
		meta := view.Get(name)
		if meta == nil {
			continue
		}
		if rev := meta.GetParentBranchRevision(); rev != nil && *rev != "" {
			targets.anchorByBranch[name] = *rev
		}
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
	// Either crawl can leave a branch without its divergence anchor: the GitHub
	// one never reads them, and the metadata one abandons a partial chain. Fill
	// in whatever the remote metadata does have before anything relies on it.
	targets.harvestAnchors(eng.GetRemoteMetadataCache())

	// If target branch exists locally, identify local descendants
	targetBranchObj := eng.GetBranch(targetBranch)
	if !opts.Downstack && targetBranchObj.IsTracked() {
		graph := eng.Graph(engine.SortStrategyAlphabetical)
		upstack := graph.Range(targetBranchObj, engine.StackRange{RecursiveChildren: true})
		for _, b := range upstack {
			targets.appendBranches(b.GetName())
		}
	}

	// Second fetch: heads of any ancestors/descendants not already fetched. The metadata
	// wildcard already came down in the first fetch, so this only needs branch heads. The
	// target was fetched above, so it is pre-seeded as seen and skipped here. For a single
	// branch whose parent is trunk this list is empty, leaving exactly one fetch total.
	branchesToFetch := make([]string, 0, len(targets.branches))
	seenFetchBranch := map[string]bool{targetBranch: true}
	for _, branchName := range targets.branches {
		if branchName == trunkName && branchName != targetBranch {
			continue
		}
		if seenFetchBranch[branchName] {
			continue
		}
		seenFetchBranch[branchName] = true
		branchesToFetch = append(branchesToFetch, branchName)
	}

	var reanchored map[string]Reanchored
	if len(branchesToFetch) > 0 {
		if err := eng.FetchRemote(remoteCtx, engine.RemoteFetchRequest{
			Remote:   remote,
			Branches: branchesToFetch,
		}); err != nil {
			// Explicit refspecs are all-or-nothing: one ancestor that landed and
			// had its branch deleted fails the whole fetch, so the branch the
			// user actually asked for never arrives. Find out which of the
			// requested branches are gone, drop them, and fetch the rest. The
			// ls-remote only runs on this error path, so a healthy stack still
			// costs one round trip.
			gone, lsErr := eng.MissingRemoteBranches(remoteCtx, branchesToFetch)
			if lsErr != nil || len(gone) == 0 {
				return fmt.Errorf("failed to fetch branches from %s: %w", remote, err)
			}
			out.Debug("branches no longer on %s: %v", remote, gone)

			stillOnRemote := make([]string, 0, len(branchesToFetch))
			for _, branchName := range branchesToFetch {
				if !slices.Contains(gone, branchName) {
					stillOnRemote = append(stillOnRemote, branchName)
				}
			}
			if len(stillOnRemote) > 0 {
				if retryErr := eng.FetchRemote(remoteCtx, engine.RemoteFetchRequest{
					Remote:   remote,
					Branches: stillOnRemote,
				}); retryErr != nil {
					return fmt.Errorf("failed to fetch branches from %s: %w", remote, retryErr)
				}
			}

			// A gone branch that is still checked out locally keeps its children;
			// only one that is gone from both sides forces a substitute parent.
			// The engine already holds every local branch name, so this decision
			// never has to fall back to a guess.
			reanchored = targets.reanchorPastLanded(eng.BranchNames(), gone, trunkName)
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
	branchesToFreeze := engine.NewBranchesBuilder(len(targets.branches))

	// Fetch PR info for branches in parallel if possible
	if ctx.GitHub() != nil {
		trunkName := eng.Trunk().GetName()

		// Filter out trunk before parallel fetch
		branchesToFetch := make([]string, 0, len(targets.branches))
		for _, branchName := range targets.branches {
			if _, ok := targets.prByBranch[branchName]; ok {
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
				// Keep the whole record, not just the number: get already paid
				// for this round trip, and without it the branch lands with no
				// PR metadata at all, so `tree full` renders no GitHub state
				// until the next sync repeats the same fetch.
				info := engine.NewPrInfo(&prNum, pr.Title, pr.Body, pr.State, pr.Base, pr.HTMLURL, pr.Draft)
				mu.Lock()
				targets.prByBranch[branchName] = &prNum
				targets.prInfoByBranch[branchName] = info
				mu.Unlock()
			}
		})
	}

	// Sync each branch
	for _, branchName := range targets.branches {
		if branchName == eng.Trunk().GetName() {
			continue
		}

		branch := eng.GetBranch(branchName)
		isNew := !branch.IsTracked()

		if isNew {
			if err := eng.CreateBranch(gctx, branchName, fmt.Sprintf("%s/%s", remote, branchName)); err != nil {
				return fmt.Errorf("failed to create local branch %s: %w", branchName, err)
			}
			// Set initial metadata. A branch re-anchored past a landed parent is
			// tracked through TrackBranchPastLandedParent so the parent it was
			// actually pushed on top of anchors the divergence point; tracking it
			// straight against the substitute would take a merge-base that puts
			// the landed commits back into a later restack's replay range.
			if parent, ok := targets.parentByBranch[branchName]; ok {
				var err error
				if landed, ok := reanchored[branchName]; ok {
					err = eng.TrackBranchPastLandedParent(gctx, branchName, parent, engine.PriorParent{
						Branch:   landed.LandedParent,
						Revision: landed.Anchor,
					})
				} else {
					err = eng.TrackBranch(gctx, branchName, parent)
				}
				if err != nil {
					out.Debug("Failed to track branch %s with parent %s: %v", branchName, parent, err)
				}
			}
			// New branches are frozen by default unless --unfrozen
			if !opts.Unfrozen {
				branchesToFreeze.Add(eng.GetBranch(branchName))
			}
			branchesCreated++

			// Emit sync event
			handler.EmitEvent(GetEvent{
				Phase:    GetPhaseSync,
				Type:     GetEventCompleted,
				Branch:   branchName,
				PRNumber: targets.prByBranch[branchName],
				IsNew:    true,
			})
		} else {
			// Both updates below act on the branch that is currently checked
			// out: `git reset --hard` moves HEAD's branch, and a merge lands in
			// HEAD's branch. Without this checkout, running get while trunk is
			// checked out rewrites trunk to the fetched branch instead of
			// updating that branch, which silently destroys the local trunk.
			if err := eng.CheckoutBranch(gctx, branch); err != nil {
				return fmt.Errorf("failed to check out branch %s: %w", branchName, err)
			}
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
			if parent, ok := targets.parentByBranch[branchName]; ok {
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
				PRNumber: targets.prByBranch[branchName],
				IsNew:    false,
			})
		}
	}

	freezeSet := branchesToFreeze.Build()

	// Re-anchoring only corrects which parent a branch is recorded against. The
	// branch still carries the landed parent's commits until it is rebased, and
	// a frozen branch is never rebased — restack skips it and resets it to the
	// remote instead. Explain that, and let the user opt into unfreezing so the
	// restack phase below can finish the job.
	if len(reanchored) > 0 {
		report := LandedAncestorReport{
			Reanchored: targets.reanchoredInSyncOrder(reanchored),
			Stale:      targets.staleAfterReanchor(eng, reanchored, freezeSet),
			CanRestack: opts.Restack,
		}
		decision, err := handler.ReportLandedAncestors(report)
		if err != nil {
			out.Debug("Landed-ancestor prompt failed: %v", err)
		}
		// Unfreezing is only worth anything because the restack phase below acts
		// on it. Without that phase it would drop the protection and rebase
		// nothing, so act on the decision only when there is a restack to feed.
		if decision == UnfreezeAndRestack && opts.Restack {
			freezeSet = unfreezeForRestack(ctx, report.Unfreezable(), freezeSet)
		}
	}

	if len(freezeSet) > 0 {
		if _, err := eng.SetFrozen(ctx, freezeSet, true); err != nil {
			out.Debug("Failed to freeze new branches: %v", err)
		}
	}

	// Record the PR details fetched above. Without this the branch has no PR
	// metadata locally, and every consumer that filters on it — `tree full`
	// most visibly — shows nothing until a later sync refetches the same data.
	if len(targets.prInfoByBranch) > 0 {
		if err := eng.BatchUpsertPrInfo(gctx, targets.prInfoByBranch); err != nil {
			out.Debug("Failed to record PR info: %v", err)
		}
	}

	// FetchRemote above already brought metadata refs local; apply any matching
	// remote metadata to the branches we synced.
	if err := eng.ApplyRemoteMetadataForBranches(ctx.Context, targets.branches); err != nil {
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
		uniqueBranches := engine.NewBranchesBuilder(len(targets.branches))
		seen := make(map[string]bool)
		for _, name := range targets.branches {
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
				prNumber := targets.prByBranch[p.Branch]
				if prNumber == nil {
					prNumber = PRNumberForBranch(eng, p.Branch)
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

				event := handlers.RestackBranchEvent{
					Branch:              p.Branch,
					NewRevision:         p.NewRev,
					PRNumber:            prNumber,
					LockReason:          p.LockReason,
					Frozen:              p.Frozen,
					HeldBy:              p.HeldBy,
					IsCurrent:           p.IsCurrent,
					Parent:              parentName,
					Reparented:          p.Reparented,
					OldParent:           p.OldParent,
					NewParent:           p.NewParent,
					RerereResolvedCount: p.RerereResolvedCount,
				}

				switch p.Result {
				case engine.RestackDone:
					restacked++
					event.Result = handlers.RestackDone
				case engine.RestackUnneeded:
					event.Result = handlers.RestackUnneeded
				case engine.RestackConflict:
					skipped++
					conflicts = append(conflicts, p.Branch)
					event.Result = handlers.RestackConflict
				case engine.RestackBlocked:
					blocked = append(blocked, p.Branch)
					event.Result = handlers.RestackBlocked
				}
				handler.OnRestackBranch(event)
			}, ConflictModeEnterWorkflow); err != nil {
				handler.OnRestackComplete(handlers.RestackSummary{Restacked: restacked, Skipped: skipped, Conflicts: conflicts, Blocked: blocked})
				return fmt.Errorf("restack failed: %w", err)
			}

			handler.OnRestackComplete(handlers.RestackSummary{Restacked: restacked, Skipped: skipped, Conflicts: conflicts, Blocked: blocked})
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

// localBranchSet reports whether a branch name exists locally. engine.BranchSet
// satisfies it; tests pass a plain set.
type localBranchSet interface {
	Contains(name string) bool
}

// reanchorPastLanded drops the branches that are gone from the remote out of
// the sync set and re-points the children they left behind at the nearest
// surviving ancestor, returning the substitutions keyed by branch name.
//
// A branch that stackit metadata still names as a parent but that no longer has
// a ref on the remote has landed: it was pushed once, its commits are on trunk
// under new SHAs, and there is nothing left to fetch. Its children are still
// fetchable, and the nearest ancestor that does exist is the only coherent
// parent left to record for them.
//
// A gone branch that still exists locally keeps its children: it is a real
// branch the user may still be working with, and restack's landed-parent
// handling already reparents past it.
func (targets *syncTargets) reanchorPastLanded(localBranches localBranchSet, gone []string, trunkName string) map[string]Reanchored {
	if len(gone) == 0 {
		return nil
	}

	goneSet := make(map[string]bool, len(gone))
	unavailable := make(map[string]bool, len(gone))
	for _, name := range gone {
		goneSet[name] = true
		if !localBranches.Contains(name) {
			unavailable[name] = true
		}
	}

	remaining := make([]string, 0, len(targets.branches))
	for _, name := range targets.branches {
		if !goneSet[name] {
			remaining = append(remaining, name)
		}
	}
	targets.branches = remaining

	reanchored := make(map[string]Reanchored)
	for _, name := range remaining {
		parent, ok := targets.parentByBranch[name]
		if !ok || !unavailable[parent] {
			continue
		}
		newParent := targets.nearestAvailableAncestor(name, unavailable, trunkName)
		targets.parentByBranch[name] = newParent
		reanchored[name] = Reanchored{
			Branch:       name,
			LandedParent: parent,
			LandedPR:     targets.prByBranch[parent],
			NewParent:    newParent,
			Anchor:       targets.anchorByBranch[name],
		}
	}

	return reanchored
}

// reanchoredInSyncOrder returns the substitutions trunk-first, matching the
// order the branches are synced in. The map is the working form; this is the
// reporting form.
func (targets *syncTargets) reanchoredInSyncOrder(reanchored map[string]Reanchored) []Reanchored {
	ordered := make([]Reanchored, 0, len(reanchored))
	for _, name := range targets.branches {
		if r, ok := reanchored[name]; ok {
			ordered = append(ordered, r)
		}
	}
	return ordered
}

// nearestAvailableAncestor walks up branchName's recorded ancestry until it
// reaches a branch that is still available, falling back to trunk.
func (targets *syncTargets) nearestAvailableAncestor(branchName string, unavailable map[string]bool, trunkName string) string {
	seen := map[string]bool{branchName: true}
	current := branchName
	for {
		parent, ok := targets.parentByBranch[current]
		if !ok || parent == "" || parent == trunkName || seen[parent] {
			return trunkName
		}
		if !unavailable[parent] {
			return parent
		}
		seen[parent] = true
		current = parent
	}
}

// staleAfterReanchor returns every branch this run leaves sitting on landed
// commits — each re-anchored branch plus its descendants within the sync set —
// in sync order, along with what get can do about each.
//
// Descendants are included because none of them are clean until the whole
// subtree is rebased onto the new parent, and they inherit their subtree root's
// anchor: with no anchor for the root, rebasing anything above it replays the
// landed commits too.
//
// pendingFreeze is the set queued to be frozen once the sync loop finishes, so
// that a branch created moments ago counts as frozen alongside one that already
// was.
func (targets *syncTargets) staleAfterReanchor(eng engine.Engine, reanchored map[string]Reanchored, pendingFreeze engine.Branches) []StaleBranch {
	inSync := make(map[string]bool, len(targets.branches))
	for _, name := range targets.branches {
		inSync[name] = true
	}

	graph := eng.Graph(engine.SortStrategyAlphabetical)
	anchored := make(map[string]bool, len(reanchored))
	mark := func(name string, hasAnchor bool) {
		if !inSync[name] {
			return
		}
		// A branch has one parent, so it belongs to one re-anchored subtree.
		// Should that ever stop holding, the unanchored answer is the safe one.
		if prev, seen := anchored[name]; seen && !prev {
			return
		}
		anchored[name] = hasAnchor
	}

	for _, r := range reanchored {
		hasAnchor := r.Anchor != ""
		// Mark the branch itself first: a branch that failed to track has no
		// place in the graph, and it is still the one carrying landed commits.
		mark(r.Branch, hasAnchor)
		for _, b := range graph.Range(eng.GetBranch(r.Branch), engine.StackRange{RecursiveChildren: true}) {
			mark(b.GetName(), hasAnchor)
		}
	}

	stale := make([]StaleBranch, 0, len(anchored))
	for _, name := range targets.branches {
		hasAnchor, ok := anchored[name]
		if !ok {
			continue
		}
		stale = append(stale, StaleBranch{
			Name:     name,
			Frozen:   pendingFreeze.Contains(name) || eng.GetBranch(name).IsFrozen(),
			Anchored: hasAnchor,
		})
	}
	return stale
}

// unfreezeForRestack thaws the named branches so the restack phase can rebase
// them, and returns pendingFreeze with those branches removed so the freeze
// that follows does not put them straight back.
func unfreezeForRestack(ctx *app.Context, names []string, pendingFreeze engine.Branches) engine.Branches {
	if len(names) == 0 {
		return pendingFreeze
	}

	unfreezing := make(map[string]bool, len(names))
	for _, name := range names {
		unfreezing[name] = true
	}

	eng := ctx.Engine
	thaw := engine.NewBranchesBuilder(len(names))
	for _, name := range names {
		if branch := eng.GetBranch(name); branch.IsFrozen() {
			thaw.Add(branch)
		}
	}
	if thawSet := thaw.Build(); len(thawSet) > 0 {
		if _, err := eng.SetFrozen(ctx, thawSet, false); err != nil {
			ctx.Output.Debug("Failed to unfreeze branches re-anchored past landed work: %v", err)
		}
	}

	return pendingFreeze.Filter(func(b engine.Branch) bool { return !unfreezing[b.GetName()] })
}

// crawlAncestorsViaMetadata walks the parent chain of targetBranch using the fetched
// remote metadata cache (refs/stackit/remote-metadata/*), avoiding GitHub calls. It
// prepends discovered ancestors to the sync set (trunk-first) and records each branch's
// parent, divergence anchor, and any known PR number on targets. The return value
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
	discoveredAnchors := make(map[string]string)

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
		if rev := meta.GetParentBranchRevision(); rev != nil && *rev != "" {
			discoveredAnchors[current] = *rev
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

	maps.Copy(targets.parentByBranch, discoveredParents)
	maps.Copy(targets.prByBranch, discoveredPRs)
	maps.Copy(targets.anchorByBranch, discoveredAnchors)
	targets.branches = discoveredBranches
	return true
}

// crawlAncestorsViaGitHub walks the parent chain of targetBranch using GitHub PR
// information. It prepends discovered ancestors to the sync set (trunk-first) and
// records each branch's parent and PR number on targets. It is a no-op when no GitHub
// client is configured. PR bases carry no revisions, so the anchors it leaves behind
// come from harvestAnchors instead. The context bounds the GitHub reads; it takes the
// narrow github.Client/engine.Engine it needs rather than the full app context, so the
// unbounded command context is not reachable here by mistake.
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
		targets.prInfoByBranch[current] = engine.NewPrInfo(&prNum, pr.Title, pr.Body, pr.State, pr.Base, pr.HTMLURL, pr.Draft)

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
