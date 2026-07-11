package branch

import (
	"fmt"

	"github.com/getstackit/stackit/internal/actions/create"
	"github.com/getstackit/stackit/internal/actions/handler"
	"github.com/getstackit/stackit/internal/cli/common"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/tui"
	"github.com/getstackit/stackit/internal/tui/style"
	"github.com/getstackit/stackit/internal/utils"
)

// NewCreateUI creates a runner and handler pair for create operations.
// The runner manages terminal state; the handler processes events.
// Caller must defer runner.Cleanup() to restore terminal on exit.
// Currently returns nil runner as there's no TUI component yet.
func NewCreateUI(out output.Output, _ output.Logger) (*tui.Runner, create.Handler) {
	if utils.IsInteractive() {
		return nil, NewInteractiveCreateHandler(out)
	}
	return nil, NewSimpleCreateHandler(out)
}

// SimpleCreateHandler provides streaming text output for create operations
type SimpleCreateHandler struct {
	common.BaseHandler
	parentBranch string
}

// NewSimpleCreateHandler creates a new SimpleCreateHandler
func NewSimpleCreateHandler(out output.Output) *SimpleCreateHandler {
	return &SimpleCreateHandler{
		BaseHandler: common.NewBaseHandler(out),
	}
}

// Start is called at the beginning of create
func (h *SimpleCreateHandler) Start(parentBranch string) {
	h.Lock()
	defer h.Unlock()
	h.parentBranch = parentBranch
}

// OnStep is called for each step in the create process
func (h *SimpleCreateHandler) OnStep(step create.Step, status handler.StepStatus, message string) {
	h.Lock()
	defer h.Unlock()

	// Only show meaningful messages for completed/failed steps
	switch status {
	case handler.StatusCompleted:
		h.printStepCompleted(step, message)
	case handler.StatusFailed:
		h.Output.Error("Failed: %s", message)
	case handler.StatusSkipped:
		// Skip silently for most steps
		if step == create.StepCommit {
			h.Output.Info("No staged changes; created a branch with no commit.")
		}
	}
}

// Complete is called when create finishes
func (h *SimpleCreateHandler) Complete(result create.Result) {
	h.Lock()
	defer h.Unlock()

	h.Output.Info("Created branch %s on %s",
		style.ColorCurrentBranch(result.BranchName),
		style.ColorBranchName(result.ParentBranch))
}

// PromptStageChanges returns false for simple handler (non-interactive)
func (h *SimpleCreateHandler) PromptStageChanges() (bool, error) {
	return false, nil
}

// PromptScope returns empty string for simple handler (non-interactive)
func (h *SimpleCreateHandler) PromptScope(_ string) (string, error) {
	return "", nil
}

func (h *SimpleCreateHandler) printStepCompleted(_ create.Step, _ string) {
	// Most steps are silent - output is handled in Complete
	// Only show certain steps for verbose feedback
	// Worktree creation is shown by the action itself
}

// InteractiveCreateHandler provides interactive prompts for create operations
type InteractiveCreateHandler struct {
	*SimpleCreateHandler
}

// NewInteractiveCreateHandler creates a new InteractiveCreateHandler
func NewInteractiveCreateHandler(out output.Output) *InteractiveCreateHandler {
	return &InteractiveCreateHandler{
		SimpleCreateHandler: NewSimpleCreateHandler(out),
	}
}

// IsInteractive returns true for interactive handler
func (h *InteractiveCreateHandler) IsInteractive() bool { return true }

// PromptStageChanges prompts user to stage unstaged changes
func (h *InteractiveCreateHandler) PromptStageChanges() (bool, error) {
	return tui.PromptConfirm("Stage all unstaged changes?", true)
}

// PromptScope prompts user for a scope value when pattern contains {scope}
func (h *InteractiveCreateHandler) PromptScope(patternHint string) (string, error) {
	prompt := fmt.Sprintf("Branch pattern uses {scope}: %s\nEnter scope (or Enter to skip):", patternHint)
	return tui.PromptTextInput(prompt, "")
}
