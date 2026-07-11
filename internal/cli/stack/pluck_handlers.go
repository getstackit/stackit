package stack

import (
	"github.com/getstackit/stackit/internal/actions/handler"
	"github.com/getstackit/stackit/internal/actions/pluck"
	"github.com/getstackit/stackit/internal/cli/common"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/tui"
	"github.com/getstackit/stackit/internal/tui/style"
)

// NewPluckUI creates a runner and handler pair for pluck operations.
// The runner manages terminal state; the handler processes events.
// Caller must defer runner.Cleanup() to restore terminal on exit.
// Currently returns nil runner as there's no TUI component yet.
func NewPluckUI(out output.Output, _ output.Logger, interactive bool) (*tui.Runner, pluck.Handler) {
	if interactive {
		return nil, NewInteractivePluckHandler(out)
	}
	return nil, NewSimplePluckHandler(out)
}

// SimplePluckHandler provides streaming text output for pluck operations
type SimplePluckHandler struct {
	common.BaseHandler
	sourceBranch string
	oldParent    string
	newParent    string
}

// NewSimplePluckHandler creates a new SimplePluckHandler
func NewSimplePluckHandler(out output.Output) *SimplePluckHandler {
	return &SimplePluckHandler{
		BaseHandler: common.NewBaseHandler(out),
	}
}

// Start is called at the beginning of pluck
func (h *SimplePluckHandler) Start(move handler.Reparent) {
	h.Lock()
	defer h.Unlock()
	h.sourceBranch = move.Branch
	h.oldParent = move.OldParent
	h.newParent = move.NewParent
}

// OnStep is called for each step in the pluck process
func (h *SimplePluckHandler) OnStep(_ pluck.Step, _ handler.StepStatus, _ string) {
	// Steps are handled silently in simple handler
}

// OnChildReparented is called when a child is reparented
func (h *SimplePluckHandler) OnChildReparented(handler.Reparent) {
	// Output is handled by the action
}

// Complete is called when pluck finishes
func (h *SimplePluckHandler) Complete(_ pluck.Result) {
	// Output already handled by the action
}

// PromptConfirmPluck returns true (auto-confirm) for simple handler (non-interactive)
func (h *SimplePluckHandler) PromptConfirmPluck(_ pluck.Preview) (bool, error) {
	return true, nil
}

// InteractivePluckHandler provides interactive prompts for pluck operations
type InteractivePluckHandler struct {
	SimplePluckHandler
}

// NewInteractivePluckHandler creates a new InteractivePluckHandler
func NewInteractivePluckHandler(out output.Output) *InteractivePluckHandler {
	return &InteractivePluckHandler{
		SimplePluckHandler: *NewSimplePluckHandler(out),
	}
}

// IsInteractive returns true for interactive handler
func (h *InteractivePluckHandler) IsInteractive() bool {
	return true
}

// PromptConfirmPluck displays a preview of the pluck and asks for confirmation
func (h *InteractivePluckHandler) PromptConfirmPluck(preview pluck.Preview) (bool, error) {
	h.Output.Newline()
	h.Output.Info("Pluck Preview:")
	h.Output.Info("  Branch: %s", style.ColorCurrentBranch(preview.SourceBranch))
	h.Output.Info("  From:   %s", style.ColorBranchName(preview.OldParent))
	h.Output.Info("  To:     %s", style.ColorBranchName(preview.NewParent))

	if len(preview.Commits) > 0 {
		h.Output.Newline()
		h.Output.Info("Commits to be moved (%d):", len(preview.Commits))
		for _, commit := range preview.Commits {
			h.Output.Info("  • %s", commit)
		}
	}

	if len(preview.Children) > 0 {
		h.Output.Newline()
		h.Output.Info("Children to be reparented to %s (%d):",
			style.ColorBranchName(preview.ChildNewParent),
			len(preview.Children))
		for _, child := range preview.Children {
			h.Output.Info("  • %s", style.ColorBranchName(child))
		}
	}

	h.Output.Newline()
	h.Output.Info("This will:")
	if len(preview.Children) > 0 {
		h.Output.Info("  1. Reparent %d children to %s",
			len(preview.Children),
			style.ColorBranchName(preview.ChildNewParent))
		h.Output.Info("  2. Move %s to %s",
			style.ColorCurrentBranch(preview.SourceBranch),
			style.ColorBranchName(preview.NewParent))
	} else {
		h.Output.Info("  1. Move %s to %s",
			style.ColorCurrentBranch(preview.SourceBranch),
			style.ColorBranchName(preview.NewParent))
	}
	h.Output.Newline()

	return tui.PromptConfirm("Proceed with pluck?", true)
}
