package stack

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/actions/foreach"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/cli/common"
	"github.com/getstackit/stackit/internal/engine"
)

// NewForeachCmd creates the foreach command
func NewForeachCmd() *cobra.Command {
	var (
		upstack    bool
		downstack  bool
		stack      bool
		branch     string
		allStacks  bool
		stacks     []string
		noFailFast bool
		parallel   bool
		firstFail  bool
		jobs       int
		jsonOutput bool
	)

	cmd := &cobra.Command{
		Use:   "foreach <command> [args...]",
		Short: "Run a shell command on each branch in the stack",
		Long: `Executes a shell command on each branch in the current stack, bottom-up.
The command is executed via /bin/sh -c.

By default, it runs on the current branch and all its descendants (up-stack).
When --parallel is used without an explicit scope, it runs on the whole stack.

Examples:
  st foreach mise run lint
  st foreach --stack 'go test ./... && go build'
  st foreach --downstack go test ./...
  st foreach --parallel mise run test
  st foreach --jobs 4 mise run test
  st foreach --all-stacks --parallel mise run test:fast
  st foreach --stacks featA,featB --parallel mise run test:fast`,
		Args:         cobra.MinimumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Validate multi-stack flags up-front so errors don't surface deep in the action.
			multiStack := allStacks || len(stacks) > 0
			if allStacks && len(stacks) > 0 {
				return fmt.Errorf("only one of --all-stacks or --stacks can be specified")
			}
			if multiStack {
				if branch != "" {
					return fmt.Errorf("--branch cannot be used with --all-stacks or --stacks")
				}
				// --upstack defaults to true, so only reject when a scope flag was explicitly set.
				if cmd.Flags().Changed("upstack") || cmd.Flags().Changed("downstack") || cmd.Flags().Changed("stack") {
					return fmt.Errorf("--upstack, --downstack, and --stack cannot be used with --all-stacks or --stacks")
				}
			}
			if jobs > 0 && !parallel && !firstFail {
				parallel = true
			}

			return common.Run(cmd, func(ctx *app.Context) error {
				opts := foreach.Options{
					Command:          args[0],
					Args:             args[1:],
					BranchName:       branch,
					AllStacks:        allStacks,
					StackRoots:       stacks,
					FailFast:         !noFailFast,
					Parallel:         parallel,
					FindFirstFailure: firstFail,
					Jobs:             jobs,
				}

				// Scope is ignored when running across independent stacks — every
				// branch in each selected stack is included.
				if !multiStack {
					// Define the traversal range
					// If parallel mode is enabled and no explicit scope flags are set, default to --stack
					explicitScopeSet := cmd.Flags().Changed("stack") || cmd.Flags().Changed("downstack") || cmd.Flags().Changed("upstack")
					if parallel && !explicitScopeSet {
						// Parallel mode defaults to entire stack
						opts.Scope = engine.StackRange{
							IncludeCurrent:    true,
							RecursiveParents:  true,
							RecursiveChildren: true,
						}
					} else {
						opts.Scope = engine.StackRange{IncludeCurrent: true}
						switch {
						case stack:
							opts.Scope.RecursiveParents = true
							opts.Scope.RecursiveChildren = true
						case downstack:
							opts.Scope.RecursiveParents = true
						case upstack:
							opts.Scope.RecursiveChildren = true
						}
					}
				}

				if jsonOutput {
					cmd.SilenceErrors = true
					jsonHandler := foreach.NewJSONHandler()
					err := foreach.Action(ctx, opts, jsonHandler)
					if err != nil {
						jsonHandler.SetError(err)
					}

					data, marshalErr := json.MarshalIndent(jsonHandler.Result, "", "  ")
					if marshalErr != nil {
						return fmt.Errorf("failed to marshal JSON: %w", marshalErr)
					}
					ctx.Output.Info("%s", string(data))
					return err
				}

				// Pre-flight: skip the TUI when there's nothing to run on. The
				// runner sets output to quiet, so without this check the
				// "No branches to process" CompletionEvent races with the
				// deferred Cleanup and the user only sees bubbletea
				// startup/teardown codes flash.
				hasWork, err := foreach.HasForeachWork(ctx, opts)
				if err != nil {
					return err
				}
				if !hasWork {
					ctx.Output.Info("No branches to process.")
					return nil
				}

				// Create runner (manages terminal state) and handler (processes events)
				runner, handler := NewForeachUI(ctx.Output, ctx.Logger, opts.Parallel)
				defer runner.Cleanup()
				return foreach.Action(ctx, opts, handler)
			})
		},
	}

	cmd.Flags().BoolVar(&upstack, "upstack", true, "Run on current branch and descendants (default)")
	cmd.Flags().BoolVar(&downstack, "downstack", false, "Run on current branch and ancestors")
	cmd.Flags().BoolVar(&stack, "stack", false, "Run on the entire stack (ancestors and descendants)")
	cmd.Flags().StringVar(&branch, "branch", "", "Which branch to run this command from. Defaults to the current branch.")
	cmd.Flags().BoolVar(&allStacks, "all-stacks", false, "Run on every independent stack rooted at trunk.")
	cmd.Flags().StringSliceVar(&stacks, "stacks", nil, "Run on specific independent stack roots (comma-separated).")
	cmd.Flags().BoolVar(&noFailFast, "no-fail-fast", false, "Don't stop execution on the first failure")
	cmd.Flags().BoolVarP(&parallel, "parallel", "p", false, "Run commands in parallel using git worktrees")
	cmd.Flags().BoolVar(&firstFail, "find-first-failure", false, "Run branches at each stack depth in parallel and stop after the first failing depth")
	cmd.Flags().IntVarP(&jobs, "jobs", "j", 0, "Number of parallel jobs (default: number of CPUs). Implies --parallel unless --find-first-failure is set")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output results in JSON format.")

	return cmd
}
