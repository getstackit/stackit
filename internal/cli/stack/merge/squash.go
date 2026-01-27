// Package merge provides CLI commands for merging stacked PRs.
package merge

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	mergeAction "stackit.dev/stackit/internal/actions/merge"
	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/cli/common"
	"stackit.dev/stackit/internal/config"
	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/github"
	"stackit.dev/stackit/internal/tui"
)

// NewSquashCmd creates the merge squash subcommand.
// This command consolidates all branches in the stack into a single PR and merges it atomically.
// It uses GitHub automerge by default and waits for the merge to complete.
func NewSquashCmd(postMergeHandler PostMergeHandler) *cobra.Command {
	var (
		dryRun      bool
		yes         bool
		force       bool
		wait        bool
		scope       string
		stacks      []string
		skipLocalCI bool
	)

	cmd := &cobra.Command{
		Use:   "squash",
		Short: "Consolidate all stack branches into a single PR and merge atomically",
		Long: `Consolidate all branches in the stack into a single PR for atomic merging.

This creates a new "consolidation" branch that contains all the commits from the stack,
opens a PR for it, and enables GitHub's automerge.

By default, the command returns immediately after enabling automerge (fire-and-forget).
Use --wait to block until the PR is merged, then automatically:
1. Pull the latest trunk
2. Delete the merged branches
3. Restack any remaining upstack branches

Use --scope to consolidate all branches within a specific scope.
Use --stacks to combine multiple independent stacks into a single PR.`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				// Handle multi-stack mode
				if len(stacks) > 0 || cmd.Flags().Changed("stacks") {
					return runMultiStackSquash(ctx, squashMultiStackOptions{
						stacks:      stacks,
						skipLocalCI: skipLocalCI,
						wait:        wait,
						yes:         yes,
					})
				}

				return runMergeSquash(ctx, mergeSquashOptions{
					dryRun: dryRun,
					yes:    yes,
					force:  force,
					wait:   wait,
					scope:  scope,
				}, postMergeHandler)
			})
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show merge plan without executing")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
	cmd.Flags().BoolVar(&force, "force", false, "Skip validation checks (draft PRs, failing CI)")
	cmd.Flags().BoolVar(&wait, "wait", false, "Wait for merge to complete (default: fire-and-forget)")
	cmd.Flags().StringVar(&scope, "scope", "", "Consolidate all branches within the specified scope")
	cmd.Flags().StringSliceVar(&stacks, "stacks", nil, "Combine multiple stacks (comma-separated stack roots)")
	cmd.Flags().BoolVar(&skipLocalCI, "skip-local-ci", false, "Skip local CI validation for multi-stack merge")

	return cmd
}

type mergeSquashOptions struct {
	dryRun bool
	yes    bool
	force  bool
	wait   bool
	scope  string
}

type squashMultiStackOptions struct {
	stacks      []string
	skipLocalCI bool
	wait        bool
	yes         bool
}

func runMergeSquash(ctx *app.Context, opts mergeSquashOptions, postMergeHandler PostMergeHandler) error {
	out := ctx.Output

	// Create consolidation plan
	plan, validation, err := mergeAction.CreateMergePlan(ctx.Context, ctx.Engine, ctx.Output, ctx.GitHubClient, mergeAction.CreatePlanOptions{
		Strategy: mergeAction.StrategySquash,
		Force:    opts.force,
		Scope:    opts.scope,
		Wait:     opts.wait,
	})
	if err != nil {
		return err
	}

	// Show plan
	planText := mergeAction.FormatMergePlan(plan, validation)
	out.Print(planText)

	// Validate
	if !validation.Valid && !opts.force {
		return formatSquashValidationError(validation.Errors, validation.Warnings)
	}

	// Dry run - just show the plan
	if opts.dryRun {
		out.Info("Dry-run mode: No changes were made.")
		return nil
	}

	// Check if individual merge is possible and user wants it
	if !opts.yes && ctx.Interactive && len(plan.BranchesToMerge) > 0 {
		// Build graph once and reuse for both leaf check and merge eligibility
		graph := engine.BuildStackGraph(ctx.Engine, engine.SortStrategyAlphabetical, nil)
		merged, err := tryIndividualMerge(ctx, opts, plan, graph, postMergeHandler)
		if err != nil {
			return err
		}
		if merged {
			return nil
		}
	}

	// Confirm unless --yes
	if !opts.yes && ctx.Interactive {
		confirmed, err := tui.PromptConfirm("Proceed with squash merge?", false)
		if err != nil {
			return fmt.Errorf("confirmation canceled: %w", err)
		}
		if !confirmed {
			out.Info("Merge canceled")
			return nil
		}
	}

	// Get config values
	cfg, _ := config.LoadConfig(ctx.RepoRoot)
	undoStackDepth := cfg.UndoStackDepth()

	// Create handler for progress reporting
	runner, eventHandler := NewMergeUI(ctx.Output, ctx.Logger)
	if runner != nil {
		defer runner.Cleanup()
	}

	// Execute consolidation merge using automerge
	actionOpts := mergeAction.Options{
		DryRun:         false,
		Confirm:        false, // Already confirmed
		Strategy:       mergeAction.StrategySquash,
		Force:          opts.force,
		Wait:           opts.wait,
		Scope:          opts.scope,
		Plan:           plan,
		UndoStackDepth: undoStackDepth,
		Handler:        mergeAction.NewLegacyHandlerAdapter(eventHandler),
	}

	if err := mergeAction.Action(ctx, actionOpts); err != nil {
		return err
	}

	// If waiting, use post-merge handler for cleanup
	if opts.wait && postMergeHandler != nil {
		return postMergeHandler(ctx, mergeAction.PostMergeSyncTrunk)
	}

	return nil
}

// tryIndividualMerge checks if all branches can be merged individually (all leaves,
// all mergeable) and prompts the user to choose between individual and consolidation.
// Returns (true, nil) if individual merge was executed, (false, nil) to continue with
// consolidation, or (false, err) if an error occurred.
//
// The graph parameter is passed to avoid rebuilding it (the caller already has one).
func tryIndividualMerge(ctx *app.Context, opts mergeSquashOptions, plan *mergeAction.Plan, graph *engine.StackGraph, postMergeHandler PostMergeHandler) (bool, error) {
	out := ctx.Output

	if !mergeAction.AllBranchesAreLeaves(graph, plan.BranchesToMerge) {
		return false, nil // Not all leaves - continue with consolidation
	}

	status, err := mergeAction.CanMergeIndividually(ctx.Context, ctx.Engine.Git(), ctx.GitHubClient, graph, plan.BranchesToMerge)
	if err != nil {
		out.Debug("Failed to check individual merge possibility: %v", err)
		return false, nil // Fall back to consolidation
	}
	if !status.CanMerge {
		return false, nil // Cannot merge individually - continue with consolidation
	}

	// Create a TUI handler for the prompt
	runner, eventHandler := NewMergeUI(ctx.Output, ctx.Logger)
	if runner != nil {
		defer runner.Cleanup()
	}

	interactiveHandler, ok := eventHandler.(mergeAction.InteractiveHandler)
	if !ok || !interactiveHandler.IsInteractive() {
		return false, nil // Not interactive - continue with consolidation
	}

	useIndividual, err := interactiveHandler.PromptIndividualMerge(plan.BranchesToMerge)
	if err != nil {
		return false, fmt.Errorf("selection canceled: %w", err)
	}
	if !useIndividual {
		return false, nil // User chose consolidation
	}

	// Switch to bottom-up strategy
	out.Info("Switching to individual merge mode...")
	plan, _, err = mergeAction.CreateMergePlan(ctx.Context, ctx.Engine, ctx.Output, ctx.GitHubClient, mergeAction.CreatePlanOptions{
		Strategy: mergeAction.StrategyBottomUp,
		Force:    opts.force,
		Scope:    opts.scope,
		Wait:     true, // Individual merge always waits
	})
	if err != nil {
		return false, err
	}

	// Execute bottom-up merge
	cfg, _ := config.LoadConfig(ctx.RepoRoot)
	actionOpts := mergeAction.Options{
		DryRun:         false,
		Confirm:        false,
		Strategy:       mergeAction.StrategyBottomUp,
		Force:          opts.force,
		Wait:           true,
		Scope:          opts.scope,
		Plan:           plan,
		UndoStackDepth: cfg.UndoStackDepth(),
		Handler:        mergeAction.NewLegacyHandlerAdapter(eventHandler),
	}

	if err := mergeAction.Action(ctx, actionOpts); err != nil {
		return false, err
	}

	// Post-merge cleanup
	if postMergeHandler != nil {
		return true, postMergeHandler(ctx, mergeAction.PostMergeSyncTrunk)
	}
	return true, nil
}

func runMultiStackSquash(ctx *app.Context, opts squashMultiStackOptions) error {
	out := ctx.Output

	// Discover available stacks
	availableStacks, err := mergeAction.DiscoverStacks(ctx.Engine)
	if err != nil {
		return fmt.Errorf("failed to discover stacks: %w", err)
	}

	if len(availableStacks) == 0 {
		return fmt.Errorf("no independent stacks found rooted at trunk")
	}

	// Show available stacks
	out.Info("Available stacks:")
	for _, stack := range availableStacks {
		label := fmt.Sprintf("  - %s (%d branches", stack.RootBranch, len(stack.AllBranches))
		if stack.PRCount > 0 {
			label += fmt.Sprintf(", %d PRs", stack.PRCount)
		}
		if stack.Scope != "" {
			label += fmt.Sprintf(", scope: %s", stack.Scope)
		}
		label += ")"
		out.Info("%s", label)
	}
	out.Newline()

	// If no stacks specified, confirm proceeding with all stacks
	if len(opts.stacks) == 0 {
		if !ctx.Interactive {
			// Non-interactive mode requires explicit stack selection or --yes
			if !opts.yes {
				return fmt.Errorf("no stacks specified. Use --stacks to select stacks or --yes to combine all %d stacks", len(availableStacks))
			}
			out.Info("Combining all %d stacks (--yes specified)", len(availableStacks))
		} else {
			confirmed, err := tui.PromptConfirm(fmt.Sprintf("Combine all %d stacks?", len(availableStacks)), true)
			if err != nil {
				return err
			}
			if !confirmed {
				out.Info("Canceled. Use --stacks to select specific stacks.")
				return nil
			}
		}
	}

	// Execute multi-stack merge
	result, err := mergeAction.ExecuteMultiStack(ctx, mergeAction.MultiStackOptions{
		SelectedStacks: opts.stacks,
		SkipLocalCI:    opts.skipLocalCI,
		Wait:           opts.wait,
	})
	if err != nil {
		return err
	}

	// Show summary
	out.Newline()
	out.Success("Multi-stack squash merge complete!")
	out.Info("  PR: #%d %s", result.PRNumber, result.PRURL)
	out.Info("  Included: %d stacks", len(result.IncludedStacks))
	if len(result.ExcludedStacks) > 0 {
		out.Warn("  Excluded: %d stacks", len(result.ExcludedStacks))
	}

	// Fire-and-forget (default): enable automerge and return immediately
	if !opts.wait {
		prNodeID, err := getPRNodeID(ctx, result.PRNumber)
		if err != nil {
			out.Warn("Could not enable automerge: %v", err)
			out.Tip("Enable automerge manually on the PR: %s", result.PRURL)
		} else {
			if err := github.EnableAutoMerge(ctx.Context, ctx.Engine.Git(), prNodeID, github.MergeMethodSquash); err != nil {
				out.Warn("Could not enable automerge: %v", err)
				out.Tip("Enable automerge manually on the PR: %s", result.PRURL)
			} else {
				out.Success("Automerge enabled - PR will merge when CI passes")
			}
		}
		out.Newline()
		out.Tip("Run 'stackit sync --restack' after the PR is merged to update your stack.")
	}
	// When Wait=true (opt-in), ExecuteMultiStack already waited for CI and merged the PR

	return nil
}

// getPRNodeID fetches the NodeID for a PR by number
func getPRNodeID(ctx *app.Context, prNumber int) (string, error) {
	owner, repo := ctx.GitHubClient.GetOwnerRepo()
	prInfo, err := ctx.GitHubClient.GetPullRequest(ctx.Context, owner, repo, prNumber)
	if err != nil {
		return "", err
	}
	if prInfo.NodeID == "" {
		return "", fmt.Errorf("PR #%d does not have a Node ID", prNumber)
	}
	return prInfo.NodeID, nil
}

// formatSquashValidationError creates a detailed error message including validation errors and warnings.
func formatSquashValidationError(errors, warnings []string) error {
	var parts []string

	if len(errors) > 0 {
		parts = append(parts, "validation failed:")
		for _, e := range errors {
			parts = append(parts, fmt.Sprintf("  ✗ %s", e))
		}
	}

	if len(warnings) > 0 {
		if len(errors) == 0 {
			parts = append(parts, "merge blocked due to warnings:")
		}
		for _, w := range warnings {
			parts = append(parts, fmt.Sprintf("  ⚠ %s", w))
		}
	}

	parts = append(parts, "(use --force to override)")

	return fmt.Errorf("%s", strings.Join(parts, "\n"))
}
