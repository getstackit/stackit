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
	"stackit.dev/stackit/internal/engine"
	sterrors "stackit.dev/stackit/internal/errors"
	"stackit.dev/stackit/internal/tui"
	"stackit.dev/stackit/internal/tui/components/tree"
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
				// Handle 'stackit merge this'
				if len(args) > 0 && args[0] == "this" {
					return runInteractiveMergeWizard(ctx, dryRun, force, "")
				}

				// Determine if we should run in interactive mode
				// Interactive if no flags are provided (except dry-run and scope which are always allowed)
				// Respect global interactive flag
				interactive := ctx.Interactive && strategy == "" && !consolidate && !yes && !force && scope == "" && len(args) == 0 && !cmd.Flags().Changed("wait")

				// Parse strategy
				var mergeStrategy merge.Strategy
				if consolidate {
					// --consolidate flag takes precedence
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

				// Run interactive wizard if needed
				if interactive {
					return runMergeTypeSelector(ctx, dryRun, force)
				}

				// Get config values
				cfg, _ := config.LoadConfig(ctx.RepoRoot)
				undoStackDepth := cfg.UndoStackDepth()

				// Create plan if scope is specified
				var plan *merge.Plan
				if scope != "" {
					p, _, err := merge.CreateMergePlan(ctx.Context, ctx.Engine, ctx.Splog, ctx.GitHubClient, merge.CreatePlanOptions{
						Strategy: mergeStrategy,
						Force:    force,
						Scope:    scope,
					})
					if err != nil {
						return err
					}
					plan = p
				}

				// Run merge action
				opts := merge.Options{
					DryRun:         dryRun,
					Confirm:        !yes && ctx.Interactive, // If --yes or --no-interactive is set, don't confirm
					Strategy:       mergeStrategy,
					Force:          force,
					Wait:           wait,
					Scope:          scope,
					Plan:           plan,
					UndoStackDepth: undoStackDepth,
					Handler:        NewMergeHandler(ctx),
				}

				if opts.Confirm {
					// If we need confirmation, we need a plan first
					if opts.Plan == nil {
						p, validation, err := merge.CreateMergePlan(ctx.Context, ctx.Engine, ctx.Splog, ctx.GitHubClient, merge.CreatePlanOptions{
							Strategy: opts.Strategy,
							Force:    opts.Force,
							Scope:    opts.Scope,
						})
						if err != nil {
							return err
						}
						opts.Plan = p

						// Show plan and confirm
						planText := merge.FormatMergePlan(p, validation)
						ctx.Splog.Page(planText)

						if !validation.Valid && !opts.Force {
							return fmt.Errorf("validation failed (use --force to override)")
						}

						confirmed, err := tui.PromptConfirm("Proceed with merge?", false)
						if err != nil {
							return fmt.Errorf("confirmation canceled: %w", err)
						}
						if !confirmed {
							ctx.Splog.Info("Merge canceled")
							return nil
						}
						opts.Confirm = false // Already confirmed
					}
				}

				return merge.Action(ctx, opts)
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

	return cmd
}

// runInteractiveMergeWizard runs the interactive merge wizard
func runInteractiveMergeWizard(ctx *app.Context, dryRun bool, forceFlag bool, scope string) error {
	return runInteractiveMergeWizardForBranch(ctx, dryRun, forceFlag, scope, "")
}

// runInteractiveMergeWizardForBranch runs the interactive merge wizard for a specific branch (if scope is empty)
func runInteractiveMergeWizardForBranch(ctx *app.Context, dryRun bool, forceFlag bool, scope string, targetBranchName string) error {
	eng := ctx.Engine
	splog := ctx.Splog

	splog.Info("🔍 Analyzing stack...")
	splog.Newline()

	// Populate remote SHAs so we can accurately check if branches match remote
	if err := eng.PopulateRemoteShas(); err != nil {
		splog.Debug("Failed to populate remote SHAs: %v", err)
	}

	// Create initial plan with bottom-up strategy (default)
	plan, validation, err := merge.CreateMergePlan(ctx.Context, eng, splog, ctx.GitHubClient, merge.CreatePlanOptions{
		Strategy:     merge.StrategyBottomUp,
		Force:        forceFlag,
		Scope:        scope,
		TargetBranch: targetBranchName,
	})
	if err != nil {
		return err
	}

	// Mid-stack detection: if not using --scope, check if there are branches above
	// the current branch in the same scope that won't be merged
	if scope == "" && len(plan.BranchesToMerge) > 0 {
		currentBranch := eng.GetBranch(plan.CurrentBranch)
		currentScope := eng.GetScope(currentBranch)
		if !currentScope.IsEmpty() {
			// Check if any upstack branches are in the same scope
			upstackInScope := []string{}
			for _, upstackName := range plan.UpstackBranches {
				upstackBranch := eng.GetBranch(upstackName)
				upstackScope := eng.GetScope(upstackBranch)
				if upstackScope.String() == currentScope.String() {
					upstackInScope = append(upstackInScope, upstackName)
				}
			}
			if len(upstackInScope) > 0 {
				splog.Warn("⚠️  You're mid-stack in scope [%s]", currentScope.String())
				splog.Warn("   Branches above in the same scope won't be merged: %s", strings.Join(upstackInScope, ", "))
				splog.Info("   💡 To merge the entire scope, use: stackit merge --scope=%s", currentScope.String())
				splog.Newline()
			}
		}
	}

	// Display current state using stack tree
	if scope != "" {
		splog.Info("Merging scope: [%s]", scope)
	} else {
		splog.Info("Target branch: %s", style.ColorBranchName(plan.CurrentBranch, false))
	}
	splog.Newline()

	if len(plan.BranchesToMerge) > 0 {
		// Create tree renderer
		renderer := tui.NewStackTreeRenderer(eng)

		// Build annotations for branches to merge
		annotations := make(map[string]tree.BranchAnnotation)
		for _, branchInfo := range plan.BranchesToMerge {
			annotation := tree.BranchAnnotation{
				PRNumber:    &branchInfo.PRNumber,
				CheckStatus: string(branchInfo.ChecksStatus),
				IsDraft:     branchInfo.IsDraft,
			}
			annotations[branchInfo.BranchName] = annotation
		}
		renderer.SetAnnotations(annotations)

		// Render a list of branches to merge
		splog.Info("Stack to merge (bottom to top):")
		branchNames := make([]string, len(plan.BranchesToMerge))
		for i, branchInfo := range plan.BranchesToMerge {
			branchNames[i] = branchInfo.BranchName
		}
		stackLines := renderer.RenderBranchList(branchNames)
		for _, line := range stackLines {
			splog.Info("%s", line)
		}
		splog.Newline()

		// Show upstack branches that will be restacked
		if len(plan.UpstackBranches) > 0 {
			splog.Info("Branches above (will be restacked on trunk):")
			for _, branchName := range plan.UpstackBranches {
				splog.Info("  • %s", style.ColorBranchName(branchName, false))
			}
			splog.Newline()
		}
	}

	// Show validation errors if any
	if !validation.Valid {
		splog.Warn("Errors found:")
		for _, errMsg := range validation.Errors {
			splog.Warn("  ✗ %s", errMsg)
		}
		splog.Newline()
		splog.Info("Cannot proceed with merge. Use --force to override validation checks.")
		return fmt.Errorf("validation failed")
	}

	// Show warnings if any
	if len(validation.Warnings) > 0 {
		splog.Warn("Warnings:")
		for _, warn := range validation.Warnings {
			splog.Warn("  %s", warn)
		}
		splog.Newline()
		if !forceFlag {
			splog.Info("Cannot proceed with merge due to warnings. Use --force to override validation checks.")
			return fmt.Errorf("merge blocked due to warnings (use --force to override)")
		}
		splog.Info("Proceeding despite warnings (--force enabled)")
	}

	// Show informational messages if any
	if len(validation.Infos) > 0 {
		splog.Info("Information:")
		for _, info := range validation.Infos {
			splog.Info("  • %s", info)
		}
		splog.Newline()
	}

	// Determine merge strategy
	var mergeStrategy merge.Strategy
	var wait bool

	// If only a single PR, automatically use top-down strategy
	if len(plan.BranchesToMerge) == 1 {
		mergeStrategy = merge.StrategyTopDown
		splog.Info("✅ Strategy: %s (auto-selected for single PR)", mergeStrategy)
		splog.Newline()
	} else {
		// Prompt for strategy using interactive selector
		// Pre-select consolidate for larger stacks (3+), bottom-up for smaller
		var strategyOptions []tui.SelectOption
		var defaultIndex int

		if len(plan.BranchesToMerge) >= 3 {
			// For 3+ branches, recommend consolidate
			strategyOptions = []tui.SelectOption{
				{Label: "🔀 Consolidate — Create single PR with all stack commits for atomic merge (recommended)", Value: "consolidate"},
				{Label: "🔄 Bottom-up — Merge PRs one at a time from bottom", Value: "bottom-up"},
				{Label: "📦 Top-down — Squash all changes into one PR, merge once", Value: "top-down"},
			}
			defaultIndex = 0 // Consolidate
		} else {
			// For 2 branches, recommend bottom-up
			strategyOptions = []tui.SelectOption{
				{Label: "🔄 Bottom-up — Merge PRs one at a time from bottom (recommended)", Value: "bottom-up"},
				{Label: "📦 Top-down — Squash all changes into one PR, merge once", Value: "top-down"},
				{Label: "🔀 Consolidate — Create single PR with all stack commits for atomic merge", Value: "consolidate"},
			}
			defaultIndex = 0 // Bottom-up
		}

		selectedStrategy, err := tui.PromptSelect("Select merge strategy:", strategyOptions, defaultIndex)
		if err != nil {
			return fmt.Errorf("strategy selection canceled: %w", err)
		}

		switch selectedStrategy {
		case "bottom-up":
			mergeStrategy = merge.StrategyBottomUp
		case "top-down":
			mergeStrategy = merge.StrategyTopDown
		case "consolidate":
			mergeStrategy = merge.StrategyConsolidate
			// Prompt for wait if consolidating
			wait, err = tui.PromptConfirm("Wait for CI and automatically merge the consolidated PR?", false)
			if err != nil {
				return fmt.Errorf("wait selection canceled: %w", err)
			}
		}

		splog.Info("✅ Strategy: %s", mergeStrategy)
		if mergeStrategy == merge.StrategyConsolidate {
			if wait {
				splog.Info("✅ Wait for CI: Enabled")
			} else {
				splog.Info("✅ Wait for CI: Disabled (manual merge required)")
			}
		}
		splog.Newline()
	}

	// Recreate plan with selected strategy
	plan, validation, err = merge.CreateMergePlan(ctx.Context, eng, splog, ctx.GitHubClient, merge.CreatePlanOptions{
		Strategy:     mergeStrategy,
		Force:        forceFlag,
		Scope:        scope,
		TargetBranch: targetBranchName,
	})
	if err != nil {
		return err
	}

	// Re-validate if strategy changed (important for top-down)
	if !validation.Valid && !forceFlag {
		splog.Warn("Errors found with selected strategy:")
		for _, errMsg := range validation.Errors {
			splog.Warn("  ✗ %s", errMsg)
		}
		return fmt.Errorf("validation failed")
	}

	// Show plan preview
	splog.Info("📋 Merge Plan:")
	if mergeStrategy == merge.StrategyConsolidate {
		// For consolidate, show a clear summary of what will happen
		splog.Info("  1. Lock all %d branches to prevent changes", len(plan.BranchesToMerge))
		splog.Info("  2. Create consolidation branch with all commits merged")
		splog.Info("  3. Create consolidation PR")
		if wait {
			splog.Info("  4. Wait for CI and auto-merge consolidation PR")
			splog.Info("  5. Update original PRs with consolidation reference")
			if len(plan.UpstackBranches) > 0 {
				splog.Info("  6. Restack %d upstack branches onto trunk", len(plan.UpstackBranches))
			}
		} else {
			splog.Info("  4. Manual merge required (individual PRs remain locked)")
		}
	} else {
		// For other strategies, show step-by-step
		for i, step := range plan.Steps {
			splog.Info("  %d. %s", i+1, step.Description)
		}
	}
	splog.Newline()

	// If dry-run, stop here
	if dryRun {
		splog.Info("Dry-run mode: plan displayed above. Use without --dry-run to execute.")
		return nil
	}

	// Prompt for confirmation
	confirmed, err := tui.PromptConfirm("Proceed with merge?", false)
	if err != nil {
		return fmt.Errorf("confirmation canceled: %w", err)
	}
	if !confirmed {
		splog.Info("Merge canceled")
		return nil
	}

	// Determine worktree usage:
	// All merges now use worktrees by default.

	// Get config values
	cfg, _ := config.LoadConfig(ctx.RepoRoot)
	undoStackDepth := cfg.UndoStackDepth()

	// Execute the plan
	mergeOpts := merge.Options{
		DryRun:         dryRun,
		Confirm:        false, // Already confirmed
		Strategy:       mergeStrategy,
		Force:          forceFlag,
		Wait:           wait,
		Scope:          scope,
		TargetBranch:   targetBranchName,
		Plan:           plan,
		UndoStackDepth: undoStackDepth,
		Handler:        NewMergeHandler(ctx),
	}

	if err := merge.Action(ctx, mergeOpts); err != nil {
		return fmt.Errorf("merge action failed: %w", err)
	}

	if !dryRun && ctx.Interactive {
		return handlePostMergeFollowUp(ctx)
	}

	return nil
}

func handlePostMergeFollowUp(ctx *app.Context) error {
	splog := ctx.Splog
	splog.Newline()
	splog.Info("🎉 Merge completed successfully in the temporary worktree!")
	splog.Info("Your main workspace remains untouched.")

	options := []tui.SelectOption{
		{Label: "🔄 Switch to trunk and sync (" + ctx.Engine.Trunk().GetName() + ")", Value: "trunk-sync"},
		{Label: "Done", Value: "done"},
	}

	selected, err := tui.PromptSelect("What would you like to do in your main workspace?", options, 0)
	if err != nil {
		if errors.Is(err, sterrors.ErrCanceled) {
			return nil
		}
		return err
	}
	if selected == "done" {
		return nil
	}

	if selected == "trunk-sync" {
		if err := actions.CheckoutAction(ctx, actions.CheckoutOptions{
			CheckoutTrunk: true,
		}); err != nil {
			return err
		}
		handler := NewSyncHandler(ctx.Splog)
		return sync.Action(ctx, sync.Options{
			Restack: true,
		}, handler)
	}
	return nil
}

// runMergeTypeSelector runs an interactive selector to choose what to merge
func runMergeTypeSelector(ctx *app.Context, dryRun bool, force bool) error {
	eng := ctx.Engine

	options := []tui.SelectOption{
		{Label: "🌿 This branch — Merge the current branch and its stack", Value: "this"},
		{Label: "🏷️  Select a scope — Merge all branches in a specific scope", Value: "scope"},
		{Label: "📚 Select an entire stack — Merge a stack from its top branch", Value: "stack"},
	}

	selected, err := tui.PromptSelect("What would you like to merge?", options, 0)
	if err != nil {
		return err
	}

	switch selected {
	case "this":
		return runInteractiveMergeWizard(ctx, dryRun, force, "")
	case "scope":
		// Get all unique scopes
		scopes := make(map[string]bool)
		for _, b := range eng.AllBranches() {
			s := eng.GetScope(b).String()
			if s != "" && s != "none" && s != "clear" {
				scopes[s] = true
			}
		}

		if len(scopes) == 0 {
			return fmt.Errorf("no branches with scopes found")
		}

		scopeOptions := make([]tui.SelectOption, 0, len(scopes))
		for s := range scopes {
			scopeOptions = append(scopeOptions, tui.SelectOption{Label: s, Value: s})
		}

		selectedScope, err := tui.PromptSelect("Select scope to merge:", scopeOptions, 0)
		if err != nil {
			return err
		}

		return runInteractiveMergeWizard(ctx, dryRun, force, selectedScope)

	case "stack":
		// Get all leaf branches (branches with no children)
		branches := eng.AllBranches()
		leafBranches := make([]engine.Branch, 0)
		for _, b := range branches {
			if !b.IsTrunk() && len(b.GetChildren()) == 0 {
				leafBranches = append(leafBranches, b)
			}
		}

		if len(leafBranches) == 0 {
			return fmt.Errorf("no stacks found")
		}

		// Use branch selector
		selectedBranch, err := tui.PromptLogSelect(ctx.Context, ctx.Engine, ctx.GitHubClient, tui.LogOptions{
			Style: "FULL",
		})
		if err != nil {
			return err
		}

		return runInteractiveMergeWizardForBranch(ctx, dryRun, force, "", selectedBranch)
	}

	return nil
}
