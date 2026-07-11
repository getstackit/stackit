package stack

import (
	"strings"

	"github.com/getstackit/stackit/internal/actions/foreach"
	"github.com/getstackit/stackit/internal/cli/common"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/tui"
	foreachComponent "github.com/getstackit/stackit/internal/tui/components/foreach"
	"github.com/getstackit/stackit/internal/tui/components/tree"
	"github.com/getstackit/stackit/internal/tui/core"
	"github.com/getstackit/stackit/internal/tui/style"
)

// NewForeachUI creates a runner and handler pair for foreach operations.
// The runner manages terminal state; the handler processes events.
// Caller must defer runner.Cleanup() to restore terminal on exit.
func NewForeachUI(out output.Output, logger output.Logger, parallel bool) (*tui.Runner, foreach.Handler) {
	// Parallel execution runs arbitrary commands concurrently and has proven
	// fragile with a raw-mode TUI. Use plain output for stable terminal behavior.
	if parallel {
		return nil, NewSimpleForeachHandler(out, true)
	}

	if tui.IsTTY() {
		model := foreachComponent.NewModel(nil)
		runner := tui.NewRunner(model, out, logger)
		runner.Start()
		return runner, NewInteractiveForeachHandler(runner, model, out)
	}
	return nil, NewSimpleForeachHandler(out, parallel)
}

// SimpleForeachHandler implements foreach.Handler with line-by-line output
type SimpleForeachHandler struct {
	common.BaseHandler
	items         map[string]*foreachBranchItem
	started       bool
	currentBranch string
	parallel      bool
}

type foreachBranchItem struct {
	name string
}

// NewSimpleForeachHandler creates a new simple foreach handler
func NewSimpleForeachHandler(out output.Output, parallel bool) *SimpleForeachHandler {
	return &SimpleForeachHandler{
		BaseHandler: common.NewBaseHandler(out),
		items:       make(map[string]*foreachBranchItem),
		parallel:    parallel,
	}
}

// OnEvent handles events from the foreach action
func (h *SimpleForeachHandler) OnEvent(e foreach.Event) {
	h.Lock()
	defer h.Unlock()

	switch ev := e.(type) {
	case foreach.StackDisplayEvent:
		// Store current branch for styling
		h.currentBranch = ev.Stack.CurrentBranch()
		// Don't display stack in simple mode - we'll show progress per branch

	case foreach.ExecutionStartEvent:
		h.started = true
		for _, branch := range ev.Branches {
			h.items[branch.Name] = &foreachBranchItem{
				name: branch.Name,
			}
		}
		if h.parallel {
			h.Output.Print("Executing in parallel: ")
		}

	case foreach.BranchProgressEvent:
		// In parallel mode, we show progress as dots to maintain some visual feedback
		// while keeping the output deterministic for the final summary.
		if h.parallel {
			if ev.Status == foreach.StatusDone || ev.Status == foreach.StatusError {
				h.Output.Print(".")
			}
			return
		}

		item := h.items[ev.BranchName]
		if item == nil {
			return
		}

		isCurrent := ev.BranchName == h.currentBranch
		switch ev.Status {
		case foreach.StatusRunning:
			// Sequential mode - show "Running on branch..."
			h.Output.Info("\nRunning on branch %s...", style.ColorBranchNameIf(ev.BranchName, isCurrent))

		case foreach.StatusDone:
			// In sequential mode, we've already printed "Running on branch..."
			// Just show the output and completion status.
			output := strings.TrimSpace(ev.Output)
			if len(output) > 0 {
				h.Output.Info("%s", strings.TrimSuffix(output, "\n"))
			}
			h.Output.Info("✓ Command succeeded on branch %s", style.ColorBranchNameIf(ev.BranchName, isCurrent))

		case foreach.StatusError:
			// In sequential mode, we've already printed "Running on branch..."
			// Just show the output and error status.
			output := strings.TrimSpace(ev.Output)
			if len(output) > 0 {
				h.Output.Info("%s", strings.TrimSuffix(output, "\n"))
			}
			h.Output.Error("✗ Command failed on branch %s: %v", style.ColorBranchNameIf(ev.BranchName, isCurrent), ev.Error)
		}

	case foreach.CompletionEvent:
		// Show consolidated output summary
		if len(ev.Results) > 0 {
			h.Output.Newline()
			h.Output.Info("Summary:")
			results := convertToCommonResults(ev.Results, func(name string) bool { return name == h.currentBranch })
			successCount, failCount := common.FormatBranchSummary(h.Output, results)
			h.Output.Newline()
			if failCount > 0 {
				h.Output.Info("Completed: %d succeeded, %d failed", successCount, failCount)
			} else {
				h.Output.Info("All branches completed successfully (%d total)", successCount)
			}
		} else if !ev.Success && ev.Message != "" {
			h.Output.Info("%s", ev.Message)
		}
	}
}

// branchStatusToCoreStatus converts action's BranchStatus to TUI's core.Status.
func branchStatusToCoreStatus(status foreach.BranchStatus) core.Status {
	switch status {
	case foreach.StatusRunning:
		return core.StatusActive
	case foreach.StatusDone:
		return core.StatusDone
	case foreach.StatusError:
		return core.StatusError
	case foreach.StatusSkipped:
		return core.StatusSkipped
	default:
		return core.StatusPending
	}
}

// convertToCommonResults converts foreach.BranchResult slice to common.BranchResult slice.
// The isCurrentFn callback determines if each branch is the current branch.
func convertToCommonResults(results []foreach.BranchResult, isCurrentFn func(branchName string) bool) []common.BranchResult {
	commonResults := make([]common.BranchResult, len(results))
	for i, r := range results {
		var status common.BranchResultStatus
		if r.Status == foreach.StatusDone {
			status = common.StatusDone
		} else {
			status = common.StatusError
		}
		commonResults[i] = common.BranchResult{
			BranchName: r.BranchName,
			Status:     status,
			Output:     r.Output,
			Error:      r.Error,
			IsCurrent:  isCurrentFn(r.BranchName),
		}
	}
	return commonResults
}

// InteractiveForeachHandler implements foreach.Handler with bubbletea for animated progress
type InteractiveForeachHandler struct {
	out         output.Output
	runner      *tui.Runner
	model       *foreachComponent.Model
	inExecPhase bool
	stack       *tree.StackTree
	command     string
}

// NewInteractiveForeachHandler creates a new interactive foreach handler
func NewInteractiveForeachHandler(runner *tui.Runner, model *foreachComponent.Model, out output.Output) *InteractiveForeachHandler {
	return &InteractiveForeachHandler{runner: runner, model: model, out: out}
}

// findRootBranch finds the root branch of the stack (the one whose parent is trunk)
func (h *InteractiveForeachHandler) findRootBranch() string {
	if h.stack == nil || len(h.stack.Branches) == 0 {
		return ""
	}

	// If we're on the trunk branch, show everything from trunk down
	if h.stack.CurrentBranch() == h.stack.TrunkBranch {
		return h.stack.TrunkBranch
	}

	// The root is the branch whose parent is trunk
	for _, branch := range h.stack.Branches {
		parent := h.stack.ParentMap[branch]
		if parent == h.stack.TrunkBranch {
			return branch
		}
	}
	// Fallback to first branch
	return h.stack.Branches[0]
}

// OnEvent handles events from the foreach action
func (h *InteractiveForeachHandler) OnEvent(e foreach.Event) {
	switch ev := e.(type) {
	case foreach.StackDisplayEvent:
		h.stack = ev.Stack
		h.command = ev.Command

		// Update model with tree renderer
		h.model.Renderer = ev.Stack.ToRenderer()
		h.model.RootBranch = h.findRootBranch()
		h.model.Command = h.command

	case foreach.ExecutionStartEvent:
		h.inExecPhase = true

		// Update items in the model
		for _, branch := range ev.Branches {
			item := foreachComponent.Item{
				BranchName: branch.Name,
				Status:     core.StatusPending,
			}
			found := false
			for i, existing := range h.model.Items {
				if existing.BranchName == branch.Name {
					h.model.Items[i] = item
					found = true
					break
				}
			}
			if !found {
				h.model.Items = append(h.model.Items, item)
			}
		}

		h.runner.Send(foreachComponent.GlobalMessageMsg("Executing..."))

	case foreach.BranchProgressEvent:
		if !h.inExecPhase {
			return
		}

		h.runner.Send(foreachComponent.ProgressUpdateMsg{
			BranchName: ev.BranchName,
			Status:     branchStatusToCoreStatus(ev.Status),
			Output:     ev.Output,
			Err:        ev.Error,
		})

	case foreach.CompletionEvent:
		if ev.Message != "" && ev.Message != "Execution complete" {
			h.runner.Send(foreachComponent.GlobalMessageMsg(ev.Message))
		} else {
			h.runner.Send(foreachComponent.GlobalMessageMsg(""))
		}
		h.runner.Send(foreachComponent.ProgressCompleteMsg{})
		// Ensure terminal is restored (raw mode disabled, output unmuted)
		// before printing final summary lines.
		h.runner.Cleanup()
		h.printSummary(ev.Results)
	}
}

// printSummary shows consolidated output after TUI completes
func (h *InteractiveForeachHandler) printSummary(results []foreach.BranchResult) {
	if len(results) == 0 {
		return
	}

	h.out.Newline()
	h.out.Info("Summary:")
	isCurrentFn := func(name string) bool {
		return h.stack != nil && name == h.stack.CurrentBranch()
	}
	commonResults := convertToCommonResults(results, isCurrentFn)
	successCount, failCount := common.FormatBranchSummary(h.out, commonResults)
	h.out.Newline()
	if failCount > 0 {
		h.out.Info("Completed: %d succeeded, %d failed", successCount, failCount)
	} else {
		h.out.Info("All branches completed successfully (%d total)", successCount)
	}
}
