package actions

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/term"
)

var newWorktreeEngine = engine.NewEngineForWorktree

// RestackDeps are the collaborators the restack command pipeline (PlanRestack,
// RestackAction, RunRestack) needs.
type RestackDeps struct {
	Engine    engine.Engine
	Out       output.Output
	Log       output.Logger
	RepoRoot  string
	Snapshots Snapshots
	// Interactive and Quiet mirror the global flags; together with the
	// terminal they decide whether RestackAction may prompt to enable rerere.
	Interactive bool
	Quiet       bool
}

// RestackOptions contains options for the restack command
type RestackOptions struct {
	BranchName         string
	Scope              engine.StackRange
	AllStacks          bool
	StackRoots         []string
	ContinueOnConflict bool
	Parallel           bool // Run independent stack groups in parallel worktrees
	Jobs               int  // Number of parallel workers (0 = NumCPU)
	// NonInteractive suppresses every prompt and reports conflicts instead of
	// entering the conflict workflow. Set it for machine-readable output: the
	// workflow prints human guidance and leaves the repository mid-rebase.
	NonInteractive bool
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
	return slices.ContainsFunc(p.groups, func(g restackPlannedGroup) bool {
		return len(g.sortedBranches) > 0
	})
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
func PlanRestack(ctx context.Context, deps RestackDeps, opts RestackOptions) (*RestackPlan, error) {
	eng := deps.Engine
	rawGroups, err := planRestackBranchGroups(ctx, eng, opts)
	if err != nil {
		return nil, err
	}
	// Drop dirty-worktree stacks before the guard so a restack that has nothing
	// left to move is a no-op rather than a guard error.
	rawGroups = skipDirtyWorktreeStacks(ctx, deps, rawGroups)
	if err := guardUnpushedTrunk(ctx, eng, rawGroups); err != nil {
		return nil, err
	}
	plan := &RestackPlan{opts: opts}
	for _, g := range rawGroups {
		sorted := eng.SortBranchesTopologically(ctx, g.branches)
		var enginePlan *engine.RestackPlan
		if len(sorted) > 0 {
			enginePlan, err = eng.PlanRestack(ctx, sorted)
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
func RestackAction(ctx context.Context, deps RestackDeps, plan *RestackPlan, handler RestackHandler) error {
	if plan == nil {
		return errors.New("restack plan is required")
	}
	out := deps.Out
	opts := plan.opts

	branchCount := plan.BranchCount()
	if branchCount == 0 {
		out.Info("No branches to restack.")
		return nil
	}

	deps.Log.Info("restack started branchCount=%v", branchCount)

	// Restack is repository reconciliation, like sync: it may run from any
	// worktree, but the engine holds branches whose checked-out worktree is
	// dirty or cannot be inspected. Content-editing actions keep the ownership
	// guard; applying it here would make unrelated main-repo stacks an
	// all-or-nothing casualty of one managed worktree.
	parallelDispatch := opts.Parallel && len(plan.groups) > 1

	// Take snapshot before modifying the repository. Skip it when no branch
	// actually needs a rebase: an up-to-date restack mutates nothing, so there
	// is nothing to undo and the snapshot is pure overhead (it hits every no-op
	// restack, and sync runs restack often).
	//
	// The sequential path can skip safely because HasWork() is the same
	// predicate restackBranchesWithPlan early-returns on, so a false verdict
	// provably means no mutation. Parallel dispatch has no such guarantee, and
	// an undo snapshot is exactly what you want when that reasoning is wrong.
	if plan.HasWork() || parallelDispatch {
		snapshotOpts := NewSnapshot("restack",
			WithArg(opts.BranchName),
			WithFlag(opts.AllStacks, "--all-stacks"),
			WithFlagValue("--stacks", strings.Join(opts.StackRoots, ",")),
			WithFlag(opts.ContinueOnConflict, "--continue-on-conflict"),
			WithFlag(opts.Parallel, "--parallel"),
		)
		deps.Snapshots.Take(ctx, snapshotOpts)
	}

	// A routine conflict under NonInteractive is data, not an error: report it
	// like --continue-on-conflict rather than entering the workflow.
	policy := RestackConflictEnterWorkflow
	prompter, _ := handler.(RestackConflictPrompter)
	switch {
	case opts.ContinueOnConflict || opts.NonInteractive:
		policy = RestackConflictReport
	case prompter != nil && prompter.IsInteractive():
		policy = RestackConflictPrompt
	}

	_, err := RunRestack(ctx, deps, RestackRequest{
		groups:         plan.groups,
		ConflictPolicy: policy,
		Prompter:       prompter,
		PromptRerere:   deps.Interactive && !deps.Quiet && term.IsInteractive() && !opts.NonInteractive,
		Parallel:       parallelDispatch,
		Jobs:           opts.Jobs,
	}, handler)
	if err != nil {
		return fmt.Errorf("restack failed: %w", err)
	}
	return nil
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
func guardUnpushedTrunk(ctx context.Context, eng engine.BranchReader, groups []restackBranchGroup) error {
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

	state := eng.TrunkRemoteState(ctx)
	if !state.AheadOrDiverged {
		return nil
	}
	return fmt.Errorf(
		"local trunk %q has commits that are not on %q; restack would build the stack on those un-pushed commits.\n"+
			"Reconcile trunk with the remote first (run `stackit sync`, or push trunk) so restack only builds on %s",
		eng.Trunk().Name(), state.RemoteRef, state.RemoteRef,
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
func planRestackBranchGroups(ctx context.Context, eng engine.BranchReader, opts RestackOptions) ([]restackBranchGroup, error) {
	if opts.AllStacks || len(opts.StackRoots) > 0 {
		return branchGroupsForIndependentStacks(ctx, eng, opts)
	}
	// A typo'd --branch would otherwise produce an empty range and a silent
	// "No branches to restack." success.
	if !eng.BranchNames().Contains(opts.BranchName) {
		return nil, fmt.Errorf("branch %s does not exist", opts.BranchName)
	}
	branch := eng.GetBranch(opts.BranchName)
	graph := eng.Graph(ctx, engine.SortStrategyAlphabetical)
	branches := graph.Range(branch, opts.Scope).WithoutTrunk()
	return []restackBranchGroup{{
		branches: branches,
	}}, nil
}

// skipDirtyWorktreeStacks drops branches that cannot be restacked without
// leaving a worktree out of sync with its own branch ref, warning once for each.
//
// Moving a branch's ref is only half the operation: the worktree holding that
// branch has to be reset to match. That reset is suppressed while the worktree
// has uncommitted tracked changes, because it would discard them — which leaves
// the worktree on the old commit's content under a moved ref. `git status` then
// reports the new commit's files as deleted, and the next `stackit modify -a`
// commits that deletion into the stack. Not restacking at all is the safe
// outcome, and it self-heals once the user commits or stashes.
//
// Two cases, because the unit that must be held back differs:
//
//   - A dirty *managed* worktree holds a whole stack, and its anchor is that
//     stack's root, so the entire stack is dropped. This also matches what sync
//     has always done (internal/actions/sync/sync.go).
//   - Any other worktree — including the main one, which `stackit restack` is
//     normally run from — holds a single branch, so only that branch is dropped.
//     Its descendants stay: they are still correctly based on a branch that did
//     not move, so restacking them is a no-op rather than a hazard.
func skipDirtyWorktreeStacks(ctx context.Context, deps RestackDeps, groups []restackBranchGroup) []restackBranchGroup {
	report := CollectRestackWorktreeHolds(ctx, deps.Engine)
	for _, message := range report.Messages() {
		deps.Out.Warn("%s", message)
	}

	kept := make([]restackBranchGroup, 0, len(groups))
	for _, g := range groups {
		if built := report.Filter(deps.Engine, g.branches); len(built) > 0 {
			kept = append(kept, restackBranchGroup{rootBranch: g.rootBranch, branches: built})
		}
	}
	return kept
}

func branchGroupsForIndependentStacks(ctx context.Context, eng engine.BranchReader, opts RestackOptions) ([]restackBranchGroup, error) {
	stacks := engine.DiscoverIndependentStacks(ctx, eng)
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
