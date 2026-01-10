package branch

import (
	"sync"

	"stackit.dev/stackit/internal/actions/absorb"
	"stackit.dev/stackit/internal/git"
	"stackit.dev/stackit/internal/output"
	"stackit.dev/stackit/internal/tui"
)

// NewAbsorbUI creates a runner and handler pair for absorb operations.
// The runner manages terminal state; the handler processes events.
// Caller must defer runner.Cleanup() to restore terminal on exit.
// Currently returns nil runner as there's no TUI component yet.
func NewAbsorbUI(out output.Output, _ output.Logger) (*tui.Runner, absorb.Handler) {
	// TODO: Add interactive TUI handler when needed
	// For now, use simple handler for both TTY and non-TTY
	return nil, NewSimpleAbsorbHandler(out)
}

// SimpleAbsorbHandler provides streaming text output for absorb operations
type SimpleAbsorbHandler struct {
	splog    output.Output
	mu       sync.Mutex
	absorbed int
}

// NewSimpleAbsorbHandler creates a new SimpleAbsorbHandler
func NewSimpleAbsorbHandler(splog output.Output) *SimpleAbsorbHandler {
	return &SimpleAbsorbHandler{
		splog: splog,
	}
}

// Start is called at the beginning of absorb
func (h *SimpleAbsorbHandler) Start(_ bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.absorbed = 0
}

// OnStep is called for each step in the absorb process
func (h *SimpleAbsorbHandler) OnStep(_ absorb.Step, _ absorb.StepStatus, _ string) {
	// Steps are handled silently in simple handler
}

// OnHunkTarget is called when a target is found for a hunk
func (h *SimpleAbsorbHandler) OnHunkTarget(_ string, _ string, _ string) {
	// Targets are shown via printAbsorbPlan in the action
}

// OnUnabsorbedHunk is called for hunks that could not be absorbed
func (h *SimpleAbsorbHandler) OnUnabsorbedHunk(_ git.Hunk) {
	// Unabsorbed hunks are shown via output in the action
}

// OnApply is called when hunks are applied to a branch
func (h *SimpleAbsorbHandler) OnApply(_ string, _ string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.absorbed++
}

// Complete is called when absorb finishes
func (h *SimpleAbsorbHandler) Complete(_ absorb.Result) {
	// Summary is implicit from OnApply calls and action output
}

// Cleanup is a no-op for the simple handler
func (h *SimpleAbsorbHandler) Cleanup() {}

// IsInteractive returns false for simple handler
func (h *SimpleAbsorbHandler) IsInteractive() bool {
	return false
}

// PromptConfirm returns false for simple handler (non-interactive)
func (h *SimpleAbsorbHandler) PromptConfirm(_ string) (bool, error) {
	return false, nil
}
