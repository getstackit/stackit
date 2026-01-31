// Package merge provides CLI commands for merging stacked PRs.
package merge

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	mergeAction "stackit.dev/stackit/internal/actions/merge"
	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/cli/common"
	"stackit.dev/stackit/internal/shippable"
)

// PostMergeHandler handles post-merge actions like syncing trunk.
// This is injected by the parent package to avoid circular dependencies.
type PostMergeHandler func(ctx *app.Context, action mergeAction.PostMergeAction) error

// NewMergeCmd creates the merge command with subcommands.
// postMergeHandler is called when a post-merge action is required (e.g., syncing trunk).
func NewMergeCmd(postMergeHandler PostMergeHandler) *cobra.Command {
	var (
		dryRun  bool
		force   bool
		wait    bool
		showAll bool
	)

	cmd := &cobra.Command{
		Use:   "merge",
		Short: "Merge pull requests for a stack",
		Long: `Merge pull requests associated with your stack.

When run without a subcommand, shows your mergeable work status and guides you
through the merge process with an interactive wizard.

By default, shows only your own stacks. Use --all to see the entire team's work.

Subcommands:
  next    Merge the next (bottom-most) unmerged PR in the stack
  squash  Consolidate all branches into a single PR and merge atomically

Examples:
  stackit merge             # Show your mergeable work, then wizard
  stackit merge --all       # Show entire team's mergeable work
  stackit merge next        # Merge bottom PR, restack, stop
  stackit merge squash      # Consolidate all branches into single PR`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				// Must be interactive for wizard mode
				if !ctx.Interactive {
					return fmt.Errorf("merge wizard requires a TTY. Use 'merge next' or 'merge squash' for non-interactive mode")
				}

				// Fetch and display shippability status before wizard
				analyzer := shippable.NewAnalyzer(ctx.Engine, ctx.GitHubClient)
				analysisResult, err := analyzer.AnalyzeAll(ctx.Context)
				if err != nil {
					ctx.Output.Debug("failed to analyze shippable stacks: %v", err)
					// Continue without status display
				} else {
					// Filter by current user unless --all is specified
					if !showAll && ctx.GitHubClient != nil {
						currentUser, userErr := ctx.GitHubClient.GetCurrentUser(ctx.Context)
						if userErr == nil && currentUser != "" {
							analysisResult = analysisResult.FilterByAuthor(currentUser)
						}
					}

					DisplayMergeStatus(ctx.Output, analysisResult)
				}

				runner, handler := NewMergeUI(ctx.Output, ctx.Logger)
				if runner != nil {
					defer runner.Cleanup()
				}

				// NewMergeUI returns an interactive handler when TTY is available
				// (which we've verified above), so this cast should always succeed
				interactiveHandler, ok := handler.(mergeAction.InteractiveHandler)
				if !ok {
					return fmt.Errorf("failed to initialize interactive handler")
				}

				err = mergeAction.RunWizard(ctx, interactiveHandler, mergeAction.WizardOptions{
					DryRun: dryRun,
					Force:  force,
					Wait:   wait,
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
	cmd.Flags().BoolVar(&showAll, "all", false, "Show all team members' stacks (default: your stacks only)")

	// Add subcommands
	cmd.AddCommand(NewNextCmd(postMergeHandler))
	cmd.AddCommand(NewSquashCmd(postMergeHandler))

	return cmd
}
