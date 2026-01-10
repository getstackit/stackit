package stack

import (
	"fmt"
	"sync"

	"stackit.dev/stackit/internal/actions/move"
	"stackit.dev/stackit/internal/output"
	"stackit.dev/stackit/internal/tui"
	"stackit.dev/stackit/internal/tui/style"
)

// NewMoveUI creates a runner and handler pair for move operations.
// The runner manages terminal state; the handler processes events.
// Caller must defer runner.Cleanup() to restore terminal on exit.
// Currently returns nil runner as there's no TUI component yet.
func NewMoveUI(out output.Output, _ output.Logger, interactive bool) (*tui.Runner, move.Handler) {
	if interactive {
		return nil, NewInteractiveMoveHandler(out)
	}
	return nil, NewSimpleMoveHandler(out)
}

// SimpleMoveHandler provides streaming text output for move operations
type SimpleMoveHandler struct {
	splog        output.Output
	mu           sync.Mutex
	sourceBranch string
	oldParent    string
	newParent    string
}

// NewSimpleMoveHandler creates a new SimpleMoveHandler
func NewSimpleMoveHandler(splog output.Output) *SimpleMoveHandler {
	return &SimpleMoveHandler{
		splog: splog,
	}
}

// Start is called at the beginning of move
func (h *SimpleMoveHandler) Start(sourceBranch, oldParent, newParent string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sourceBranch = sourceBranch
	h.oldParent = oldParent
	h.newParent = newParent
}

// OnStep is called for each step in the move process
func (h *SimpleMoveHandler) OnStep(_ move.Step, _ move.StepStatus, _ string) {
	// Steps are handled silently in simple handler
}

// OnRename is called when a branch is renamed due to scope change
func (h *SimpleMoveHandler) OnRename(oldName, newName string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.splog.Info("Renamed branch %s to %s",
		style.ColorBranchName(oldName, false),
		style.ColorBranchName(newName, true))
}

// Complete is called when move finishes
func (h *SimpleMoveHandler) Complete(result move.Result) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.splog.Info("Moved %s from %s to %s",
		style.ColorBranchName(result.SourceBranch, true),
		style.ColorBranchName(result.OldParent, false),
		style.ColorBranchName(result.NewParent, false))
}

// Cleanup is a no-op for the simple handler
func (h *SimpleMoveHandler) Cleanup() {}

// IsInteractive returns false for simple handler
func (h *SimpleMoveHandler) IsInteractive() bool {
	return false
}

// PromptRename returns false for simple handler (non-interactive)
func (h *SimpleMoveHandler) PromptRename(_, oldScope, newScope string) (bool, error) {
	// In non-interactive mode, print a message but don't rename
	h.splog.Info("Branch name contains '%s', but its scope will now be '%s'. Use interactive mode to rename.",
		oldScope, newScope)
	return false, nil
}

// InteractiveMoveHandler provides interactive prompts for move operations
type InteractiveMoveHandler struct {
	SimpleMoveHandler
}

// NewInteractiveMoveHandler creates a new InteractiveMoveHandler
func NewInteractiveMoveHandler(splog output.Output) *InteractiveMoveHandler {
	return &InteractiveMoveHandler{
		SimpleMoveHandler: SimpleMoveHandler{splog: splog},
	}
}

// IsInteractive returns true for interactive handler
func (h *InteractiveMoveHandler) IsInteractive() bool {
	return true
}

// PromptRename prompts user to confirm branch rename due to scope change
func (h *InteractiveMoveHandler) PromptRename(_, oldScope, newScope string) (bool, error) {
	return tui.PromptConfirm(fmt.Sprintf("Branch name contains '%s', but its scope will now be '%s'. Would you like to rename the branch?", oldScope, newScope), true)
}
