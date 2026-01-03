package stack

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"

	"stackit.dev/stackit/internal/actions/foreach"
	"stackit.dev/stackit/internal/tui"
	foreachComponent "stackit.dev/stackit/internal/tui/components/foreach"
	"stackit.dev/stackit/internal/tui/components/tree"
	"stackit.dev/stackit/internal/tui/style"
)

// NewForeachHandler creates the appropriate handler based on TTY availability
func NewForeachHandler(splog *tui.Splog, parallel bool) foreach.Handler {
	if tui.IsTTY() {
		return NewInteractiveForeachHandler(splog)
	}
	return NewSimpleForeachHandler(splog, parallel)
}

// SimpleForeachHandler implements foreach.Handler with line-by-line output
type SimpleForeachHandler struct {
	splog         *tui.Splog
	out           io.Writer
	items         map[string]*foreachBranchItem
	mu            sync.Mutex
	started       bool
	currentBranch string
	parallel      bool
}

type foreachBranchItem struct {
	name string
}

// NewSimpleForeachHandler creates a new simple foreach handler
func NewSimpleForeachHandler(splog *tui.Splog, parallel bool) *SimpleForeachHandler {
	return &SimpleForeachHandler{
		splog:    splog,
		out:      os.Stdout,
		items:    make(map[string]*foreachBranchItem),
		parallel: parallel,
	}
}

// OnEvent handles events from the foreach action
func (h *SimpleForeachHandler) OnEvent(e foreach.Event) {
	h.mu.Lock()
	defer h.mu.Unlock()

	switch ev := e.(type) {
	case foreach.StackDisplayEvent:
		// Store current branch for styling
		h.currentBranch = ev.Stack.CurrentBranch
		// Don't display stack in simple mode - we'll show progress per branch

	case foreach.ExecutionStartEvent:
		h.started = true
		for _, branch := range ev.Branches {
			h.items[branch.Name] = &foreachBranchItem{
				name: branch.Name,
			}
		}
		if h.parallel {
			h.splog.Page("Executing in parallel: ")
		}

	case foreach.BranchProgressEvent:
		// In parallel mode, we show progress as dots to maintain some visual feedback
		// while keeping the output deterministic for the final summary.
		if h.parallel {
			if ev.Status == foreach.StatusDone || ev.Status == foreach.StatusError {
				h.splog.Page(".")
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
			h.splog.Info("\nRunning on branch %s...", style.ColorBranchName(ev.BranchName, isCurrent))

		case foreach.StatusDone:
			// In sequential mode, we've already printed "Running on branch..."
			// Just show the output and completion status.
			output := strings.TrimSpace(ev.Output)
			if len(output) > 0 {
				h.splog.Info("%s", strings.TrimSuffix(output, "\n"))
			}
			h.splog.Info("✓ Command succeeded on branch %s", style.ColorBranchName(ev.BranchName, isCurrent))

		case foreach.StatusError:
			// In sequential mode, we've already printed "Running on branch..."
			// Just show the output and error status.
			output := strings.TrimSpace(ev.Output)
			if len(output) > 0 {
				h.splog.Info("%s", strings.TrimSuffix(output, "\n"))
			}
			h.splog.Error("✗ Command failed on branch %s: %v", style.ColorBranchName(ev.BranchName, isCurrent), ev.Error)
		}

	case foreach.CompletionEvent:
		// Show consolidated output summary
		if len(ev.Results) > 0 {
			h.splog.Newline()
			h.splog.Info("Summary:")
			successCount := 0
			failCount := 0
			for _, result := range ev.Results {
				switch result.Status {
				case foreach.StatusDone:
					successCount++
					h.splog.Info("  ✓ %s", style.ColorBranchName(result.BranchName, result.BranchName == h.currentBranch))
					if result.Output != "" {
						output := strings.TrimSpace(result.Output)
						if len(output) > 0 {
							// Indent output
							lines := strings.Split(output, "\n")
							for _, line := range lines {
								if strings.TrimSpace(line) != "" {
									h.splog.Info("    %s", line)
								}
							}
						}
					}
				case foreach.StatusError:
					failCount++
					h.splog.Error("  ✗ %s", style.ColorBranchName(result.BranchName, result.BranchName == h.currentBranch))
					if result.Error != nil {
						h.splog.Error("    Error: %v", result.Error)
					}
					if result.Output != "" {
						output := strings.TrimSpace(result.Output)
						if len(output) > 0 {
							// Indent output
							lines := strings.Split(output, "\n")
							for _, line := range lines {
								if strings.TrimSpace(line) != "" {
									h.splog.Info("    %s", line)
								}
							}
						}
					}
				}
			}
			h.splog.Newline()
			if failCount > 0 {
				h.splog.Info("Completed: %d succeeded, %d failed", successCount, failCount)
			} else {
				h.splog.Info("All branches completed successfully (%d total)", successCount)
			}
		} else if !ev.Success && ev.Message != "" {
			h.splog.Info("%s", ev.Message)
		}
	}
}

// InteractiveForeachHandler implements foreach.Handler with bubbletea for animated progress
type InteractiveForeachHandler struct {
	splog       *tui.Splog
	program     *tea.Program
	model       *foreachComponent.Model
	inExecPhase bool
	mu          sync.Mutex
	stack       *tree.StackTree
	command     string
}

// NewInteractiveForeachHandler creates a new interactive foreach handler
func NewInteractiveForeachHandler(splog *tui.Splog) *InteractiveForeachHandler {
	return &InteractiveForeachHandler{splog: splog}
}

// findRootBranch finds the root branch of the stack (the one whose parent is trunk)
func (h *InteractiveForeachHandler) findRootBranch() string {
	if h.stack == nil || len(h.stack.Branches) == 0 {
		return ""
	}

	// If we're on the trunk branch, show everything from trunk down
	if h.stack.CurrentBranch == h.stack.TrunkBranch {
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

func (h *InteractiveForeachHandler) ensureProgramStarted() {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.program != nil {
		return
	}

	// Initialize model if needed
	if h.model == nil {
		h.model = foreachComponent.NewModel(nil)
	}

	// Quiet the splog so it doesn't interfere with the TUI
	h.splog.SetQuiet(true)

	h.program = tea.NewProgram(h.model, tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout))

	// Run program in background
	go func() {
		if _, err := h.program.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error running foreach TUI: %v\n", err)
		}
	}()
}

// OnEvent handles events from the foreach action
func (h *InteractiveForeachHandler) OnEvent(e foreach.Event) {
	switch ev := e.(type) {
	case foreach.StackDisplayEvent:
		h.stack = ev.Stack
		h.command = ev.Command

		// Create model with tree renderer
		h.model = foreachComponent.NewModel(nil)
		h.model.Renderer = ev.Stack.ToRenderer()
		h.model.RootBranch = h.findRootBranch()
		h.model.Command = h.command

		h.ensureProgramStarted()

	case foreach.ExecutionStartEvent:
		h.inExecPhase = true
		h.ensureProgramStarted()

		// Update items in the model
		for _, branch := range ev.Branches {
			item := foreachComponent.Item{
				BranchName: branch.Name,
				Status:     "pending",
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

		h.program.Send(foreachComponent.GlobalMessageMsg("Executing..."))

	case foreach.BranchProgressEvent:
		if !h.inExecPhase || h.program == nil {
			return
		}

		status := string(ev.Status)
		h.program.Send(foreachComponent.ProgressUpdateMsg{
			BranchName: ev.BranchName,
			Status:     status,
			Output:     ev.Output,
			Err:        ev.Error,
		})

	case foreach.CompletionEvent:
		if h.program == nil {
			return
		}

		if ev.Message != "" && ev.Message != "Execution complete" {
			h.program.Send(foreachComponent.GlobalMessageMsg(ev.Message))
		} else {
			h.program.Send(foreachComponent.GlobalMessageMsg(""))
		}
		h.program.Send(foreachComponent.ProgressCompleteMsg{})
		h.complete(ev.Results)
	}
}

// complete finalizes the display and shows consolidated output
func (h *InteractiveForeachHandler) complete(results []foreach.BranchResult) {
	if h.program != nil {
		h.program.Wait()
		h.program = nil
		h.splog.SetQuiet(false)
	}

	// Show consolidated output summary
	if len(results) > 0 {
		h.splog.Newline()
		h.splog.Info("Summary:")
		successCount := 0
		failCount := 0
		for _, result := range results {
			switch result.Status {
			case foreach.StatusDone:
				successCount++
				isCurrent := h.stack != nil && result.BranchName == h.stack.CurrentBranch
				h.splog.Info("  ✓ %s", style.ColorBranchName(result.BranchName, isCurrent))
				if result.Output != "" {
					output := strings.TrimSpace(result.Output)
					if len(output) > 0 {
						// Indent output
						lines := strings.Split(output, "\n")
						for _, line := range lines {
							if strings.TrimSpace(line) != "" {
								h.splog.Info("    %s", line)
							}
						}
					}
				}
			case foreach.StatusError:
				failCount++
				isCurrent := h.stack != nil && result.BranchName == h.stack.CurrentBranch
				h.splog.Error("  ✗ %s", style.ColorBranchName(result.BranchName, isCurrent))
				if result.Error != nil {
					h.splog.Error("    Error: %v", result.Error)
				}
				if result.Output != "" {
					output := strings.TrimSpace(result.Output)
					if len(output) > 0 {
						// Indent output
						lines := strings.Split(output, "\n")
						for _, line := range lines {
							if strings.TrimSpace(line) != "" {
								h.splog.Info("    %s", line)
							}
						}
					}
				}
			}
		}
		h.splog.Newline()
		if failCount > 0 {
			h.splog.Info("Completed: %d succeeded, %d failed", successCount, failCount)
		} else {
			h.splog.Info("All branches completed successfully (%d total)", successCount)
		}
	}
}
