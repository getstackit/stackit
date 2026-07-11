package stack

import (
	"fmt"

	"github.com/getstackit/stackit/internal/actions/flatten"
	"github.com/getstackit/stackit/internal/actions/handler"
	"github.com/getstackit/stackit/internal/cli/common"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/tui"
	flattenComponent "github.com/getstackit/stackit/internal/tui/components/flatten"
)

// NewFlattenUI creates a runner and handler pair for flatten operations.
// The runner manages terminal state; the handler processes events.
// Caller must defer runner.Cleanup() to restore terminal on exit.
func NewFlattenUI(out output.Output, logger output.Logger) (*tui.Runner, flatten.Handler) {
	if tui.IsTTY() {
		model := flattenComponent.NewModel()
		runner := tui.NewRunner(model, out, logger)
		runner.Start()
		return runner, NewInteractiveFlattenHandler(out, runner, model)
	}
	return nil, NewSimpleFlattenHandler(out)
}

// SimpleFlattenHandler provides streaming text output for flatten operations
type SimpleFlattenHandler struct {
	common.BaseHandler
	branchCount int
}

// NewSimpleFlattenHandler creates a new SimpleFlattenHandler
func NewSimpleFlattenHandler(out output.Output) *SimpleFlattenHandler {
	return &SimpleFlattenHandler{
		BaseHandler: common.NewBaseHandler(out),
	}
}

// Start is called at the beginning of flatten
func (h *SimpleFlattenHandler) Start(branchCount int) {
	h.Lock()
	defer h.Unlock()
	h.branchCount = branchCount
}

// OnStep is called for each step in the flatten process
func (h *SimpleFlattenHandler) OnStep(_ flatten.Step, _ handler.StepStatus, _ string) {
	// Steps are handled silently in simple handler
}

// OnValidationProgress is called during branch validation to report progress
func (h *SimpleFlattenHandler) OnValidationProgress(_, _ int, _ string) {
	// Progress is silent in simple handler
}

// OnBranchMoved is called when a branch is moved to a new parent
func (h *SimpleFlattenHandler) OnBranchMoved(handler.Reparent) {
	// Moves are reported by the action itself
}

// Complete is called when flatten finishes
func (h *SimpleFlattenHandler) Complete(_ flatten.Result) {
	// Output already handled by the action
}

// PromptConfirmFlatten returns true (auto-confirm) for simple handler (non-interactive)
func (h *SimpleFlattenHandler) PromptConfirmFlatten(_ flatten.Preview) (bool, error) {
	return true, nil
}

// InteractiveFlattenHandler provides interactive prompts for flatten operations
type InteractiveFlattenHandler struct {
	SimpleFlattenHandler
	runner *tui.Runner
	model  *flattenComponent.Model
}

// NewInteractiveFlattenHandler creates a new InteractiveFlattenHandler
func NewInteractiveFlattenHandler(out output.Output, runner *tui.Runner, model *flattenComponent.Model) *InteractiveFlattenHandler {
	return &InteractiveFlattenHandler{
		SimpleFlattenHandler: *NewSimpleFlattenHandler(out),
		runner:               runner,
		model:                model,
	}
}

// IsInteractive returns true for interactive handler
func (h *InteractiveFlattenHandler) IsInteractive() bool {
	return true
}

// OnValidationProgress sends validation progress to the TUI
func (h *InteractiveFlattenHandler) OnValidationProgress(current, total int, branchName string) {
	if h.runner != nil {
		h.runner.Send(flattenComponent.ValidationProgressMsg{
			Current: current,
			Total:   total,
			Branch:  branchName,
		})
	}
}

// OnStep sends step updates to the TUI
func (h *InteractiveFlattenHandler) OnStep(step flatten.Step, status handler.StepStatus, _ string) {
	if h.runner == nil {
		return
	}

	// Map flatten steps to TUI phases
	var phase flattenComponent.Phase
	switch step {
	case flatten.StepAnalyzing:
		phase = flattenComponent.PhaseAnalyzing
	case flatten.StepValidating:
		phase = flattenComponent.PhaseValidating
	case flatten.StepFlattening:
		phase = flattenComponent.PhaseFlattening
	case flatten.StepRestacking:
		phase = flattenComponent.PhaseRestacking
	default:
		return
	}

	if status == handler.StatusStarted {
		h.runner.Send(flattenComponent.PhaseStartMsg{Phase: phase})
	}
}

// Complete sends completion message to the TUI
func (h *InteractiveFlattenHandler) Complete(result flatten.Result) {
	if h.runner == nil {
		return
	}

	var summary string
	if result.MovedCount == 0 {
		summary = "All branches are already as close to trunk as possible."
	} else {
		summary = fmt.Sprintf("Flatten complete: %d branches moved, %d unchanged.",
			result.MovedCount, result.UnchangedCount)
	}

	h.runner.Send(flattenComponent.CompleteMsg{Summary: summary})
}

// Cleanup restores the terminal
func (h *InteractiveFlattenHandler) Cleanup() {
	if h.runner != nil {
		h.runner.Cleanup()
	}
}

// PromptConfirmFlatten displays a preview of the flatten and asks for confirmation
func (h *InteractiveFlattenHandler) PromptConfirmFlatten(preview flatten.Preview) (bool, error) {
	// Pause TUI to allow terminal prompt
	if h.runner != nil {
		h.runner.Pause()
	}

	h.Output.Newline()

	// Convert to TUI preview data
	moves := make([]tui.FlattenPlannedMove, len(preview.Moves))
	for i, m := range preview.Moves {
		moves[i] = tui.FlattenPlannedMove{
			Branch:    m.Branch,
			OldParent: m.OldParent,
			NewParent: m.NewParent,
		}
	}

	excluded := make([]tui.FlattenExcludedBranch, len(preview.ExcludedBranches))
	for i, e := range preview.ExcludedBranches {
		excluded[i] = tui.FlattenExcludedBranch{
			Branch: e.Branch,
			Reason: e.Reason,
		}
	}

	previewData := tui.FlattenPreviewData{
		Moves:            moves,
		UnchangedCount:   preview.UnchangedCount,
		ExcludedBranches: excluded,
	}
	h.Output.Print(tui.RenderFlattenPreview(previewData))

	h.Output.Newline()

	// If all moves were excluded due to dependencies, inform user
	if len(preview.Moves) == 0 && len(preview.ExcludedBranches) > 0 {
		h.Output.Info("No branches can be flattened (all have dependent branches).")
		return false, nil
	}

	confirmed, err := tui.PromptConfirm("Proceed with flatten?", true)
	if err != nil {
		return false, err
	}

	// Only resume TUI if proceeding with flatten
	if confirmed && h.runner != nil {
		h.runner.Resume()
	}

	return confirmed, nil
}
