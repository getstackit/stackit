// Package merge provides CLI commands for merging stacked PRs.
package merge

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	mergeAction "github.com/getstackit/stackit/internal/actions/merge"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/cli/common"
	"github.com/getstackit/stackit/internal/config"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/github"
	"github.com/getstackit/stackit/internal/tui"
)

// NewShipCmd creates the merge ship subcommand.
// This command consolidates all branches in the stack into a single PR and merges it atomically.
// It uses GitHub automerge by default and waits for the merge to complete.
func NewShipCmd(postMergeHandler PostMergeHandler) *cobra.Command {
	var (
		dryRun      bool
		yes         bool
		force       bool
		wait        bool
		method      string
		scope       string
		branch      string
		stacks      []string
		skipLocalCI bool
	)

	cmd := &cobra.Command{
		Use:     mergeStrategyShip,
		Aliases: []string{mergeStrategySquash},
		Short:   "Consolidate all stack branches into a single PR and merge atomically",
		Long: `Consolidate all branches in the stack into a single PR for atomic merging.

This creates a new "consolidation" branch that contains all the commits from the stack,
opens a PR for it, and enables GitHub's automerge.

By default, the command waits for the PR to merge, then automatically:
1. Pulls the latest trunk
2. Deletes the merged branches
3. Restacks any remaining upstack branches

Use --wait=false to return immediately after enabling automerge (fire-and-forget).
In fire-and-forget mode, run 'stackit sync --restack' after the PR merges to clean up.

Use --scope to consolidate all branches within a specific scope.
Use --stacks to combine multiple independent stacks into a single PR.`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				// Handle multi-stack mode
				if len(stacks) > 0 || cmd.Flags().Changed("stacks") {
					return runMultiStackShip(ctx, shipMultiStackOptions{
						dryRun:      dryRun,
						stacks:      stacks,
						skipLocalCI: skipLocalCI,
						wait:        wait,
						yes:         yes,
					})
				}

				return runMergeShip(ctx, mergeShipOptions{
					dryRun: dryRun,
					yes:    yes,
					force:  force,
					wait:   wait,
					method: method,
					scope:  scope,
					branch: branch,
				}, postMergeHandler)
			})
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show merge plan without executing")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
	cmd.Flags().BoolVar(&force, "force", false, "Skip validation checks (draft PRs, failing CI)")
	cmd.Flags().BoolVar(&wait, "wait", true, "Wait for merge to complete")
	cmd.Flags().StringVar(&method, "method", "", "Merge method (squash, merge, rebase). Uses config merge.method if not specified")
	cmd.Flags().StringVar(&scope, "scope", "", "Consolidate all branches within the specified scope")
	cmd.Flags().StringVar(&branch, "branch", "", "Target branch to merge from (default: current branch)")
	cmd.Flags().StringSliceVar(&stacks, "stacks", nil, "Combine multiple stacks (comma-separated stack roots)")
	cmd.Flags().BoolVar(&skipLocalCI, "skip-local-ci", false, "Skip local CI validation for multi-stack merge")
	cmd.MarkFlagsMutuallyExclusive("scope", "branch")
	cmd.MarkFlagsMutuallyExclusive("stacks", "scope")
	cmd.MarkFlagsMutuallyExclusive("stacks", "branch")
	cmd.MarkFlagsMutuallyExclusive("stacks", "force")

	return cmd
}

type mergeShipOptions struct {
	dryRun bool
	yes    bool
	force  bool
	wait   bool
	method string
	scope  string
	branch string
}

type shipMultiStackOptions struct {
	dryRun      bool
	stacks      []string
	skipLocalCI bool
	wait        bool
	yes         bool
}

func runMergeShip(ctx *app.Context, opts mergeShipOptions, postMergeHandler PostMergeHandler) error {
	out := ctx.Output

	// Collect branches once (expensive: GitHub API calls) then build plan
	collected, err := mergeAction.CollectMergeBranches(ctx.Context, ctx.Engine, ctx.Output, ctx.GitHub(), mergeAction.CreatePlanOptions{
		Strategy:     mergeAction.StrategyShip,
		Force:        opts.force,
		Scope:        opts.scope,
		TargetBranch: opts.branch,
		Wait:         opts.wait,
	})
	if err != nil {
		return err
	}

	plan := mergeAction.BuildMergePlan(collected, mergeAction.StrategyShip, opts.wait)
	validation := collected.Validation

	// Show plan
	planText := mergeAction.FormatMergePlan(plan, validation)
	out.Print(planText)

	// Validate
	if !validation.Valid && !opts.force {
		return mergeAction.FormatValidationError(validation.Errors, validation.Warnings)
	}

	// Dry run - just show the plan
	if opts.dryRun {
		out.Info("Dry-run mode: No changes were made.")
		return nil
	}

	if err := requireYesInNonInteractive(ctx, "merge ship", opts.yes, opts.dryRun); err != nil {
		return err
	}

	// Fail fast if no GitHub client
	if _, err := ctx.RequireGitHub(); err != nil {
		return err
	}

	// Confirm unless --yes
	if !opts.yes && ctx.Interactive {
		confirmed, err := tui.PromptConfirm("Proceed with ship merge?", false)
		if err != nil {
			return fmt.Errorf("confirmation canceled: %w", err)
		}
		if !confirmed {
			out.Info("Merge canceled")
			return nil
		}
	}

	// Prompt for a stack description on multi-PR stacks that have none set.
	// Best-effort: a failure here never blocks the ship.
	if len(plan.BranchesToMerge) > 1 {
		promptForShipDescription(ctx, plan.BranchesToMerge, opts.yes)
	}

	// Get config values
	cfg, _ := config.LoadConfig(ctx.RepoRoot)
	undoStackDepth := cfg.UndoStackDepth()

	// Create handler for progress reporting. runner.Cleanup is nil-safe.
	runner, eventHandler := NewMergeUI(ctx.Output, ctx.Logger)
	defer runner.Cleanup()

	// Resolve merge method if specified via flag
	var mergeMethod github.MergeMethod
	if opts.method != "" {
		var err error
		mergeMethod, err = resolveMergeMethod(ctx, opts.method)
		if err != nil {
			return err
		}
	}

	// Execute consolidation merge using automerge
	actionOpts := mergeAction.Options{
		DryRun:         false,
		Confirm:        false, // Already confirmed
		Strategy:       mergeAction.StrategyShip,
		Force:          opts.force,
		Wait:           opts.wait,
		Scope:          opts.scope,
		TargetBranch:   opts.branch,
		Plan:           plan,
		UndoStackDepth: undoStackDepth,
		Handler:        eventHandler,
		MergeMethod:    mergeMethod,
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

// promptForShipDescription checks whether the stack has a description and, when
// interactive and not skipped via --yes, prompts the user to set one. For
// non-interactive or --yes invocations it prints a warning and continues.
//
// This is best-effort: any prompt cancellation or failure is treated as
// "leave the description unset" and never blocks the ship.
func promptForShipDescription(ctx *app.Context, branches []mergeAction.BranchMergeInfo, skipPrompt bool) {
	out := ctx.Output
	if len(branches) == 0 {
		return
	}

	rootBranch := ctx.Engine.GetBranch(branches[0].BranchName)
	desc := ctx.Engine.GetStackDescription(rootBranch)
	if desc != nil && !desc.IsEmpty() {
		return
	}

	if !ctx.Interactive || skipPrompt {
		out.Warn("Stack has %d PRs but no description is set. Run 'stackit describe' to add one.", len(branches))
		return
	}

	out.Warn("This stack has %d PRs but no description is set.", len(branches))
	addDesc, err := tui.PromptConfirm("Add a title for this stack now?", true)
	if err != nil || !addDesc {
		return
	}

	title, err := tui.PromptTextInput("Stack title:", "")
	if err != nil || strings.TrimSpace(title) == "" {
		return
	}

	newDesc := &git.StackDescription{Title: strings.TrimSpace(title)}
	if err := ctx.Engine.SetStackDescription(ctx.Context, rootBranch, newDesc); err != nil {
		out.Warn("Failed to set stack description: %v", err)
	} else {
		out.Info("Set stack title: %s", newDesc.Title)
	}
}

func runMultiStackShip(ctx *app.Context, opts shipMultiStackOptions) error {
	out := ctx.Output

	// Discover available stacks
	availableStacks, err := mergeAction.DiscoverStacks(ctx.Engine)
	if err != nil {
		return fmt.Errorf("failed to discover stacks: %w", err)
	}

	if len(availableStacks) == 0 {
		return fmt.Errorf("no independent stacks found rooted at trunk")
	}

	if !opts.dryRun {
		if _, err := ctx.RequireGitHub(); err != nil {
			return err
		}
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

	// Dry-run mode: show the plan and exit without side effects. Evaluated
	// before the --yes check so it is usable non-interactively without flags.
	if opts.dryRun {
		selected := availableStacks
		if len(opts.stacks) > 0 {
			selected = mergeAction.FilterStacks(availableStacks, opts.stacks)
			if len(selected) == 0 {
				return fmt.Errorf("none of the specified stacks were found")
			}
		}

		out.Info("Dry-run mode: multi-stack merge plan")
		out.Info("Combining %d stacks:", len(selected))
		for _, stack := range selected {
			out.Info("  - %s (%d branches)", stack.RootBranch, len(stack.AllBranches))
		}
		out.Info("Local CI validation: not run in dry-run")
		if opts.wait {
			out.Info("Automerge: would wait for CI and merge")
		} else {
			out.Info("Automerge: fire-and-forget (manual enablement after PR creation)")
		}
		out.Info("Dry-run mode: No changes were made.")
		return nil
	}

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
	out.Success("Multi-stack ship merge complete!")
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
			mergeMethod, methodErr := mergeAction.GetMergeMethod(ctx, ctx.GitHub())
			if methodErr != nil {
				out.Warn("Could not determine merge method: %v", methodErr)
				out.Tip("Enable automerge manually on the PR: %s", result.PRURL)
			} else {
				if err := github.EnableAutoMerge(ctx.Context, ctx.Engine.Git(), prNodeID, github.EnableAutoMergeOptions{MergeMethod: mergeMethod}); err != nil { //nolint:forbidigo // GitHub integration needs the git runner to run gh; not a domain bypass
					out.Warn("Could not enable automerge: %v", err)
					out.Tip("Enable automerge manually on the PR: %s", result.PRURL)
				} else {
					out.Success("Automerge enabled - PR will merge when CI passes")
				}
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
	owner, repo := ctx.GitHub().GetOwnerRepo()
	prInfo, err := ctx.GitHub().GetPullRequest(ctx.Context, owner, repo, prNumber)
	if err != nil {
		return "", err
	}
	if prInfo.NodeID == "" {
		return "", fmt.Errorf("PR #%d does not have a Node ID", prNumber)
	}
	return prInfo.NodeID, nil
}
