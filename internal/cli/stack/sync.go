// Package stack provides CLI commands for operating on entire stacks.
package stack

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/actions/sync"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/cli/common"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/tui/style"
	"github.com/getstackit/stackit/internal/utils"
)

// NewSyncCmd creates the sync command
func NewSyncCmd() *cobra.Command {
	var (
		all        bool
		force      bool
		restack    bool
		noRestack  bool
		dryRun     bool
		jsonOutput bool
	)

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync all branches with remote",
		Long: `Sync all branches with remote, prompting to delete any branches for PRs that have been merged or closed.
Restacks branches that were reparented during sync. Use --restack to restack all branches in the current stack.
If trunk cannot be fast-forwarded to match remote, overwrites trunk with the remote version.`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Validate --json requires --dry-run
			if jsonOutput && !dryRun {
				return fmt.Errorf("--json requires --dry-run")
			}

			return common.Run(cmd, func(ctx *app.Context) error {
				opts := sync.Options{
					All:       all,
					Force:     force,
					Restack:   restack,
					NoRestack: noRestack,
					DryRun:    dryRun,
				}

				// --dry-run is a read-only PREVIEW. Compute the plan from the
				// current (remote-aware) state and render it without ever calling
				// sync.Action, so nothing is fast-forwarded, deleted, restacked,
				// or pushed to GitHub. This is the whole contract of --dry-run.
				if dryRun {
					remoteCtx, cancelRemote := ctx.RemoteOperationContext()
					plan := computeSyncDryRun(remoteCtx, ctx.Engine, opts)
					cancelRemote()
					if jsonOutput {
						return renderSyncDryRunJSON(ctx.Output, plan.toResult())
					}
					renderSyncDryRunText(ctx.Output, plan)
					return nil
				}

				// Check for uncommitted changes BEFORE starting TUI to avoid
				// terminal control codes leaking on early error exit
				if ctx.Reader().HasUncommittedChanges(ctx.Context) && !ctx.InManagedWorktree {
					return fmt.Errorf("you have uncommitted changes. Please commit or stash them before syncing")
				}

				// Create runner (manages terminal state) and handler (processes events)
				runner, handler := NewSyncUI(ctx.Output, ctx.Logger)
				defer runner.Cleanup()

				// Run sync action with handler
				return sync.Action(ctx, opts, handler)
			})
		},
	}

	cmd.Flags().BoolVarP(&all, "all", "a", false, "Sync branches across all configured trunks")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Don't prompt for confirmation before overwriting or deleting a branch")
	cmd.Flags().BoolVar(&restack, "restack", false, "Restack all branches in the current stack")
	cmd.Flags().BoolVar(&noRestack, "no-restack", false, "Skip restacking branches entirely")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview what sync would do without making any changes")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format (requires --dry-run)")

	return cmd
}

// dryRunPlan is the read-only snapshot of what a sync WOULD do, built from the
// current (remote-aware) state. It carries enough detail to render a preview
// that mirrors the live run — target revision, deletion reasons, restack
// parents. The JSON contract is a stable, names-only projection (see toResult);
// the richer fields here exist only to make the human-readable preview as
// informative as the real sync.
type dryRunPlan struct {
	pullBranch    string              // trunk that would be pulled (empty if up to date)
	pullRev       string              // short remote SHA the trunk would move to
	clean         []dryRunCleanItem   // branches that would be deleted
	restack       []dryRunRestackItem // branches that would be restacked
	restackStacks []string            // deduped stack roots covering the restack set (JSON only)
	skipped       []string            // dirty-worktree anchors whose stacks are skipped
	restacked     bool                // whether --restack was requested
	trunkUnknown  bool                // trunk's remote relationship could not be resolved
}

type dryRunCleanItem struct {
	branch string
	reason string // e.g. "merged into main", "closed on GitHub"
}

type dryRunRestackItem struct {
	branch string
	parent string
}

// toResult projects the plan onto the stable JSON contract (branch names only).
// The shape is byte-compatible with what scripts already consume, so the richer
// preview fields are deliberately dropped here.
func (p dryRunPlan) toResult() sync.DryRunResult {
	result := sync.DryRunResult{
		WouldPull:          p.pullBranch,
		WouldClean:         []string{},
		WouldRestack:       []string{},
		WouldRestackStacks: p.restackStacks,
		SkippedStacks:      p.skipped,
		TrunkStateUnknown:  p.trunkUnknown,
	}
	for _, c := range p.clean {
		result.WouldClean = append(result.WouldClean, c.branch)
	}
	for _, r := range p.restack {
		result.WouldRestack = append(result.WouldRestack, r.branch)
	}
	return result
}

// computeSyncDryRun builds a read-only snapshot of what a sync WOULD do from the
// current (remote-aware) state. It never mutates: it probes remote status and
// reads PR/deletion state, but performs no fetch-and-merge, deletion, restack,
// or GitHub write. It intentionally reimplements a slice of sync.Action's
// planning rather than running the action with a special handler, because a
// dry-run is a query, not a simulation of the full interactive process. It takes
// the bounded context and engine directly (not the full app context) so every
// remote-status/deletion read shares one deadline and the unbounded command
// context is not reachable here by mistake.
func computeSyncDryRun(ctx context.Context, eng engine.Engine, opts sync.Options) dryRunPlan {
	plan := dryRunPlan{restacked: opts.Restack}

	// Check if trunk needs to be pulled from remote
	trunk := eng.Trunk()
	remoteStatus := eng.ReadBranchRemoteStatuses(ctx, engine.BranchesOf(trunk)).ForBranch(trunk)
	switch {
	case remoteStatus.Unknown():
		// Report the uncertainty rather than the absence of work: the remote
		// SHA is known but its objects are not local, so Behind() is false for
		// want of a merge base, not because trunk is current.
		plan.trunkUnknown = true
	case remoteStatus.Behind():
		plan.pullBranch = trunk.GetName()
		plan.pullRev = utils.ShortRevision(remoteStatus.RemoteSha, 0)
	}

	// Collect candidate branches for deletion and restack checks
	allBranches := eng.AllBranches()
	var candidateNames []string
	restackRootSet := make(map[string]struct{})

	// Resolve up-to-date status for every branch in one batched parent-revision
	// read instead of a per-branch IsBranchUpToDate() inside the loop (each of
	// which would shell a separate `git rev-parse` for the parent).
	var statuses engine.BranchStatuses
	if opts.Restack {
		statuses = eng.ReadBranchStatuses(allBranches)
	}

	for _, branch := range allBranches {
		if branch.IsTrunk() || !branch.IsTracked() {
			continue
		}
		candidateNames = append(candidateNames, branch.GetName())

		// Check restack status while iterating
		if opts.Restack && !statuses.IsUpToDate(branch) {
			plan.restack = append(plan.restack, dryRunRestackItem{
				branch: branch.GetName(),
				parent: branch.GetParentOrTrunk(),
			})
			if root := eng.GetStackRootForBranch(branch); root != "" {
				restackRootSet[root] = struct{}{}
			}
		}
	}

	// Sort and dedupe restack roots for the current dry-run snapshot. Recompute after
	// running sync before using these roots for a follow-up `restack --stacks`, since
	// cleanup and reparenting can change which roots need work.
	if len(restackRootSet) > 0 {
		roots := make([]string, 0, len(restackRootSet))
		for root := range restackRootSet {
			roots = append(roots, root)
		}
		sort.Strings(roots)
		plan.restackStacks = roots
	}

	// Batch-check deletion status for all candidates
	if len(candidateNames) > 0 {
		statuses, err := eng.GetDeletionStatuses(ctx, candidateNames)
		if err == nil {
			for _, name := range candidateNames {
				if status, ok := statuses[name]; ok && status.SafeToDelete {
					plan.clean = append(plan.clean, dryRunCleanItem{branch: name, reason: status.Reason})
				}
			}
		}
	}

	// Same decision sync itself makes, so the preview cannot disagree with the run.
	managedWorktrees, err := eng.ListManagedWorktrees()
	if err == nil {
		for _, wt := range managedWorktrees {
			if sync.SkipReasonForWorktree(ctx, eng, wt.Path.String()) != "" {
				plan.skipped = append(plan.skipped, wt.AnchorBranch)
			}
		}
	}

	return plan
}

// renderSyncDryRunJSON prints the dry-run snapshot as indented JSON. The shape
// is a stable contract for scripting, so keep it byte-compatible.
func renderSyncDryRunJSON(out output.Output, result sync.DryRunResult) error {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}
	out.Info("%s", string(data))
	return nil
}

// renderSyncDryRunText prints a human-readable preview of what a sync would do,
// mirroring the live run's shape: grouped sections, then a one-line summary, then
// the command to apply it. Secondary detail (target revision, deletion reason,
// restack parent) is dimmed so branch names stay scannable.
func renderSyncDryRunText(out output.Output, plan dryRunPlan) {
	out.Info("🔍 Dry run — no changes will be made.")

	printed := false

	if plan.pullBranch != "" {
		out.Newline()
		out.Info("Would pull from remote:")
		line := "  " + style.ColorBranchName(plan.pullBranch)
		if plan.pullRev != "" {
			line += " → " + style.ColorDim(plan.pullRev)
		}
		out.Info("%s", line)
		printed = true
	}

	if len(plan.clean) > 0 {
		out.Newline()
		out.Info("Would delete (merged or closed PRs):")
		for _, c := range plan.clean {
			line := "  " + style.ColorBranchName(c.branch)
			if c.reason != "" {
				line += " " + style.ColorDim("("+c.reason+")")
			}
			out.Info("%s", line)
		}
		printed = true
	}

	if len(plan.restack) > 0 {
		out.Newline()
		out.Info("Would restack:")
		for _, r := range plan.restack {
			line := "  " + style.ColorBranchName(r.branch)
			if r.parent != "" {
				line += " " + style.ColorDim("on "+r.parent)
			}
			out.Info("%s", line)
		}
		printed = true
	}

	if len(plan.skipped) > 0 {
		out.Newline()
		out.Info("Skipped (worktree has uncommitted changes):")
		for _, name := range plan.skipped {
			out.Info("  %s", style.ColorBranchName(name))
		}
		printed = true
	}

	if plan.trunkUnknown {
		out.Newline()
		out.Warn("Could not determine whether trunk is behind its remote; fetch and retry.")
		printed = true
	}

	if !printed {
		out.Newline()
		out.Info("Everything is up to date — sync would make no changes.")
		return
	}

	out.Newline()
	if summary := dryRunSummaryLine(plan); summary != "" {
		out.Info("Summary: %s", summary)
	}
	if !plan.restacked {
		out.Info("%s", style.ColorDim("Restacking is previewed only with --restack."))
	}
	out.Info("Run %s to apply.", style.ColorCyan("stackit sync"))
}

// dryRunSummaryLine renders the one-line "would …" tally that mirrors the live
// sync's "✅ Summary:" line, so a preview and a real run read the same shape.
func dryRunSummaryLine(plan dryRunPlan) string {
	var parts []string
	if plan.pullBranch != "" {
		parts = append(parts, "pull trunk")
	}
	if n := len(plan.clean); n > 0 {
		parts = append(parts, fmt.Sprintf("delete %d", n))
	}
	if n := len(plan.restack); n > 0 {
		parts = append(parts, fmt.Sprintf("restack %d", n))
	}
	if n := len(plan.skipped); n > 0 {
		parts = append(parts, fmt.Sprintf("skip %d %s", n, actions.Pluralize("stack", n)))
	}
	if len(parts) == 0 {
		return ""
	}
	return "would " + strings.Join(parts, ", ")
}
