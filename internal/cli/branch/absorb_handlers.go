package branch

import (
	"github.com/getstackit/stackit/internal/actions/absorb"
	"github.com/getstackit/stackit/internal/actions/handler"
	"github.com/getstackit/stackit/internal/cli/common"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/tui"
)

// NewAbsorbUI creates a runner and handler pair for absorb operations.
// The runner manages terminal state; the handler processes events.
// Caller must defer runner.Cleanup() to restore terminal on exit.
// Currently returns nil runner as there's no TUI component yet.
func NewAbsorbUI(out output.Output, _ output.Logger, interactive bool) (*tui.Runner, absorb.Handler) {
	if interactive {
		return nil, NewInteractiveAbsorbHandler(out)
	}
	return nil, NewSimpleAbsorbHandler(out)
}

// SimpleAbsorbHandler provides streaming text output for absorb operations
type SimpleAbsorbHandler struct {
	common.BaseHandler
	absorbed int
}

// NewSimpleAbsorbHandler creates a new SimpleAbsorbHandler
func NewSimpleAbsorbHandler(out output.Output) *SimpleAbsorbHandler {
	return &SimpleAbsorbHandler{
		BaseHandler: common.NewBaseHandler(out),
	}
}

// Start is called at the beginning of absorb
func (h *SimpleAbsorbHandler) Start(_ bool) {
	h.Lock()
	defer h.Unlock()
	h.absorbed = 0
}

// OnStep is called for each step in the absorb process
func (h *SimpleAbsorbHandler) OnStep(_ absorb.Step, _ handler.StepStatus, _ string) {
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
	h.Lock()
	defer h.Unlock()
	h.absorbed++
}

// Complete is called when absorb finishes
func (h *SimpleAbsorbHandler) Complete(_ absorb.Result) {
	// Summary is implicit from OnApply calls and action output
}

// PromptConfirm returns false for simple handler (non-interactive)
func (h *SimpleAbsorbHandler) PromptConfirm(_ string) (bool, error) {
	return false, nil
}

// InteractiveAbsorbHandler provides interactive prompts for absorb operations
type InteractiveAbsorbHandler struct {
	SimpleAbsorbHandler
}

// NewInteractiveAbsorbHandler creates a new InteractiveAbsorbHandler
func NewInteractiveAbsorbHandler(out output.Output) *InteractiveAbsorbHandler {
	return &InteractiveAbsorbHandler{
		SimpleAbsorbHandler: *NewSimpleAbsorbHandler(out),
	}
}

// IsInteractive returns true for interactive handler
func (h *InteractiveAbsorbHandler) IsInteractive() bool {
	return true
}

// PromptConfirm prompts the user for confirmation
func (h *InteractiveAbsorbHandler) PromptConfirm(prompt string) (bool, error) {
	return tui.PromptConfirm(prompt, true)
}
