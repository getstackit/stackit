package actions

import (
	"errors"
	"fmt"
	stdruntime "runtime"
	"strings"
	"sync"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/handlers"
	"github.com/getstackit/stackit/internal/rerere"
	"github.com/getstackit/stackit/internal/tui"
	"github.com/getstackit/stackit/internal/utils"
)

var newWorktreeEngine = engine.NewEngineForWorktree

// RestackOptions contains options for the restack command
type RestackOptions struct {
	BranchName         string
	Scope              engine.StackRange
	AllStacks          bool
	StackRoots         []string
	ContinueOnConflict bool
	Parallel           bool // Run independent stack groups in parallel worktrees
	Jobs               int  // Number of parallel workers (0 = NumCPU)
}

// RestackPlan is the precomputed work for a restack operation. Build it via
// PlanRestack, inspect via HasBranches/HasWork, then pass it to RestackAction.
//
// Sharing the plan between the CLI's "is there work?" gate and the action
// itself avoids running engine.PlanRestack twice — that's ~3 git operations
// per branch, ~10ms/branch on a warm filesystem.
type RestackPlan struct {
	opts   RestackOptions
	groups []restackPlannedGroup
}

type restackConflictPrompter interface {
	IsInteractive() bool
	PromptResolveConflicts(conflictBranches []string) (bool, error)
}

// restackPlannedGroup pairs an independent stack with the engine plan that
// describes which of its branches actually need rebasing. enginePlan is nil
// for empty groups (which never reach the action's main loop).
type restackPlannedGroup struct {
	rootBranch     string
	sortedBranches engine.Branches
	enginePlan     *engine.RestackPlan
}

// HasBranches reports whether any branch was resolved from the requested
// scope. False means the scope is empty (e.g. on trunk with no children).
func (p *RestackPlan) HasBranches() bool {
	for _, g := range p.groups {
		if len(g.sortedBranches) > 0 {
			return true
		}
	}
	return false
}

// HasWork reports whether any resolved branch needs an actual rebase.
// Branches that are up-to-date, locked, or frozen do not count.
func (p *RestackPlan) HasWork() bool {
	for _, g := range p.groups {
		if g.enginePlan == nil {
			continue
		}
		if len(g.enginePlan.Specs) > 0 || len(g.enginePlan.ApplyMap) > 0 {
			return true
		}
	}
	return false
}

// BranchCount returns the total number of branches across all groups.
func (p *RestackPlan) BranchCount() int {
	n := 0
	for _, g := range p.groups {
		n += len(g.sortedBranches)
	}
	return n
}

// PlanRestack resolves the branch groups for opts and pre-computes the
// engine restack plan for each group. Pass the result to RestackAction to
// skip recomputing those plans during execution.
//
// The per-group engine plan is *not* reused in parallel mode — each worktree
// has its own engine and must compute its own plan against worktree-local
// state.
func PlanRestack(ctx *app.Context, opts RestackOptions) (*RestackPlan, error) {
	eng := ctx.Engine
	rawGroups, err := planRestackBranchGroups(eng, opts)
	if err != nil {
		return nil, err
	}
	if err := guardUnpushedTrunk(ctx, eng, rawGroups); err != nil {
		return nil, err
	}
	plan := &RestackPlan{opts: opts}
	for _, g := range rawGroups {
		sorted := eng.SortBranchesTopologically(g.branches)
		var enginePlan *engine.RestackPlan
		if len(sorted) > 0 {
			enginePlan, err = eng.PlanRestack(ctx.Context, sorted)
			if err != nil {
				return nil, err
			}
		}
		plan.groups = append(plan.groups, restackPlannedGroup{
			rootBranch:     g.rootBranch,
			sortedBranches: sorted,
			enginePlan:     enginePlan,
		})
	}
	return plan, nil
}

// RestackAction performs the restack operation using a pre-computed plan.
// Build the plan via PlanRestack — callers are expected to use the plan to
// gate UX decisions (e.g. whether to start a TUI), so requiring it here
// keeps the two phases consistent.
func RestackAction(ctx *app.Context, plan *RestackPlan, handler handlers.RestackHandler) error {
	if plan == nil {
		return fmt.Errorf("restack plan is required")
	}
	out := ctx.Output
	opts := plan.opts
	eng := ctx.Engine

	branchCount := plan.BranchCount()
	if branchCount == 0 {
		out.Info("No branches to restack.")
		return nil
	}

	ctx.Logger.Info("restack started branchCount=%v", branchCount)

	// Take snapshot before modifying the repository. Skip it when no branch
	// actually needs a rebase: an up-to-date restack mutates nothing, so there
	// is nothing to undo and the snapshot is pure overhead (it hits every no-op
	// restack, and sync runs restack often).
	if plan.HasWork() {
		snapshotOpts := NewSnapshot("restack",
			WithArg(opts.BranchName),
			WithFlag(opts.AllStacks, "--all-stacks"),
			WithFlagValue("--stacks", strings.Join(opts.StackRoots, ",")),
			WithFlag(opts.ContinueOnConflict, "--continue-on-conflict"),
			WithFlag(opts.Parallel, "--parallel"),
		)
		TakeBestEffortSnapshot(ctx, snapshotOpts)
	}

	// If no handler provided, use NullRestackHandler (silent)
	if handler == nil {
		handler = &handlers.NullRestackHandler{}
	}

	interactiveRererePrompt := ctx.Interactive && !ctx.Quiet && tui.IsTTY()
	if _, jsonOutput := handler.(*handlers.JSONRestackHandler); jsonOutput {
		interactiveRererePrompt = false
	}
	pauser, _ := handler.(rerere.Pauser)
	if _, err := rerere.EnsureEnabled(ctx.Context, ctx.Engine, interactiveRererePrompt, pauser); err != nil {
		out.Warn("Failed to enable git rerere: %v", err)
	}

	// Use RestackHandler for consistent output
	handler.OnRestackStart(branchCount)

	var restacked, skipped int
	var conflicts, blocked []string
	prompter, promptForConflicts := conflictPrompter(handler, opts)

	// Parallel mode: dispatch independent stack groups to separate worktrees.
	// The pre-computed engine plans are not portable across worktree engines,
	// so each worker computes its own plan inside its worktree.
	if opts.Parallel && len(plan.groups) > 1 {
		var err error
		restacked, skipped, conflicts, blocked, err = restackGroupsParallel(ctx, opts, plan.groups, handler)
		ctx.Logger.Info("restack completed (parallel) restacked=%v skipped=%v conflicts=%v blocked=%v", restacked, skipped, len(conflicts), len(blocked))
		handler.OnRestackComplete(restacked, skipped, conflicts, blocked)
		if err != nil {
			return fmt.Errorf("restack failed: %w", err)
		}
		return nil
	}

	conflictMode := ConflictModeEnterWorkflow
	if opts.ContinueOnConflict || promptForConflicts {
		conflictMode = ConflictModeContinue
	}

	for _, group := range plan.groups {
		// Wrap the progress callback to inject the stack root for this group.
		groupRoot := group.rootBranch
		progress := func(p RestackProgress) {
			p.StackRoot = groupRoot
			handleRestackProgress(eng, handler, p, &restacked, &skipped, &conflicts, &blocked)
		}

		if err := restackBranchesWithPlan(ctx, group.sortedBranches, group.enginePlan, progress, conflictMode); err != nil {
			return fmt.Errorf("restack failed: %w", err)
		}
	}

	ctx.Logger.Info("restack completed restacked=%v skipped=%v conflicts=%v blocked=%v", restacked, skipped, len(conflicts), len(blocked))

	if promptForConflicts && len(conflicts) > 0 {
		resolve, err := prompter.PromptResolveConflicts(conflicts)
		if err != nil {
			return fmt.Errorf("failed to prompt for conflict resolution: %w", err)
		}
		if resolve {
			return EnterConflictWorkflow(ctx, conflicts[0], branchesForConflict(plan, conflicts[0]))
		}
	}

	handler.OnRestackComplete(restacked, skipped, conflicts, blocked)
	return nil
}

func conflictPrompter(handler handlers.RestackHandler, opts RestackOptions) (restackConflictPrompter, bool) {
	if opts.ContinueOnConflict {
		return nil, false
	}
	prompter, ok := handler.(restackConflictPrompter)
	if !ok || !prompter.IsInteractive() {
		return nil, false
	}
	return prompter, true
}

func branchesForConflict(plan *RestackPlan, conflictBranch string) engine.Branches {
	for _, group := range plan.groups {
		for _, branch := range group.sortedBranches {
			if branch.GetName() == conflictBranch {
				return group.sortedBranches
			}
		}
	}
	return nil
}

// parallelResultCollector serializes progress updates, error accumulation, and
// counter bookkeeping across parallel restack workers. All methods take the
// mutex internally; callers must not hold it.
type parallelResultCollector struct {
	mu        sync.Mutex
	eng       engine.BranchReader
	handler   handlers.RestackHandler
	restacked int
	skipped   int
	conflicts []string
	blocked   []string
	errs      []error
}

// recordProgress routes a progress event from a worker through handleRestackProgress.
func (c *parallelResultCollector) recordProgress(p RestackProgress) {
	c.mu.Lock()
	defer c.mu.Unlock()
	handleRestackProgress(c.eng, c.handler, p, &c.restacked, &c.skipped, &c.conflicts, &c.blocked)
}

// recordGroupFailure attributes every branch in a failed group to the handler
// as a conflict so the final summary reflects what the worker did not process.
// Without this a worktree/engine setup failure silently shows "skipped=0" while
// an entire stack failed to start.
func (c *parallelResultCollector) recordGroupFailure(group restackPlannedGroup, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.errs = append(c.errs, err)
	for _, b := range group.sortedBranches {
		handleRestackProgress(c.eng, c.handler, RestackProgress{
			Branch:    b.GetName(),
			Result:    engine.RestackConflict,
			Conflict:  true,
			StackRoot: group.rootBranch,
		}, &c.restacked, &c.skipped, &c.conflicts, &c.blocked)
	}
}

// recordRebaseError records an unexpected error returned from
// RestackBranchesWithHandler. Per-branch outcomes were already routed through
// recordProgress by the progress callback before the error surfaced, so only
// the error itself needs to be captured.
func (c *parallelResultCollector) recordRebaseError(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.errs = append(c.errs, err)
}

// joinedError returns the accumulated errors as a single joined error, or nil.
func (c *parallelResultCollector) joinedError() error {
	if len(c.errs) == 0 {
		return nil
	}
	return errors.Join(c.errs...)
}

// restackGroupsParallel runs each independent stack group in its own temporary
// worktree. The groups have no shared branches, so rebases + ref updates are
// safe to interleave. Progress callbacks and handler calls are serialized
// through parallelResultCollector to prevent data races on shared counters.
//
// Each worker calls RestackBranchesWithHandler which computes its own engine
// plan against the worktree-local engine; the parent's pre-computed plan in
// restackPlannedGroup.enginePlan is not portable across engines.
func restackGroupsParallel(
	ctx *app.Context,
	opts RestackOptions,
	groups []restackPlannedGroup,
	handler handlers.RestackHandler,
) (restacked, skipped int, conflicts, blocked []string, err error) {
	eng := ctx.Engine

	numJobs := opts.Jobs
	if numJobs <= 0 {
		numJobs = stdruntime.NumCPU()
	}
	if numJobs > len(groups) {
		numJobs = len(groups)
	}

	// Prune stale worktrees once before creating new ones.
	_ = eng.PruneWorktrees(ctx.Context)

	// Snapshot the parent engine state once — each worktree engine gets a copy.
	snapshot := eng.SnapshotForWorktree()

	collector := &parallelResultCollector{eng: eng, handler: handler}

	utils.RunWithWorkers(groups, numJobs, func(group restackPlannedGroup) {
		// Create a temporary worktree for this group. PruneSkip because we
		// pruned once above and don't want N workers racing on prune.
		wtPath, cleanup, err := eng.CreateTemporaryWorktreeSkipPrune(ctx.Context, eng.Trunk().GetName(), "stackit-restack-*")
		if err != nil {
			wrappedErr := fmt.Errorf("stack %s: create worktree: %w", group.rootBranch, err)
			ctx.Logger.Warn("failed to create worktree for parallel restack: %v", wrappedErr)
			collector.recordGroupFailure(group, wrappedErr)
			return
		}
		defer cleanup()

		// Build a per-worktree engine from the snapshot.
		wtEngine, err := newWorktreeEngine(engine.WorktreeEngineOptions{
			WorktreePath: wtPath,
			Snapshot:     snapshot,
		})
		if err != nil {
			wrappedErr := fmt.Errorf("stack %s: create worktree engine: %w", group.rootBranch, err)
			ctx.Logger.Warn("failed to create worktree engine: %v", wrappedErr)
			collector.recordGroupFailure(group, wrappedErr)
			return
		}

		// Shallow-copy the app context, swapping in the worktree engine.
		wtCtx := *ctx
		wtCtx.Engine = wtEngine
		wtCtx.RepoRoot = wtPath

		groupRoot := group.rootBranch
		progress := func(p RestackProgress) {
			p.StackRoot = groupRoot
			collector.recordProgress(p)
		}

		// Parallel mode always reports conflicts via callback: the interactive conflict
		// workflow writes rebase state into the worktree, which defer cleanup() tears
		// down, so entering it would silently destroy what the user needs to resolve.
		if err := RestackBranchesWithHandler(&wtCtx, group.sortedBranches, progress, ConflictModeContinue); err != nil {
			wrappedErr := fmt.Errorf("stack %s: %w", group.rootBranch, err)
			ctx.Logger.Warn("parallel restack group failed: %v", wrappedErr)
			collector.recordRebaseError(wrappedErr)
		}
	})

	// Rebuild the main engine's cache so it sees the ref changes made by the
	// worktree engines (they share the same .git directory).
	if err := eng.Rebuild(eng.Trunk().GetName()); err != nil {
		ctx.Logger.Warn("failed to rebuild engine after parallel restack: %v", err)
	}

	return collector.restacked, collector.skipped, collector.conflicts, collector.blocked, collector.joinedError()
}

// guardUnpushedTrunk refuses to restack when the local trunk has commits that
// are not on its remote-tracking branch (origin/<trunk>). Restacking rebases
// trunk-anchored branches onto the LOCAL trunk tip, so un-pushed/divergent
// trunk commits would be baked into the stack — work that only exists locally
// and may never land as-is. Reconciling trunk with the remote first (sync, or a
// push) keeps restack building only on commits that exist on origin/<trunk>.
//
// It is a no-op when there is no work to do, and for repos with no
// remote-tracking trunk ref (local-only, never fetched, fresh clone), which the
// engine reports via HasRemoteRef=false.
func guardUnpushedTrunk(ctx *app.Context, eng engine.BranchReader, groups []restackBranchGroup) error {
	hasBranches := false
	for _, g := range groups {
		if len(g.branches) > 0 {
			hasBranches = true
			break
		}
	}
	if !hasBranches {
		return nil
	}

	state := eng.TrunkRemoteState(ctx.Context)
	if !state.AheadOrDiverged {
		return nil
	}
	return fmt.Errorf(
		"local trunk %q has commits that are not on %q; restack would build the stack on those un-pushed commits.\n"+
			"Reconcile trunk with the remote first (run `stackit sync`, or push trunk) so restack only builds on %s",
		eng.Trunk().GetName(), state.RemoteRef, state.RemoteRef,
	)
}

type restackBranchGroup struct {
	rootBranch string // independent stack root name (direct child of trunk)
	branches   engine.Branches
}

// planRestackBranchGroups computes the branch groups that RestackAction would
// process for the given options. Extracted so callers can detect "no work"
// before starting an interactive TUI (which would otherwise leak terminal
// codes when the action returns immediately).
func planRestackBranchGroups(eng engine.BranchReader, opts RestackOptions) ([]restackBranchGroup, error) {
	if opts.AllStacks || len(opts.StackRoots) > 0 {
		return branchGroupsForIndependentStacks(eng, opts)
	}
	branch := eng.GetBranch(opts.BranchName)
	graph := eng.Graph(engine.SortStrategyAlphabetical)
	branches := graph.Range(branch, opts.Scope).WithoutTrunk()
	return []restackBranchGroup{{
		branches: branches,
	}}, nil
}

func branchGroupsForIndependentStacks(eng engine.BranchReader, opts RestackOptions) ([]restackBranchGroup, error) {
	stacks := engine.DiscoverIndependentStacks(eng)
	if len(opts.StackRoots) > 0 {
		stackByRoot := make(map[string]engine.IndependentStack, len(stacks))
		for _, stack := range stacks {
			stackByRoot[stack.RootBranch] = stack
		}

		filtered := make([]engine.IndependentStack, 0, len(opts.StackRoots))
		for _, root := range opts.StackRoots {
			stack, ok := stackByRoot[root]
			if !ok {
				return nil, fmt.Errorf("stack root %s not found", root)
			}
			filtered = append(filtered, stack)
		}
		stacks = filtered
	}

	groups := make([]restackBranchGroup, 0, len(stacks))
	for _, stack := range stacks {
		branches := engine.NewBranchesBuilder(len(stack.Branches))
		for _, branch := range engine.BranchesFromNames(eng, stack.Branches) {
			if branch.IsTracked() && !branch.IsTrunk() {
				branches.Add(branch)
			}
		}
		if built := branches.Build(); len(built) > 0 {
			groups = append(groups, restackBranchGroup{
				rootBranch: stack.RootBranch,
				branches:   built,
			})
		}
	}
	return groups, nil
}

func handleRestackProgress(
	eng engine.BranchReader,
	handler handlers.RestackHandler,
	p RestackProgress,
	restacked *int,
	skipped *int,
	conflicts *[]string,
	blocked *[]string,
) {
	res := handlers.RestackDone
	switch p.Result {
	case engine.RestackDone:
		*restacked++
		res = handlers.RestackDone
	case engine.RestackUnneeded:
		res = handlers.RestackUnneeded
	case engine.RestackConflict:
		*skipped++
		*conflicts = append(*conflicts, p.Branch)
		res = handlers.RestackConflict
	case engine.RestackBlocked:
		*blocked = append(*blocked, p.Branch)
		res = handlers.RestackBlocked
	}

	// Determine parent name for consistent output
	parentName := ""
	br := eng.GetBranch(p.Branch)
	if br.GetName() != "" {
		if parent := br.GetParent(); parent != nil {
			parentName = parent.GetName()
		} else {
			parentName = eng.Trunk().GetName()
		}
	}

	// PR number is not always available without extra fetching, but we can try
	var prNumber *int
	if br.GetName() != "" {
		if pr, err := eng.GetPrInfo(br); err == nil && pr != nil {
			num := pr.Number()
			prNumber = num
		}
	}

	handler.OnRestackBranch(p.Branch, res, p.NewRev, prNumber, p.LockReason, p.Frozen, p.IsCurrent, parentName, p.Reparented, p.OldParent, p.NewParent, p.RerereResolvedCount)

	// Enrich JSON handler with stack root info (not part of the interface to avoid
	// touching all implementors for a JSON-only concern).
	if p.StackRoot != "" {
		if jsonHandler, ok := handler.(*handlers.JSONRestackHandler); ok {
			jsonHandler.SetLastBranchStackRoot(p.Branch, p.StackRoot)
		}
	}
}
