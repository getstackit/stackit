// Package merge provides CLI commands for merging stacked PRs.
package merge

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	mergeAction "github.com/getstackit/stackit/internal/actions/merge"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/cli/common"
)

// PostMergeHandler handles post-merge actions like syncing trunk.
// This is injected by the parent package to avoid circular dependencies.
type PostMergeHandler func(ctx *app.Context, action mergeAction.PostMergeAction) error

// NewMergeCmd creates the merge command with subcommands.
// postMergeHandler is called when a post-merge action is required (e.g., syncing trunk).
func NewMergeCmd(postMergeHandler PostMergeHandler) *cobra.Command {
	var (
		dryRun bool
		force  bool
		wait   bool
		yes    bool
		scope  string
		branch string
	)

	cmd := &cobra.Command{
		Use:   "merge",
		Short: "Merge pull requests for a stack",
		Long: `Merge pull requests associated with your stack.

When run without a subcommand, launches an interactive wizard to guide you
through the merge process.

Subcommands:
  status  Show shippability status of your stacks
  next    Merge the next (bottom-most) unmerged PR in the stack
  drain   Merge all PRs bottom-up, waiting for each to complete
  ship    Consolidate all branches into a single PR and merge atomically

Use --scope or --branch to skip the initial prompts and go straight to strategy selection.

Without a TTY, pass --yes to merge the next (bottom-most) ready PR without
prompting (equivalent to 'merge next --yes'); it never consolidates. Use the
'ship'/'drain' subcommands for other strategies.

Examples:
  stackit merge                    # Launch interactive merge wizard
  stackit merge --yes              # Non-interactive: merge the next ready PR
  stackit merge --scope=PROJ-100   # Merge all branches in scope PROJ-100
  stackit merge --branch=feature   # Merge from specific branch
  stackit merge status             # Show your mergeable work
  stackit merge status --all       # Show entire team's mergeable work
  stackit merge next               # Merge bottom PR, restack, stop
  stackit merge drain              # Merge all PRs bottom-up, wait for each
  stackit merge ship               # Consolidate all branches into single PR`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				// The strategy-selection wizard needs a TTY. Non-interactively we
				// can't safely guess between consolidating (ship) and incremental
				// merges, so --yes routes to the conservative default — merge the
				// next (bottom-most) ready PR, equivalent to `merge next`. It never
				// consolidates and re-runs to drain the stack one PR at a time.
				// Other strategies stay explicit via the subcommands.
				if !ctx.Interactive {
					if !yes && !dryRun {
						return fmt.Errorf("merge wizard requires a TTY. For non-interactive use pass --yes to merge the next (bottom-most) PR, or use 'merge next'/'merge drain'/'merge ship' (each accepts --yes)")
					}
					return runMergeNext(ctx, mergeNextOptions{
						dryRun: dryRun,
						yes:    yes,
						force:  force,
						wait:   wait,
						branch: branch,
						scope:  scope,
					}, postMergeHandler)
				}

				// runner.Cleanup is nil-safe so no extra guard is needed.
				runner, handler := NewMergeUI(ctx.Output, ctx.Logger)
				defer runner.Cleanup()

				// NewMergeUI returns an interactive handler when TTY is available
				// (which we've verified above), so this cast should always succeed
				interactiveHandler, ok := handler.(mergeAction.InteractiveHandler)
				if !ok {
					return fmt.Errorf("failed to initialize interactive handler")
				}

				err := mergeAction.RunWizard(ctx, interactiveHandler, mergeAction.WizardOptions{
					DryRun:       dryRun,
					Force:        force,
					Wait:         wait,
					Scope:        scope,
					TargetBranch: branch,
				})

				// Handle post-merge follow-up action
				var postMerge *mergeAction.PostMergeActionRequired
				if errors.As(err, &postMerge) {
					if postMergeHandler != nil {
						return postMergeHandler(ctx, postMerge.Action)
					}
					return nil
				}
				return err
			})
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show merge plan without executing")
	cmd.Flags().BoolVar(&force, "force", false, "Skip validation checks (draft PRs, failing CI)")
	cmd.Flags().BoolVar(&wait, "wait", false, "Wait for merge to complete (default: fire-and-forget)")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Non-interactive: merge the next (bottom-most) ready PR without prompting (equivalent to 'merge next --yes'). Use 'merge ship'/'merge drain' for other strategies.")
	cmd.Flags().StringVar(&scope, "scope", "", "Pre-select scope to merge (skips scope prompt)")
	cmd.Flags().StringVar(&branch, "branch", "", "Pre-select target branch to merge from (skips branch prompt)")
	cmd.MarkFlagsMutuallyExclusive("scope", "branch")

	// Add subcommands
	cmd.AddCommand(NewStatusCmd())
	cmd.AddCommand(NewNextCmd(postMergeHandler))
	cmd.AddCommand(NewShipCmd(postMergeHandler))
	cmd.AddCommand(NewDrainCmd())

	return cmd
}
