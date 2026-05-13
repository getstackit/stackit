package stack

import (
	"fmt"

	"github.com/getstackit/stackit/internal/actions/handler"
	"github.com/getstackit/stackit/internal/actions/move"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/cli/common"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/tui"
	"github.com/getstackit/stackit/internal/tui/style"
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
	common.BaseHandler
	sourceBranch string
	oldParent    string
	newParent    string
}

// NewSimpleMoveHandler creates a new SimpleMoveHandler
func NewSimpleMoveHandler(out output.Output) *SimpleMoveHandler {
	return &SimpleMoveHandler{
		BaseHandler: common.NewBaseHandler(out),
	}
}

// Start is called at the beginning of move
func (h *SimpleMoveHandler) Start(sourceBranch, oldParent, newParent string) {
	h.Lock()
	defer h.Unlock()
	h.sourceBranch = sourceBranch
	h.oldParent = oldParent
	h.newParent = newParent
}

// OnStep is called for each step in the move process
func (h *SimpleMoveHandler) OnStep(_ move.Step, _ handler.StepStatus, _ string) {
	// Steps are handled silently in simple handler
}

// OnRename is called when a branch is renamed due to scope change
func (h *SimpleMoveHandler) OnRename(oldName, newName string) {
	h.Lock()
	defer h.Unlock()
	h.Output.Info("Renamed branch %s to %s",
		style.ColorBranchName(oldName, false),
		style.ColorBranchName(newName, true))
}

// Complete is called when move finishes
func (h *SimpleMoveHandler) Complete(_ move.Result) {
	// Output already handled by the action
}

// PromptRename returns false for simple handler (non-interactive)
func (h *SimpleMoveHandler) PromptRename(_, oldScope, newScope string) (bool, error) {
	// In non-interactive mode, print a message but don't rename
	h.Output.Info("Branch name contains '%s', but its scope will now be '%s'. Use interactive mode to rename.",
		oldScope, newScope)
	return false, nil
}

// PromptConfirmMove returns true (auto-confirm) for simple handler (non-interactive)
func (h *SimpleMoveHandler) PromptConfirmMove(_ move.Preview) (bool, error) {
	return true, nil
}

// PromptSelectOnto returns error for simple handler (non-interactive).
func (h *SimpleMoveHandler) PromptSelectOnto(_ *app.Context, _ string) (string, []engine.RebaseSpec, error) {
	return "", nil, fmt.Errorf("target branch must be specified for move")
}

// InteractiveMoveHandler provides interactive prompts for move operations
type InteractiveMoveHandler struct {
	SimpleMoveHandler
}

// NewInteractiveMoveHandler creates a new InteractiveMoveHandler
func NewInteractiveMoveHandler(out output.Output) *InteractiveMoveHandler {
	return &InteractiveMoveHandler{
		SimpleMoveHandler: *NewSimpleMoveHandler(out),
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

// PromptConfirmMove displays a preview of the move and asks for confirmation
func (h *InteractiveMoveHandler) PromptConfirmMove(preview move.Preview) (bool, error) {
	h.Output.Newline()

	// Render visual tree preview
	previewData := tui.MovePreviewData{
		SourceBranch:   preview.SourceBranch,
		OldParent:      preview.OldParent,
		NewParent:      preview.NewParent,
		Commits:        preview.Commits,
		Descendants:    preview.Descendants,
		HasConflicts:   preview.HasConflicts,
		ConflictBranch: preview.ConflictBranch,
		ConflictError:  preview.ConflictError,
	}
	h.Output.Print(tui.RenderMovePreviewSimple(previewData))

	h.Output.Newline()

	// If there are conflicts, offer to enter the conflict-resolution workflow.
	if preview.HasConflicts {
		h.Output.Info("The move will hit conflicts that need to be resolved manually.")
		h.Output.Info("Proceeding will pause the rebase so you can fix the files, then run %s.",
			style.ColorCyan("stackit continue"))
		return tui.PromptConfirm("Proceed and resolve conflicts manually?", false)
	}

	return tui.PromptConfirm("Proceed with move?", true)
}

// PromptSelectOnto prompts user to select a new parent when --onto is not provided.
func (h *InteractiveMoveHandler) PromptSelectOnto(ctx *app.Context, sourceBranch string) (string, []engine.RebaseSpec, error) {
	return move.SelectOntoInteractive(ctx, sourceBranch)
}
