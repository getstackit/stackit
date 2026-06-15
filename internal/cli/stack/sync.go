// Package stack provides CLI commands for operating on entire stacks.
package stack

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/actions/sync"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/cli/common"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/tui/style"
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
					result := computeSyncDryRun(ctx, opts)
					if jsonOutput {
						return renderSyncDryRunJSON(ctx.Output, result)
					}
					renderSyncDryRunText(ctx.Output, result, restack)
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
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview metadata changes without applying them")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format (requires --dry-run)")

	return cmd
}

// computeSyncDryRun builds a read-only snapshot of what a sync WOULD do from the
// current (remote-aware) state. It never mutates: it probes remote status and
// reads PR/deletion state, but performs no fetch-and-merge, deletion, restack,
// or GitHub write. It intentionally reimplements a slice of sync.Action's
// planning rather than running the action with a special handler, because a
// dry-run is a query, not a simulation of the full interactive process.
func computeSyncDryRun(ctx *app.Context, opts sync.Options) sync.DryRunResult {
	eng := ctx.Engine

	result := sync.DryRunResult{
		WouldClean:   []string{},
		WouldRestack: []string{},
	}

	// Check if trunk needs to be pulled from remote
	trunk := eng.Trunk()
	remoteStatus := eng.ReadBranchRemoteStatuses(ctx.Context, engine.BranchesOf(trunk)).ForBranch(trunk)
	if remoteStatus.Behind() {
		result.WouldPull = trunk.GetName()
	}

	// Collect candidate branches for deletion and restack checks
	allBranches := eng.AllBranches()
	var candidateNames []string
	restackRootSet := make(map[string]struct{})
	for _, branch := range allBranches {
		if branch.IsTrunk() || !branch.IsTracked() {
			continue
		}
		candidateNames = append(candidateNames, branch.GetName())

		// Check restack status while iterating
		if opts.Restack && !branch.IsBranchUpToDate() {
			result.WouldRestack = append(result.WouldRestack, branch.GetName())
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
		result.WouldRestackStacks = roots
	}

	// Batch-check deletion status for all candidates
	if len(candidateNames) > 0 {
		statuses, err := eng.GetDeletionStatuses(ctx.Context, candidateNames)
		if err == nil {
			for _, name := range candidateNames {
				if status, ok := statuses[name]; ok && status.SafeToDelete {
					result.WouldClean = append(result.WouldClean, name)
				}
			}
		}
	}

	// Check for dirty worktrees
	managedWorktrees, err := eng.ListManagedWorktrees()
	if err == nil {
		for _, wt := range managedWorktrees {
			if hasChanges, _ := eng.WorktreeHasUncommittedChanges(ctx.Context, wt.Path); hasChanges {
				result.SkippedStacks = append(result.SkippedStacks, wt.AnchorBranch)
			}
		}
	}

	return result
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

// renderSyncDryRunText prints a human-readable preview of what a sync would do.
// restackRequested mirrors the --restack flag so we can hint that restacking is
// only previewed when explicitly requested (WouldRestack is empty otherwise).
func renderSyncDryRunText(out output.Output, result sync.DryRunResult, restackRequested bool) {
	out.Info("🔍 Dry run — no changes will be made.")

	printed := false
	section := func(title string, items []string) {
		if len(items) == 0 {
			return
		}
		out.Newline()
		out.Info("%s", title)
		for _, name := range items {
			out.Info("  %s", style.ColorBranchName(name, false))
		}
		printed = true
	}

	if result.WouldPull != "" {
		out.Newline()
		out.Info("Would pull from remote:")
		out.Info("  %s", style.ColorBranchName(result.WouldPull, false))
		printed = true
	}
	section("Would delete (merged or closed PRs):", result.WouldClean)
	section("Would restack:", result.WouldRestack)
	section("Skipped (worktree has uncommitted changes):", result.SkippedStacks)

	if !printed {
		out.Newline()
		out.Info("Everything is up to date — sync would make no changes.")
		return
	}

	out.Newline()
	if !restackRequested {
		out.Info("%s", style.ColorDim("Restacking is previewed only with --restack."))
	}
	out.Info("Run %s to apply.", style.ColorCyan("stackit sync"))
}
