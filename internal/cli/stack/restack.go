// Package stack provides CLI commands for operating on entire stacks.
package stack

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/cli/common"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/handlers"
	"github.com/getstackit/stackit/internal/tui"
	"github.com/getstackit/stackit/internal/tui/style"
)

// NewRestackCmd creates the restack command
func NewRestackCmd() *cobra.Command {
	var (
		branch             string
		downstack          bool
		only               bool
		upstack            bool
		allStacks          bool
		stacks             []string
		continueOnConflict bool
		parallel           bool
		jobs               int
		jsonOutput         bool
	)

	cmd := &cobra.Command{
		Use:   "restack",
		Short: "Ensure each branch in the current stack has its parent in its Git commit history, rebasing if necessary",
		Long: `Ensure each branch in the current stack has its parent in its Git commit history, rebasing if necessary.
If conflicts are encountered, you will be prompted to resolve them via an interactive Git rebase.`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return common.Run(cmd, func(ctx *app.Context) error {
				// Validation: only one scope flag at a time
				scopeFlags := 0
				if downstack {
					scopeFlags++
				}
				if only {
					scopeFlags++
				}
				if upstack {
					scopeFlags++
				}
				if scopeFlags > 1 {
					return fmt.Errorf("only one of --downstack, --only, or --upstack can be specified")
				}
				multiStack := allStacks || len(stacks) > 0
				if allStacks && len(stacks) > 0 {
					return fmt.Errorf("only one of --all-stacks or --stacks can be specified")
				}
				if multiStack && branch != "" {
					return fmt.Errorf("--branch cannot be used with --all-stacks or --stacks")
				}
				if multiStack && scopeFlags > 0 {
					return fmt.Errorf("--downstack, --only, and --upstack cannot be used with --all-stacks or --stacks")
				}
				// --jobs implies --parallel
				if jobs > 0 {
					parallel = true
				}
				if parallel && !multiStack {
					return fmt.Errorf("--parallel requires --all-stacks or --stacks")
				}

				// Determine target branch
				targetBranch := branch
				if targetBranch == "" && !multiStack {
					currentBranch := ctx.Engine.CurrentBranch()
					if currentBranch == nil {
						return fmt.Errorf("not on a branch and --branch not specified")
					}
					targetBranch = currentBranch.GetName()
				}

				// Determine scope based on flags
				rng := engine.StackRange{
					RecursiveParents:  !only && !upstack,   // Default or downstack
					IncludeCurrent:    true,                // Always include current
					RecursiveChildren: !only && !downstack, // Default or upstack
				}

				opts := actions.RestackOptions{
					BranchName:         targetBranch,
					Scope:              rng,
					AllStacks:          allStacks,
					StackRoots:         stacks,
					ContinueOnConflict: continueOnConflict,
					Parallel:           parallel,
					Jobs:               jobs,
				}

				// Build the plan once. RestackAction reuses it so we don't
				// pay for engine.PlanRestack twice (~3 git ops per branch).
				plan, err := actions.PlanRestack(ctx, opts)
				if err != nil {
					return err
				}

				// JSON output mode
				if jsonOutput {
					cmd.SilenceErrors = true
					jsonHandler := handlers.NewJSONRestackHandler()
					err := actions.RestackAction(ctx, plan, jsonHandler)

					// Set error status if there was an error
					if err != nil {
						jsonHandler.SetError(err)
					}

					// Output JSON (includes error info if there was one)
					data, marshalErr := json.MarshalIndent(jsonHandler.Result, "", "  ")
					if marshalErr != nil {
						return fmt.Errorf("failed to marshal JSON: %w", marshalErr)
					}
					ctx.Output.Info("%s", string(data))

					// Return the error so exit code is non-zero on failure
					// (JSON output still contains full error details)
					return err
				}

				// Skip the TUI when the scope is empty — the runner would set
				// output to quiet, suppress the message, and only flash
				// bubbletea startup/teardown escape codes. A scope emptied by
				// worktree holds is incomplete, so the simple handler below
				// reports it instead.
				if !plan.HasBranches() && !plan.HasHolds() {
					ctx.Output.Info("No branches to restack.")
					return nil
				}

				// If every branch is already up-to-date, restack is a no-op
				// that only streams "up to date" lines. Skip the bubbletea TUI
				// in that case so its startup/teardown queries (kitty keyboard,
				// modifyOtherKeys, mode reports) don't leak as garbled text on
				// terminals that don't recognize them.
				if !plan.HasWork() {
					return actions.RestackAction(ctx, plan, NewSimpleSyncHandler(ctx.Output))
				}

				if tui.IsTTY() {
					scope := RestackScope{
						Target:         targetBranch,
						Trunk:          ctx.Engine.Trunk().GetName(),
						ExplicitBranch: branch != "",
						Only:           only,
						Upstack:        upstack,
						Downstack:      downstack,
						AllStacks:      allStacks,
						Stacks:         stacks,
					}
					ctx.Output.Info("%s", RestackHeadline(scope, plan.BranchCount()))
				}

				// Create runner (manages terminal state) and handler (processes events)
				runner, handler := NewSyncUI(ctx.Output, ctx.Logger)
				defer runner.Cleanup()

				return actions.RestackAction(ctx, plan, handler)
			})
		},
	}

	cmd.Flags().StringVar(&branch, "branch", "", "Which branch to run this command from. Defaults to the current branch.")
	cmd.Flags().BoolVar(&downstack, "downstack", false, "Only restack this branch and its ancestors.")
	cmd.Flags().BoolVar(&only, "only", false, "Only restack this branch.")
	cmd.Flags().BoolVar(&upstack, "upstack", false, "Only restack this branch and its descendants.")
	cmd.Flags().BoolVar(&allStacks, "all-stacks", false, "Restack every independent stack rooted at trunk.")
	cmd.Flags().StringSliceVar(&stacks, "stacks", nil, "Restack specific independent stack roots (comma-separated).")
	cmd.Flags().BoolVar(&continueOnConflict, "continue-on-conflict", false, "Report restack conflicts without entering conflict resolution, continuing to independent stacks when possible.")
	cmd.Flags().BoolVarP(&parallel, "parallel", "p", false, "Run independent stack groups in parallel worktrees (requires --all-stacks or --stacks).")
	cmd.Flags().IntVarP(&jobs, "jobs", "j", 0, "Number of parallel jobs (default: number of CPUs). Implies --parallel.")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output results in JSON format.")

	return cmd
}

// RestackScope describes which branches a restack invocation targets, as the
// flags resolved them. It only feeds the interactive headline.
type RestackScope struct {
	Target         string   // resolved target branch; empty for multi-stack
	Trunk          string   // trunk branch name
	ExplicitBranch bool     // target came from --branch
	Only           bool     // --only
	Upstack        bool     // --upstack
	Downstack      bool     // --downstack
	AllStacks      bool     // --all-stacks
	Stacks         []string // --stacks roots
}

// Label names the restack scope for humans, e.g. "current stack" or
// "upstack from feat/api".
func (s RestackScope) Label() string {
	switch {
	case s.AllStacks:
		return "all stacks"
	case len(s.Stacks) == 1:
		return "stack " + style.DisplayBranchName(s.Stacks[0])
	case len(s.Stacks) > 1:
		names := make([]string, len(s.Stacks))
		for i, root := range s.Stacks {
			names[i] = style.DisplayBranchName(root)
		}
		return "stacks " + strings.Join(names, ", ")
	case s.Only:
		return style.DisplayBranchName(s.Target)
	case s.Upstack:
		return "upstack from " + style.DisplayBranchName(s.Target)
	case s.Downstack:
		return "downstack from " + style.DisplayBranchName(s.Target)
	case s.Target != "" && s.Target == s.Trunk:
		// Trunk's "stack" is every trunk-rooted branch.
		return "all stacks"
	case s.ExplicitBranch:
		return "stack containing " + style.DisplayBranchName(s.Target)
	default:
		return "current stack"
	}
}

// RestackHeadline is the line printed before the interactive restack UI.
func RestackHeadline(scope RestackScope, branchCount int) string {
	return fmt.Sprintf("Restacking %s · %d %s", scope.Label(), branchCount, pluralizeBranches(branchCount))
}
