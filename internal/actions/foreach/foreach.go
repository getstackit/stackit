package foreach

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	stdruntime "runtime"
	"strings"
	"sync"

	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/tui/components/tree"
	"stackit.dev/stackit/internal/utils"
)

// Options contains options for the foreach command
type Options struct {
	Command  string
	Args     []string
	Scope    engine.StackRange
	FailFast bool
	Parallel bool
	Jobs     int
}

// Action executes a command on each branch in the stack with event handling
func Action(ctx *app.Context, opts Options, handler Handler) error {
	eng := ctx.Engine

	currentBranch := eng.CurrentBranch()
	if currentBranch == nil {
		return fmt.Errorf("not on a branch")
	}

	// Get branches based on scope
	branches := currentBranch.GetRelativeStack(opts.Scope)
	if len(branches) == 0 {
		handler.OnEvent(CompletionEvent{Success: true, Message: "No branches to process"})
		return nil
	}

	// Filter out trunk branches
	nonTrunkBranches := make([]engine.Branch, 0, len(branches))
	for _, branch := range branches {
		if !branch.IsTrunk() {
			nonTrunkBranches = append(nonTrunkBranches, branch)
		}
	}

	if len(nonTrunkBranches) == 0 {
		handler.OnEvent(CompletionEvent{Success: true, Message: "No branches to process"})
		return nil
	}

	// Build tree structure for display
	currentBranchName := currentBranch.GetName()
	stackTree := tree.NewStackTree(nonTrunkBranches, currentBranchName, eng.Trunk().GetName())

	// Display the stack
	fullCommand := strings.Join(append([]string{opts.Command}, opts.Args...), " ")
	handler.OnEvent(StackDisplayEvent{
		Stack:   stackTree,
		Command: fullCommand,
	})

	// Start execution phase
	branchInfos := make([]BranchInfo, len(nonTrunkBranches))
	for i, branch := range nonTrunkBranches {
		branchInfos[i] = BranchInfo{
			Name: branch.GetName(),
		}
	}
	handler.OnEvent(ExecutionStartEvent{Branches: branchInfos})

	if opts.Parallel {
		return foreachParallel(ctx, opts, nonTrunkBranches, handler)
	}
	return foreachSequential(ctx, opts, nonTrunkBranches, handler)
}

func foreachSequential(ctx *app.Context, opts Options, branches []engine.Branch, handler Handler) error {
	eng := ctx.Engine
	splog := ctx.Splog

	currentBranch := eng.CurrentBranch()
	originalBranchName := ""
	if currentBranch != nil {
		originalBranchName = currentBranch.GetName()
	}

	defer func() {
		// Always try to return to the original branch
		if originalBranchName != "" {
			if err := eng.CheckoutBranch(ctx.Context, eng.GetBranch(originalBranchName)); err != nil {
				splog.Error("Failed to return to original branch %s: %v", originalBranchName, err)
			}
		}
	}()

	// Join command and args into a single string for shell execution
	fullCommand := strings.Join(append([]string{opts.Command}, opts.Args...), " ")

	var firstErr error
	allResults := make([]BranchResult, 0, len(branches))
	for _, branch := range branches {
		branchName := branch.GetName()

		// Emit running event
		handler.OnEvent(BranchProgressEvent{
			BranchName: branchName,
			Status:     StatusRunning,
		})

		var result BranchResult
		result.BranchName = branchName

		if err := eng.CheckoutBranch(ctx.Context, branch); err != nil {
			result.Status = StatusError
			result.Error = err
			handler.OnEvent(BranchProgressEvent{
				BranchName: branchName,
				Status:     StatusError,
				Error:      err,
			})
			allResults = append(allResults, result)
			if opts.FailFast {
				handler.OnEvent(CompletionEvent{
					Success: false,
					Message: fmt.Sprintf("Failed to checkout %s", branchName),
					Results: allResults,
				})
				return err
			}
			firstErr = err
			continue
		}

		// Execute the command via shell
		var output strings.Builder
		cmd := exec.CommandContext(ctx.Context, "/bin/sh", "-c", fullCommand)
		cmd.Stdout = &output
		cmd.Stderr = &output
		cmd.Stdin = os.Stdin
		cmd.Dir = ctx.RepoRoot
		cmd.Env = os.Environ()
		cmd.Env = append(cmd.Env, "STACKIT_BRANCH="+branchName)

		err := cmd.Run()
		outputStr := output.String()

		if err != nil {
			result.Status = StatusError
			result.Output = outputStr
			result.Error = err
			handler.OnEvent(BranchProgressEvent{
				BranchName: branchName,
				Status:     StatusError,
				Output:     outputStr,
				Error:      err,
			})
			allResults = append(allResults, result)
			if opts.FailFast {
				handler.OnEvent(CompletionEvent{
					Success: false,
					Message: fmt.Sprintf("Command failed on branch %s", branchName),
					Results: allResults,
				})
				return fmt.Errorf("command failed on branch %s: %w", branchName, err)
			}
			if firstErr == nil {
				firstErr = err
			}
		} else {
			result.Status = StatusDone
			result.Output = outputStr
			handler.OnEvent(BranchProgressEvent{
				BranchName: branchName,
				Status:     StatusDone,
				Output:     outputStr,
			})
			allResults = append(allResults, result)
		}
	}

	if firstErr != nil {
		// Only return error if FailFast is true
		if opts.FailFast {
			handler.OnEvent(CompletionEvent{
				Success: false,
				Message: "Command failed on one or more branches",
				Results: allResults,
			})
			return fmt.Errorf("command failed on one or more branches")
		}
		// With --no-fail-fast, continue and return nil (errors are in results)
		handler.OnEvent(CompletionEvent{
			Success: true,
			Message: "Execution complete",
			Results: allResults,
		})
		return nil
	}

	handler.OnEvent(CompletionEvent{
		Success: true,
		Message: "Execution complete",
		Results: allResults,
	})
	return nil
}

func foreachParallel(ctx *app.Context, opts Options, branches []engine.Branch, handler Handler) error {
	numJobs := opts.Jobs
	if numJobs <= 0 {
		numJobs = stdruntime.NumCPU()
	}

	fullCommand := strings.Join(append([]string{opts.Command}, opts.Args...), " ")

	type result struct {
		branchName string
		output     string
		err        error
	}

	results := make(chan result, len(branches))

	// Context for canceling remaining jobs if fail-fast is on
	runCtx, cancel := context.WithCancel(ctx.Context)
	defer cancel()

	var mu sync.Mutex
	var firstErr error

	utils.RunWithWorkers(branches, numJobs, func(branch engine.Branch) {
		select {
		case <-runCtx.Done():
			return
		default:
		}

		branchName := branch.GetName()

		// Emit running event
		handler.OnEvent(BranchProgressEvent{
			BranchName: branchName,
			Status:     StatusRunning,
		})

		res := executeCommandOnBranch(runCtx, ctx, branch, fullCommand)

		if res.err != nil {
			mu.Lock()
			if firstErr == nil {
				firstErr = res.err
			}
			mu.Unlock()
			if opts.FailFast {
				cancel()
			}

			handler.OnEvent(BranchProgressEvent{
				BranchName: branchName,
				Status:     StatusError,
				Output:     res.output,
				Error:      res.err,
			})
		} else {
			handler.OnEvent(BranchProgressEvent{
				BranchName: branchName,
				Status:     StatusDone,
				Output:     res.output,
			})
		}

		results <- result{
			branchName: res.branchName,
			output:     res.output,
			err:        res.err,
		}
	})
	close(results)

	// Collect all results for consolidated output
	allResults := make([]BranchResult, 0, len(branches))
	for res := range results {
		// Events already handled in goroutines, but collect results for summary
		status := StatusDone
		if res.err != nil {
			status = StatusError
		}
		allResults = append(allResults, BranchResult{
			BranchName: res.branchName,
			Status:     status,
			Output:     res.output,
			Error:      res.err,
		})
	}

	// Sort results to match branch order
	resultMap := make(map[string]BranchResult)
	for _, r := range allResults {
		resultMap[r.BranchName] = r
	}
	sortedResults := make([]BranchResult, 0, len(branches))
	for _, branch := range branches {
		if result, ok := resultMap[branch.GetName()]; ok {
			sortedResults = append(sortedResults, result)
		}
	}

	if firstErr != nil {
		// Only return error if FailFast is true
		if opts.FailFast {
			handler.OnEvent(CompletionEvent{
				Success: false,
				Message: "Command failed on one or more branches",
				Results: sortedResults,
			})
			return fmt.Errorf("command failed on one or more branches")
		}
		// With --no-fail-fast, continue and return nil (errors are in results)
		handler.OnEvent(CompletionEvent{
			Success: true,
			Message: "Execution complete",
			Results: sortedResults,
		})
		return nil
	}

	handler.OnEvent(CompletionEvent{
		Success: true,
		Message: "Execution complete",
		Results: sortedResults,
	})
	return nil
}

func executeCommandOnBranch(ctx context.Context, appCtx *app.Context, branch engine.Branch, fullCommand string) struct {
	branchName string
	output     string
	err        error
} {
	res := struct {
		branchName string
		output     string
		err        error
	}{branchName: branch.GetName()}

	// Create a temporary directory for the worktree
	worktreePath, cleanup, err := appCtx.Engine.CreateTemporaryWorktree(ctx, branch.GetName(), "stackit-foreach-*")
	if err != nil {
		res.err = err
		return res
	}
	defer cleanup()

	var output strings.Builder
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", fullCommand)
	cmd.Stdout = &output
	cmd.Stderr = &output
	cmd.Dir = worktreePath
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, "STACKIT_BRANCH="+branch.GetName())

	if err := cmd.Run(); err != nil {
		res.err = err
	}
	res.output = output.String()
	return res
}
