// Package merge provides CLI commands for merging stacked PRs.
package merge

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	mergeAction "github.com/getstackit/stackit/internal/actions/merge"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/cli/common"
	"github.com/getstackit/stackit/internal/tui"
)

// NewNextCmd creates the merge next subcommand.
// This command merges the bottom-most unmerged PR in the stack, restacks the remaining
// branches, and stops. It uses GitHub automerge by default and waits for the merge to complete.
func NewNextCmd(postMergeHandler PostMergeHandler) *cobra.Command {
	var (
		dryRun bool
		yes    bool
		force  bool
		wait   bool
		method string
		branch string
		scope  string
	)

	cmd := &cobra.Command{
		Use:   "next",
		Short: "Merge the next (bottom-most) unmerged PR in the stack",
		Long: `Merge the bottom-most unmerged PR in the stack using GitHub automerge.

After enabling automerge, the command returns immediately (fire-and-forget).

Use --wait to block until the PR is merged, then automatically:
1. Pull the latest trunk
2. Restack the remaining branches in the stack
3. Stop (run again to merge the next PR)`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				return runMergeNext(ctx, mergeNextOptions{
					dryRun: dryRun,
					yes:    yes,
					force:  force,
					wait:   wait,
					method: method,
					branch: branch,
					scope:  scope,
				}, postMergeHandler)
			})
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show merge plan without executing")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
	cmd.Flags().BoolVar(&force, "force", false, "Skip validation checks (draft PRs, failing CI)")
	cmd.Flags().BoolVar(&wait, "wait", false, "Wait for merge to complete (default: fire-and-forget)")
	cmd.Flags().StringVar(&method, "method", "", "Merge method (squash, merge, rebase). Uses config merge.method if not specified")
	cmd.Flags().StringVar(&branch, "branch", "", "Target branch to merge from (default: current branch)")
	cmd.Flags().StringVar(&scope, "scope", "", "Merge the next PR within the specified scope")
	cmd.MarkFlagsMutuallyExclusive("scope", "branch")

	return cmd
}

type mergeNextOptions struct {
	dryRun bool
	yes    bool
	force  bool
	wait   bool
	method string
	branch string
	scope  string
}

func runMergeNext(ctx *app.Context, opts mergeNextOptions, postMergeHandler PostMergeHandler) error {
	out := ctx.Output
	eng := ctx.Engine

	// Use CreateMergePlan with bottom-up strategy to find what to merge
	plan, validation, err := mergeAction.CreateMergePlan(ctx.Context, eng, out, ctx.GitHub(), mergeAction.CreatePlanOptions{
		Strategy:     mergeAction.StrategyBottomUp,
		Force:        opts.force,
		TargetBranch: opts.branch,
		Scope:        opts.scope,
	})
	if err != nil {
		return err
	}

	if len(plan.BranchesToMerge) == 0 {
		out.Success("No unmerged PRs found in the stack")
		return nil
	}

	// For "merge next", we only care about the bottom-most PR
	bottomPR := plan.BranchesToMerge[0]

	nextValidation := buildMergeNextValidation(plan, validation, opts.force)

	// Show the plan
	planText := formatMergeNextPlan(plan, nextValidation, opts.wait)
	out.Print(planText)

	// Validate
	if !nextValidation.Valid && !opts.force {
		return mergeAction.FormatValidationError(nextValidation.Errors, nextValidation.Warnings)
	}

	// Dry run - just show the plan
	if opts.dryRun {
		out.Info("Dry-run mode: No changes were made.")
		return nil
	}

	if err := requireYesInNonInteractive(ctx, "merge next", opts.yes, opts.dryRun); err != nil {
		return err
	}

	// Fail fast if no GitHub client
	if _, err := ctx.RequireGitHub(); err != nil {
		return err
	}

	// Confirm unless --yes
	if !opts.yes && ctx.Interactive {
		confirmed, err := tui.PromptConfirm(fmt.Sprintf("Merge PR #%d (%s)?", bottomPR.PRNumber, bottomPR.BranchName), false)
		if err != nil {
			return fmt.Errorf("confirmation canceled: %w", err)
		}
		if !confirmed {
			out.Info("Merge canceled")
			return nil
		}
	}

	// Determine merge method: flag > config > prompt
	mergeMethod, err := resolveMergeMethod(ctx, opts.method)
	if err != nil {
		return err
	}

	// Get the PR's NodeID for merge operations
	remoteCtx, cancelRemote := ctx.RemoteOperationContext()
	prInfo, err := ctx.GitHub().GetPullRequest(remoteCtx, bottomPR.PRNumber)
	cancelRemote()
	if err != nil {
		return fmt.Errorf("failed to get PR info: %w", err)
	}
	if prInfo.NodeID == "" {
		return fmt.Errorf("PR #%d does not have a Node ID", bottomPR.PRNumber)
	}

	// Orchestrate the merge (direct merge → automerge → poll fallback)
	outcome, err := orchestrateMerge(ctx, orchestrateMergeOptions{
		branchName:  bottomPR.BranchName,
		prNumber:    bottomPR.PRNumber,
		prNodeID:    prInfo.NodeID,
		mergeMethod: mergeMethod,
		wait:        opts.wait,
	})
	if err != nil {
		return err
	}

	switch outcome {
	case OutcomeMerged:
		// Perform post-merge cleanup
		out.Newline()
		out.Info("Performing post-merge cleanup...")
		if postMergeHandler != nil {
			return postMergeHandler(ctx, mergeAction.PostMergeSyncTrunk)
		}
		return nil
	case OutcomeAutomergeEnabled:
		out.Info("PR will be merged automatically when CI passes and requirements are met.")
		out.Tip("Run 'stackit sync --restack' after the PR is merged to update your stack.")
		return nil
	default:
		return nil
	}
}

func buildMergeNextValidation(plan *mergeAction.Plan, base *mergeAction.PlanValidation, force bool) *mergeAction.PlanValidation {
	next := &mergeAction.PlanValidation{
		Valid:    true,
		Errors:   []string{},
		Warnings: []string{},
		Infos:    []string{},
	}
	if plan == nil || len(plan.BranchesToMerge) == 0 {
		return next
	}

	bottom := plan.BranchesToMerge[0]
	if bottom.PRNumber <= 0 {
		next.Errors = append(next.Errors, fmt.Sprintf("Branch %s has no associated PR", bottom.BranchName))
	}

	if !force {
		if bottom.IsDraft {
			next.Errors = append(next.Errors, fmt.Sprintf("Branch %s PR #%d is a draft", bottom.BranchName, bottom.PRNumber))
		}
		if bottom.ChecksStatus == mergeAction.ChecksFailing {
			next.Errors = append(next.Errors, fmt.Sprintf("Branch %s PR #%d has failing CI checks", bottom.BranchName, bottom.PRNumber))
		}
	}

	if base != nil {
		next.Warnings = filterBranchScopedMessages(base.Warnings, bottom.BranchName)
		next.Infos = filterBranchScopedMessages(base.Infos, bottom.BranchName)
	}

	if len(next.Errors) > 0 {
		next.Valid = false
	}

	return next
}

func filterBranchScopedMessages(messages []string, branchName string) []string {
	if len(messages) == 0 {
		return nil
	}

	needle := fmt.Sprintf("Branch %s", branchName)
	filtered := make([]string, 0, len(messages))
	for _, msg := range messages {
		if strings.Contains(msg, "Branch ") {
			if strings.Contains(msg, needle) {
				filtered = append(filtered, msg)
			}
			continue
		}
		filtered = append(filtered, msg)
	}
	return filtered
}

func formatMergeNextPlan(plan *mergeAction.Plan, validation *mergeAction.PlanValidation, wait bool) string {
	var result strings.Builder

	result.WriteString("Merge Strategy: next\n")
	if plan == nil {
		result.WriteString("Current Branch: (unknown)\n\n")
		result.WriteString("Merge Plan:\n")
		result.WriteString("  (no branches to merge)\n")
		return result.String()
	}
	fmt.Fprintf(&result, "Current Branch: %s\n", plan.CurrentBranch)
	result.WriteString("\n")

	mergeAction.FormatValidationSection(&result, validation)

	result.WriteString("Merge Plan:\n")
	if len(plan.BranchesToMerge) == 0 {
		result.WriteString("  (no branches to merge)\n")
		return result.String()
	}

	bottom := plan.BranchesToMerge[0]
	step := 1
	if wait {
		fmt.Fprintf(&result, "  %d. Merge PR #%d (%s)\n", step, bottom.PRNumber, bottom.BranchName)
	} else {
		fmt.Fprintf(&result, "  %d. Enable automerge for PR #%d (%s)\n", step, bottom.PRNumber, bottom.BranchName)
	}
	step++

	remaining := len(plan.BranchesToMerge) - 1 + len(plan.UpstackBranches)
	if wait {
		fmt.Fprintf(&result, "  %d. Wait for merge to complete\n", step)
		step++
		if remaining > 0 {
			fmt.Fprintf(&result, "  %d. Sync trunk and restack %d remaining branches\n", step, remaining)
		} else {
			fmt.Fprintf(&result, "  %d. Sync trunk\n", step)
		}
	} else {
		if remaining > 0 {
			fmt.Fprintf(&result, "  %d. After merge, Restack %d remaining branches (run 'stackit sync --restack')\n", step, remaining)
		} else {
			fmt.Fprintf(&result, "  %d. After merge, sync trunk (run 'stackit sync --restack')\n", step)
		}
	}

	return result.String()
}
