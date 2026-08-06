package foreach

import (
	"context"
	stderrors "errors"
	"fmt"
	"os"
	"os/exec"
	stdruntime "runtime"
	"strings"

	"github.com/getstackit/stackit/internal/actions/worktree"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/errors"
	"github.com/getstackit/stackit/internal/tui/components/tree"
	"github.com/getstackit/stackit/internal/utils"
)

const executionCompleteMsg = "Execution complete"

// Options contains options for the foreach command
type Options struct {
	Command          string
	Args             []string
	BranchName       string
	Scope            engine.StackRange
	AllStacks        bool     // Run across every independent stack rooted at trunk
	StackRoots       []string // Run across the listed independent stack roots only
	FailFast         bool
	Parallel         bool
	FindFirstFailure bool
	Jobs             int
}

// HasForeachWork reports whether Action would have at least one non-trunk
// branch to execute the command on. Use this from the CLI to gate TUI
// initialization — when the answer is no, starting the bubbletea runner
// only flashes startup/teardown escape codes and races with the deferred
// Cleanup before the "no branches to process" CompletionEvent can render.
func HasForeachWork(ctx *app.Context, opts Options) (bool, error) {
	eng := ctx.Engine
	multiStack := opts.AllStacks || len(opts.StackRoots) > 0

	currentBranch := eng.CurrentBranch()
	if !multiStack && currentBranch == nil && opts.BranchName == "" {
		return false, errors.ErrNotOnBranch
	}

	var branches engine.Branches
	if multiStack {
		multiStackBranches, err := collectMultiStackBranches(eng, opts)
		if err != nil {
			return false, err
		}
		branches = multiStackBranches
	} else {
		targetBranch := engine.Branch{}
		if opts.BranchName != "" {
			targetBranch = eng.GetBranch(opts.BranchName)
			if !targetBranch.IsTrunk() && !targetBranch.IsTracked() {
				return false, fmt.Errorf("branch %s is not tracked by stackit", opts.BranchName)
			}
		} else if currentBranch != nil {
			targetBranch = *currentBranch
		}
		graph := eng.Graph(engine.SortStrategyAlphabetical)
		branches = graph.Range(targetBranch, opts.Scope)
	}

	for _, branch := range branches {
		if !branch.IsTrunk() {
			return true, nil
		}
	}
	return false, nil
}

// Action executes a command on each branch in the stack with event handling
func Action(ctx *app.Context, opts Options, handler Handler) error {
	eng := ctx.Engine

	multiStack := opts.AllStacks || len(opts.StackRoots) > 0

	currentBranch := eng.CurrentBranch()
	if !multiStack && currentBranch == nil && opts.BranchName == "" {
		return errors.ErrNotOnBranch
	}

	// Build graph once — needed for scope range and for find-first-failure depth grouping.
	graph := eng.Graph(engine.SortStrategyAlphabetical)

	var branches engine.Branches
	if multiStack {
		multiStackBranches, err := collectMultiStackBranches(eng, opts)
		if err != nil {
			return err
		}
		branches = multiStackBranches
	} else {
		targetBranch := engine.Branch{}
		if opts.BranchName != "" {
			targetBranch = eng.GetBranch(opts.BranchName)
			if !targetBranch.IsTrunk() && !targetBranch.IsTracked() {
				return fmt.Errorf("branch %s is not tracked by stackit", opts.BranchName)
			}
		} else if currentBranch != nil {
			targetBranch = *currentBranch
		}
		branches = graph.Range(targetBranch, opts.Scope)
	}

	if len(branches) == 0 {
		handler.OnEvent(CompletionEvent{Success: true, Message: "No branches to process"})
		return nil
	}

	// Filter out trunk branches
	nonTrunkBranches := branches.WithoutTrunk()

	if len(nonTrunkBranches) == 0 {
		handler.OnEvent(CompletionEvent{Success: true, Message: "No branches to process"})
		return nil
	}

	// Build tree structure for display
	currentBranchName := ""
	if currentBranch != nil {
		currentBranchName = currentBranch.GetName()
	}
	stackTree := tree.NewStackTree(nonTrunkBranches.All(), currentBranchName, eng.Trunk().GetName())

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

	if opts.FindFirstFailure {
		return foreachFindFirstFailure(ctx, opts, nonTrunkBranches, graph, handler)
	}
	if opts.Parallel {
		return foreachParallel(ctx, opts, nonTrunkBranches, handler)
	}
	return foreachSequential(ctx, opts, nonTrunkBranches, handler)
}

func foreachSequential(ctx *app.Context, opts Options, branches engine.Branches, handler Handler) error {
	eng := ctx.Engine
	out := ctx.Output

	currentBranch := eng.CurrentBranch()
	originalBranchName := ""
	if currentBranch != nil {
		originalBranchName = currentBranch.GetName()
	}

	defer func() {
		// Always try to return to the original branch
		if originalBranchName != "" {
			if err := eng.CheckoutBranch(ctx.Context, eng.GetBranch(originalBranchName)); err != nil {
				out.Error("Failed to return to original branch %s: %v", originalBranchName, err)
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
			result.ExitCode = -1
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
			result.ExitCode = exitCodeFromError(err)
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
			result.ExitCode = 0
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
			Message: executionCompleteMsg,
			Results: allResults,
		})
		return nil
	}

	handler.OnEvent(CompletionEvent{
		Success: true,
		Message: executionCompleteMsg,
		Results: allResults,
	})
	return nil
}

func foreachParallel(ctx *app.Context, opts Options, branches engine.Branches, handler Handler) error {
	fullCommand, approvedHooks, numJobs := prepareWorktreeRun(ctx, opts)
	sortedResults := runBranchesInWorktrees(ctx, opts, branches, handler, fullCommand, approvedHooks, numJobs)

	if hasFailedResult(sortedResults) {
		if !opts.FailFast {
			handler.OnEvent(CompletionEvent{
				Success: true,
				Message: executionCompleteMsg,
				Results: sortedResults,
			})
			return nil
		}
		handler.OnEvent(CompletionEvent{
			Success: false,
			Message: "Command failed on one or more branches",
			Results: sortedResults,
		})
		return fmt.Errorf("command failed on one or more branches")
	}

	handler.OnEvent(CompletionEvent{
		Success: true,
		Message: executionCompleteMsg,
		Results: sortedResults,
	})
	return nil
}

func prepareWorktreeRun(ctx *app.Context, opts Options) (string, []string, int) {
	numJobs := opts.Jobs
	if numJobs <= 0 {
		numJobs = stdruntime.NumCPU()
	}

	// Prune stale worktree entries ONCE before starting parallel execution.
	// This prevents "failed to read commondir" errors caused by incomplete cleanup
	// from previous operations, without interfering with parallel worktree creation.
	_ = ctx.Engine.PruneWorktrees(ctx.Context)

	// Resolve post-worktree-create hooks once on the main thread (may prompt user).
	// The resolved list is then executed in each worktree without prompting.
	approvedHooks, err := worktree.ResolveApprovedHooks(ctx)
	if err != nil {
		ctx.Output.Warn("Failed to resolve worktree hooks: %v", err)
	}

	fullCommand := strings.Join(append([]string{opts.Command}, opts.Args...), " ")
	return fullCommand, approvedHooks, numJobs
}

func foreachFindFirstFailure(ctx *app.Context, opts Options, branches engine.Branches, graph *engine.StackGraph, handler Handler) error {
	fullCommand, approvedHooks, numJobs := prepareWorktreeRun(ctx, opts)
	branchesByDepth := groupBranchesByDepth(branches, graph)

	allResults := make([]BranchResult, 0, len(branches))
	depthOpts := opts
	depthOpts.FailFast = false
	for _, depthGroup := range branchesByDepth {
		groupResults := runBranchesInWorktrees(ctx, depthOpts, depthGroup.branches, handler, fullCommand, approvedHooks, numJobs)
		allResults = append(allResults, groupResults...)

		if hasFailedResult(groupResults) {
			message := fmt.Sprintf("Command failed at stack depth %d", depthGroup.depth)
			handler.OnEvent(CompletionEvent{
				Success: false,
				Message: message,
				Results: allResults,
			})
			return fmt.Errorf("command failed at stack depth %d", depthGroup.depth)
		}
	}

	handler.OnEvent(CompletionEvent{
		Success: true,
		Message: executionCompleteMsg,
		Results: allResults,
	})
	return nil
}

type branchDepthGroup struct {
	depth    int
	branches engine.Branches
}

func groupBranchesByDepth(branches engine.Branches, graph *engine.StackGraph) []branchDepthGroup {
	groups := []branchDepthGroup{}
	groupByDepth := make(map[int]int)
	builders := make(map[int]*engine.BranchesBuilder)
	for _, branch := range branches {
		depth := 0
		if node := graph.GetNode(branch.GetName()); node != nil {
			depth = node.Depth
		}

		if _, ok := groupByDepth[depth]; !ok {
			groupByDepth[depth] = len(groups)
			groups = append(groups, branchDepthGroup{depth: depth})
			builders[depth] = engine.NewBranchesBuilder(len(branches))
		}
		builders[depth].Add(branch)
	}
	for i := range groups {
		groups[i].branches = builders[groups[i].depth].Build()
	}
	return groups
}

func runBranchesInWorktrees(ctx *app.Context, opts Options, branches engine.Branches, handler Handler, fullCommand string, hooks []string, numJobs int) []BranchResult {
	type result struct {
		branchName string
		output     string
		exitCode   int
		err        error
	}

	results := make(chan result, len(branches))
	runCtx, cancel := context.WithCancel(ctx.Context)
	defer cancel()

	utils.RunWithWorkers(branches.All(), numJobs, func(branch engine.Branch) {
		select {
		case <-runCtx.Done():
			return
		default:
		}

		branchName := branch.GetName()
		handler.OnEvent(BranchProgressEvent{
			BranchName: branchName,
			Status:     StatusRunning,
		})

		res := executeCommandOnBranch(runCtx, ctx, branch, fullCommand, hooks)
		if res.err != nil {
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
			exitCode:   res.exitCode,
			err:        res.err,
		}
	})
	close(results)

	resultMap := make(map[string]BranchResult)
	for res := range results {
		status := StatusDone
		if res.err != nil {
			status = StatusError
		}
		resultMap[res.branchName] = BranchResult{
			BranchName: res.branchName,
			Status:     status,
			ExitCode:   res.exitCode,
			Output:     res.output,
			Error:      res.err,
		}
	}

	sortedResults := make([]BranchResult, 0, len(branches))
	for _, branch := range branches {
		if result, ok := resultMap[branch.GetName()]; ok {
			sortedResults = append(sortedResults, result)
		}
	}
	return sortedResults
}

func hasFailedResult(results []BranchResult) bool {
	for _, result := range results {
		if result.Status == StatusError {
			return true
		}
	}
	return false
}

func executeCommandOnBranch(ctx context.Context, appCtx *app.Context, branch engine.Branch, fullCommand string, hooks []string) struct {
	branchName string
	output     string
	exitCode   int
	err        error
} {
	res := struct {
		branchName string
		output     string
		exitCode   int
		err        error
	}{branchName: branch.GetName()}

	// Create a temporary directory for the worktree.
	// Use SkipPrune variant since worktree execution pruned once before parallel execution.
	worktreePath, cleanup, err := appCtx.Engine.CreateTemporaryWorktreeSkipPrune(ctx, branch.GetName(), "stackit-foreach-*")
	if err != nil {
		res.exitCode = -1
		res.err = err
		return res
	}
	defer cleanup()

	// Run post-worktree-create hooks (pre-resolved, no prompting)
	worktree.RunResolvedHooks(appCtx.Context, hooks, worktreePath, appCtx.Output)

	var output strings.Builder
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", fullCommand)
	cmd.Stdout = &output
	cmd.Stderr = &output
	cmd.Dir = worktreePath
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, "STACKIT_BRANCH="+branch.GetName())

	if err := cmd.Run(); err != nil {
		res.exitCode = exitCodeFromError(err)
		res.err = err
	}
	res.output = output.String()
	return res
}

// collectMultiStackBranches expands --all-stacks / --stacks into a flat branch list,
// preserving independent-stack ordering and excluding trunk and untracked branches.
func collectMultiStackBranches(eng engine.BranchReader, opts Options) (engine.Branches, error) {
	stacks := engine.DiscoverIndependentStacks(eng)
	if len(opts.StackRoots) > 0 {
		stackByRoot := make(map[string]engine.IndependentStack, len(stacks))
		for _, stack := range stacks {
			stackByRoot[stack.RootBranch] = stack
		}
		filtered := make([]engine.IndependentStack, 0, len(opts.StackRoots))
		for _, root := range opts.StackRoots {
			stack, ok := stackByRoot[root]
			if !ok {
				return nil, fmt.Errorf("stack root %s not found", root)
			}
			filtered = append(filtered, stack)
		}
		stacks = filtered
	}

	branches := engine.NewBranchesBuilder(len(stacks) * 4)
	for _, stack := range stacks {
		for _, branchName := range stack.Branches {
			branch := eng.GetBranch(branchName)
			if branch.IsTracked() && !branch.IsTrunk() {
				branches.Add(branch)
			}
		}
	}
	return branches.Build(), nil
}

func exitCodeFromError(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if stderrors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}
