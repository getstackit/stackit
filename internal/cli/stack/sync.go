// Package stack provides CLI commands for operating on entire stacks.
package stack

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"stackit.dev/stackit/internal/actions/sync"
	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/cli/common"
)

// NewSyncCmd creates the sync command
func NewSyncCmd() *cobra.Command {
	var (
		all        bool
		force      bool
		restack    bool
		dryRun     bool
		jsonOutput bool
	)

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync all branches with remote",
		Long: `Sync all branches with remote, prompting to delete any branches for PRs that have been merged or closed.
Restacks all branches in your repository that can be restacked without conflicts.
If trunk cannot be fast-forwarded to match remote, overwrites trunk with the remote version.`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Validate --json requires --dry-run
			if jsonOutput && !dryRun {
				return fmt.Errorf("--json requires --dry-run")
			}

			return common.Run(cmd, func(ctx *app.Context) error {
				// JSON output for dry-run mode
				if jsonOutput && dryRun {
					return syncDryRunJSON(ctx, sync.Options{
						All:     all,
						Force:   force,
						Restack: restack,
						DryRun:  true,
					})
				}

				// Create runner (manages terminal state) and handler (processes events)
				runner, handler := NewSyncUI(ctx.Output, ctx.Logger)
				defer runner.Cleanup()

				// Run sync action with handler
				return sync.Action(ctx, sync.Options{
					All:     all,
					Force:   force,
					Restack: restack,
					DryRun:  dryRun,
				}, handler)
			})
		},
	}

	var noRestack bool

	cmd.Flags().BoolVarP(&all, "all", "a", false, "Sync branches across all configured trunks")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Don't prompt for confirmation before overwriting or deleting a branch")
	cmd.Flags().BoolVar(&restack, "restack", true, "Restack any branches that can be restacked without conflicts")
	cmd.Flags().BoolVar(&noRestack, "no-restack", false, "Skip restacking branches")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview metadata changes without applying them")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format (requires --dry-run)")

	// Apply --no-restack flag
	cmd.PreRun = func(_ *cobra.Command, _ []string) {
		if noRestack {
			restack = false
		}
	}

	return cmd
}

// syncDryRunJSON generates JSON output for sync --dry-run.
//
// This function intentionally duplicates some logic from sync.Action rather than
// calling the action with a special handler. This is because:
//  1. sync.Action has complex interactive behavior (prompts, confirmations) that
//     doesn't make sense for a pure dry-run query
//  2. The dry-run JSON output needs to be a simple snapshot of current state,
//     not a simulation of the full sync process
//  3. Keeping this separate makes it easier to extend the JSON output without
//     affecting the main sync action's behavior
func syncDryRunJSON(ctx *app.Context, opts sync.Options) error {
	eng := ctx.Engine

	result := sync.DryRunResult{
		WouldClean:   []string{},
		WouldRestack: []string{},
	}

	// Check if trunk needs to be pulled from remote
	trunk := eng.Trunk()
	remoteStatus, err := eng.GetBranchRemoteStatus(trunk)
	if err == nil && remoteStatus.Behind() {
		result.WouldPull = trunk.GetName()
	}

	// Collect candidate branches for deletion and restack checks
	allBranches := eng.AllBranches()
	var candidateNames []string
	for _, branch := range allBranches {
		if branch.IsTrunk() || !branch.IsTracked() {
			continue
		}
		candidateNames = append(candidateNames, branch.GetName())

		// Check restack status while iterating
		if opts.Restack && !branch.IsBranchUpToDate() {
			result.WouldRestack = append(result.WouldRestack, branch.GetName())
		}
	}

	// Batch-check deletion status for all candidates
	if len(candidateNames) > 0 {
		statuses, err := eng.BatchGetDeletionStatuses(ctx.Context, candidateNames)
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
			if hasChanges, _ := eng.Git().WorktreeHasUncommittedChanges(ctx.Context, wt.Path); hasChanges {
				result.SkippedStacks = append(result.SkippedStacks, wt.AnchorBranch)
			}
		}
	}

	// Output JSON
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}
	ctx.Output.Info("%s", string(data))

	return nil
}
