// Package stack provides CLI commands for operating on entire stacks.
package stack

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"stackit.dev/stackit/internal/actions"
	"stackit.dev/stackit/internal/actions/merge"
	"stackit.dev/stackit/internal/actions/sync"
	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/cli/common"
	"stackit.dev/stackit/internal/config"
	"stackit.dev/stackit/internal/tui"
	"stackit.dev/stackit/internal/tui/style"
)

// NewMergeCmd creates the merge command
func NewMergeCmd() *cobra.Command {
	var (
		dryRun      bool
		yes         bool
		force       bool
		strategy    string
		scope       string
		consolidate bool
		wait        bool
		multiStack  bool
		stacks      []string
		skipLocalCI bool
	)

	cmd := &cobra.Command{
		Use:   "merge [this]",
		Short: "Merge pull requests for a stack or scope",
		Long: `Merge the pull requests associated with all branches from trunk to the current branch via Stackit.
This command merges PRs for all branches in the stack from trunk up to (and including) the current branch.

If --scope is specified, all branches with that scope will be merged.

If no flags or arguments are provided, an interactive wizard will guide you through the merge process.`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				// Handle --multi-stack flag
				if multiStack {
					return runMultiStackMerge(ctx, stacks, skipLocalCI, wait)
				}

				// Parse strategy
				var mergeStrategy merge.Strategy
				if consolidate {
					mergeStrategy = merge.StrategyConsolidate
				} else if strategy != "" {
					switch strings.ToLower(strategy) {
					case "bottom-up", "bottomup":
						mergeStrategy = merge.StrategyBottomUp
					case "top-down", "topdown":
						mergeStrategy = merge.StrategyTopDown
					case "consolidate":
						mergeStrategy = merge.StrategyConsolidate
					default:
						return fmt.Errorf("invalid strategy: %s (must be 'bottom-up', 'top-down', or 'consolidate')", strategy)
					}
				}

				// Determine if we should run in interactive wizard mode
				interactive := ctx.Interactive && strategy == "" && !consolidate && !yes && !force && scope == "" && len(args) == 0 && !cmd.Flags().Changed("wait")

				// Handle interactive mode via wizard
				if interactive || (len(args) > 0 && args[0] == "this") {
					runner, handler := NewMergeUI(ctx.Output, ctx.Logger)
					if runner != nil {
						defer runner.Cleanup()
					}

					// Cast to InteractiveHandler and verify it supports interactive prompts
					interactiveHandler, ok := handler.(merge.InteractiveHandler)
					if !ok || !interactiveHandler.IsInteractive() {
						return fmt.Errorf("interactive mode requires a TTY")
					}

					err := merge.RunWizard(ctx, interactiveHandler, merge.WizardOptions{
						DryRun:   dryRun,
						Force:    force,
						Strategy: mergeStrategy,
						Wait:     wait,
					})

					// Handle post-merge follow-up action
					var postMerge *merge.PostMergeActionRequired
					if errors.As(err, &postMerge) {
						return handlePostMergeAction(ctx, postMerge.Action)
					}
					return err
				}

				// Non-interactive mode: run merge action directly
				return runNonInteractiveMerge(ctx, mergeOptions{
					dryRun:   dryRun,
					yes:      yes,
					force:    force,
					strategy: mergeStrategy,
					scope:    scope,
					wait:     wait,
				})
			})
		},
	}

	cmd.Flags().StringVar(&strategy, "strategy", "", "Merge strategy: 'bottom-up' (merge each PR from bottom), 'top-down' (squash into one PR), or 'consolidate' (single atomic merge). Interactive if not specified.")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
	cmd.Flags().BoolVar(&force, "force", false, "Skip validation checks (draft PRs, failing CI) and automatically overwrite local trunk if it has diverged from remote")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show merge plan without executing")
	cmd.Flags().StringVar(&scope, "scope", "", "Bulk-merge all branches within the specified scope")
	cmd.Flags().BoolVarP(&consolidate, "consolidate", "c", false, "Use consolidate strategy (shortcut for --strategy=consolidate)")
	cmd.Flags().BoolVar(&wait, "wait", false, "Wait for CI checks and automatically merge (for consolidate strategy)")

	// Multi-stack flags
	cmd.Flags().BoolVar(&multiStack, "multi-stack", false, "Combine multiple independent stacks into a single PR")
	cmd.Flags().StringSliceVar(&stacks, "stacks", nil, "Stack roots to include in multi-stack merge (comma-separated)")
	cmd.Flags().BoolVar(&skipLocalCI, "skip-local-ci", false, "Skip local CI validation for multi-stack merge")

	return cmd
}

// mergeOptions holds parsed merge flags for non-interactive mode
type mergeOptions struct {
	dryRun   bool
	yes      bool
	force    bool
	strategy merge.Strategy
	scope    string
	wait     bool
}

// runNonInteractiveMerge runs merge in non-interactive (flag-based) mode
func runNonInteractiveMerge(ctx *app.Context, opts mergeOptions) error {
	// Get config values
	cfg, _ := config.LoadConfig(ctx.RepoRoot)
	undoStackDepth := cfg.UndoStackDepth()

	// Create handler for progress reporting
	runner, eventHandler := NewMergeUI(ctx.Output, ctx.Logger)
	if runner != nil {
		defer runner.Cleanup()
	}

	// Build merge action options
	actionOpts := merge.Options{
		DryRun:         opts.dryRun,
		Confirm:        !opts.yes && ctx.Interactive,
		Strategy:       opts.strategy,
		Force:          opts.force,
		Wait:           opts.wait,
		Scope:          opts.scope,
		UndoStackDepth: undoStackDepth,
		Handler:        merge.NewLegacyHandlerAdapter(eventHandler),
	}

	// If we need confirmation, create plan first and prompt
	if actionOpts.Confirm {
		plan, validation, err := merge.CreateMergePlan(ctx.Context, ctx.Engine, ctx.Output, ctx.GitHubClient, merge.CreatePlanOptions{
			Strategy: opts.strategy,
			Force:    opts.force,
			Scope:    opts.scope,
			Wait:     opts.wait,
		})
		if err != nil {
			return err
		}
		actionOpts.Plan = plan

		// Show plan and validate
		planText := merge.FormatMergePlan(plan, validation)
		ctx.Output.Print(planText)

		if !validation.Valid && !opts.force {
			return fmt.Errorf("validation failed (use --force to override)")
		}

		// Prompt for confirmation
		confirmed, err := tui.PromptConfirm("Proceed with merge?", false)
		if err != nil {
			return fmt.Errorf("confirmation canceled: %w", err)
		}
		if !confirmed {
			ctx.Output.Info("Merge canceled")
			return nil
		}
		actionOpts.Confirm = false // Already confirmed
	}

	return merge.Action(ctx, actionOpts)
}

// handlePostMergeAction handles post-merge follow-up actions
func handlePostMergeAction(ctx *app.Context, action merge.PostMergeAction) error {
	out := ctx.Output

	switch action {
	case merge.PostMergeSyncTrunk:
		result, err := actions.CheckoutAction(ctx, actions.CheckoutOptions{
			CheckoutTrunk: true,
		}, nil)
		if err != nil {
			out.Newline()
			out.Error("%v", err)
			out.Newline()
			out.Info("%s", style.ColorYellow("To fix and continue:"))
			out.Info("  (1) Handle your local changes (e.g., %s or %s)", style.ColorCyan("git stash"), style.ColorCyan("git commit"))
			out.Info("  (2) Switch to trunk: %s", style.ColorCyan("stackit checkout --trunk"))
			out.Info("  (3) Sync your workspace: %s", style.ColorCyan("stackit sync --restack"))
			return nil
		}

		if result.WorktreeSwitchPath != "" {
			if common.HasShellIntegration() {
				ctx.Output.DirectiveCD(result.WorktreeSwitchPath)
				if len(result.RerunArgs) > 0 {
					ctx.Output.DirectiveRerun(result.RerunArgs...)
				}
			} else {
				for _, tip := range result.FallbackTips {
					ctx.Output.Tip("%s", tip)
				}
			}
		}

		runner, handler := NewSyncUI(ctx.Output, ctx.Logger)
		if runner != nil {
			defer runner.Cleanup()
		}

		return sync.Action(ctx, sync.Options{
			Restack: true,
		}, handler)

	case merge.PostMergeDone:
		return nil
	}

	return nil
}

// runMultiStackMerge runs the multi-stack merge operation
func runMultiStackMerge(ctx *app.Context, selectedStacks []string, skipLocalCI bool, wait bool) error {
	out := ctx.Output

	// Discover available stacks for preview
	availableStacks, err := merge.DiscoverStacks(ctx.Engine)
	if err != nil {
		return fmt.Errorf("failed to discover stacks: %w", err)
	}

	if len(availableStacks) == 0 {
		return fmt.Errorf("no independent stacks found rooted at trunk")
	}

	// Show available stacks
	out.Info("Available stacks:")
	for _, stack := range availableStacks {
		label := fmt.Sprintf("  • %s (%d branches", stack.RootBranch, len(stack.AllBranches))
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

	// If interactive and no stacks specified, confirm proceeding with all stacks
	if len(selectedStacks) == 0 && ctx.Interactive {
		confirmed, err := tui.PromptConfirm(fmt.Sprintf("Combine all %d stacks?", len(availableStacks)), true)
		if err != nil {
			return err
		}
		if !confirmed {
			out.Info("Canceled. Use --stacks to select specific stacks.")
			return nil
		}
	}

	// Execute multi-stack merge
	result, err := merge.ExecuteMultiStack(ctx, merge.MultiStackOptions{
		SelectedStacks: selectedStacks,
		SkipLocalCI:    skipLocalCI,
		Wait:           wait,
	})
	if err != nil {
		return err
	}

	// Show summary
	out.Newline()
	out.Success("Multi-stack merge complete!")
	out.Info("  PR: #%d %s", result.PRNumber, result.PRURL)
	out.Info("  Included: %d stacks", len(result.IncludedStacks))
	if len(result.ExcludedStacks) > 0 {
		out.Warn("  Excluded: %d stacks", len(result.ExcludedStacks))
	}

	return nil
}
