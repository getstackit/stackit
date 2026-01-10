package branch

import (
	"sync"

	"stackit.dev/stackit/internal/actions/split"
	"stackit.dev/stackit/internal/output"
	"stackit.dev/stackit/internal/tui"
	"stackit.dev/stackit/internal/tui/style"
)

// NewSplitUI creates a runner and handler pair for split operations.
// The runner manages terminal state; the handler processes events.
// Caller must defer runner.Cleanup() to restore terminal on exit.
// Currently returns nil runner as there's no TUI component yet.
func NewSplitUI(out output.Output, _ output.Logger) (*tui.Runner, split.Handler) {
	// TODO: Add interactive TUI handler when needed
	// For now, use simple handler for both TTY and non-TTY
	return nil, NewSimpleSplitHandler(out)
}

// SimpleSplitHandler provides streaming text output for split operations
type SimpleSplitHandler struct {
	splog       output.Output
	mu          sync.Mutex
	branchName  string
	newBranches []string
}

// NewSimpleSplitHandler creates a new SimpleSplitHandler
func NewSimpleSplitHandler(splog output.Output) *SimpleSplitHandler {
	return &SimpleSplitHandler{
		splog: splog,
	}
}

// Start is called at the beginning of split
func (h *SimpleSplitHandler) Start(branchName string, _ split.Style) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.branchName = branchName
	h.newBranches = nil
}

// OnStep is called for each step in the split process
func (h *SimpleSplitHandler) OnStep(_ split.Step, _ split.StepStatus, _ string) {
	// Steps are handled silently in simple handler
}

// OnBranchCreated is called when a new branch is created during split
func (h *SimpleSplitHandler) OnBranchCreated(branchName string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.newBranches = append(h.newBranches, branchName)
	h.splog.Info("Created branch %s", style.ColorBranchName(branchName, false))
}

// Complete is called when split finishes
func (h *SimpleSplitHandler) Complete(result split.ActionResult) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if len(result.NewBranches) > 0 {
		h.splog.Info("Split %s into %d branches",
			style.ColorBranchName(result.OriginalBranch, false),
			len(result.NewBranches))
	}
}

// Cleanup is a no-op for the simple handler
func (h *SimpleSplitHandler) Cleanup() {}
