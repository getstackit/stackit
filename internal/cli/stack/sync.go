// Package stack provides CLI commands for operating on entire stacks.
package stack

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/actions/sync"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/cli/common"
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
					remoteCtx, cancelRemote := ctx.RemoteOperationContext()
					plan := sync.PlanDryRun(remoteCtx, ctx.Engine, sync.PlanRequest{Restack: restack})
					cancelRemote()
					if jsonOutput {
						return renderSyncDryRunJSON(ctx.Output, plan.Result())
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
func renderSyncDryRunText(out output.Output, plan sync.DryRunPlan) {
	out.Info("🔍 Dry run — no changes will be made.")

	printed := false

	if plan.PullBranch != "" {
		out.Newline()
		out.Info("Would pull from remote:")
		line := "  " + style.ColorBranchName(plan.PullBranch)
		if plan.PullRevision != "" {
			line += " → " + style.ColorDim(plan.PullRevision)
		}
		out.Info("%s", line)
		printed = true
	}

	if len(plan.Clean) > 0 {
		out.Newline()
		out.Info("Would delete (merged or closed PRs):")
		for _, c := range plan.Clean {
			line := "  " + style.ColorBranchName(c.Branch)
			if c.Reason != "" {
				line += " " + style.ColorDim("("+c.Reason+")")
			}
			out.Info("%s", line)
		}
		printed = true
	}

	if len(plan.Restack) > 0 {
		out.Newline()
		out.Info("Would restack:")
		for _, r := range plan.Restack {
			line := "  " + style.ColorBranchName(r.Branch)
			if r.Parent != "" {
				line += " " + style.ColorDim("on "+r.Parent)
			}
			out.Info("%s", line)
		}
		printed = true
	}

	if len(plan.SkippedStacks) > 0 {
		out.Newline()
		out.Info("Skipped (worktree has uncommitted changes):")
		for _, name := range plan.SkippedStacks {
			out.Info("  %s", style.ColorBranchName(name))
		}
		printed = true
	}

	if plan.TrunkStateUnknown {
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
	if !plan.RestackRequested {
		out.Info("%s", style.ColorDim("Restacking is previewed only with --restack."))
	}
	out.Info("Run %s to apply.", style.ColorCyan("stackit sync"))
}

// dryRunSummaryLine renders the one-line "would …" tally that mirrors the live
// sync's "✅ Summary:" line, so a preview and a real run read the same shape.
func dryRunSummaryLine(plan sync.DryRunPlan) string {
	var parts []string
	if plan.PullBranch != "" {
		parts = append(parts, "pull trunk")
	}
	if n := len(plan.Clean); n > 0 {
		parts = append(parts, fmt.Sprintf("delete %d", n))
	}
	if n := len(plan.Restack); n > 0 {
		parts = append(parts, fmt.Sprintf("restack %d", n))
	}
	if n := len(plan.SkippedStacks); n > 0 {
		parts = append(parts, fmt.Sprintf("skip %d %s", n, actions.Pluralize("stack", n)))
	}
	if len(parts) == 0 {
		return ""
	}
	return "would " + strings.Join(parts, ", ")
}
