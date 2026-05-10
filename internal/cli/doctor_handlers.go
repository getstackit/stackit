package cli

import (
	"github.com/getstackit/stackit/internal/actions/doctor"
	"github.com/getstackit/stackit/internal/cli/common"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/tui"
)

// NewDoctorUI creates a runner and handler pair for doctor operations.
// The runner manages terminal state; the handler processes events.
// Caller must defer runner.Cleanup() to restore terminal on exit.
// Currently returns nil runner as there's no TUI component yet.
func NewDoctorUI(out output.Output, _ output.Logger) (*tui.Runner, doctor.Handler) {
	// TODO: Add interactive TUI handler when needed
	// For now, use simple handler for both TTY and non-TTY
	return nil, NewSimpleDoctorHandler(out)
}

// SimpleDoctorHandler provides streaming text output for doctor operations
type SimpleDoctorHandler struct {
	common.BaseHandler
	currentCategory doctor.Category
	passedCount     int
	warningCount    int
	errorCount      int
}

// NewSimpleDoctorHandler creates a new SimpleDoctorHandler
func NewSimpleDoctorHandler(out output.Output) *SimpleDoctorHandler {
	return &SimpleDoctorHandler{
		BaseHandler: common.NewBaseHandler(out),
	}
}

// Start is called at the beginning of doctor
func (h *SimpleDoctorHandler) Start(fix bool) {
	h.Lock()
	defer h.Unlock()

	if fix {
		h.Output.Info("Running stackit doctor with --fix...")
	} else {
		h.Output.Info("Running stackit doctor...")
	}
	h.Output.Newline()
}

// OnCategory is called when starting a new category of checks
func (h *SimpleDoctorHandler) OnCategory(category doctor.Category) {
	h.Lock()
	defer h.Unlock()

	// Add spacing between categories (but not before first)
	if h.currentCategory != "" {
		h.Output.Newline()
	}
	h.currentCategory = category

	switch category {
	case doctor.CategoryEnvironment:
		h.Output.Info("Environment:")
	case doctor.CategoryRepository:
		h.Output.Info("Repository:")
	case doctor.CategoryStackState:
		h.Output.Info("Stack State:")
	}
}

// OnCheck is called for each individual check result
func (h *SimpleDoctorHandler) OnCheck(_ string, status doctor.CheckStatus, message string) {
	h.Lock()
	defer h.Unlock()

	switch status {
	case doctor.CheckPassed:
		h.passedCount++
		h.Output.Info("  ✅ %s", message)
	case doctor.CheckWarning:
		h.warningCount++
		h.Output.Warn("  %s", message)
	case doctor.CheckError:
		h.errorCount++
		h.Output.Error("  %s", message)
	}
}

// Complete is called when doctor finishes
func (h *SimpleDoctorHandler) Complete(_, _, _ int) {
	h.Lock()
	defer h.Unlock()

	h.Output.Newline()

	switch {
	case h.errorCount > 0:
		h.Output.Warn("Doctor found %d error(s) and %d warning(s).", h.errorCount, h.warningCount)
	case h.warningCount > 0:
		h.Output.Info("Doctor found %d warning(s). Your stackit setup is mostly healthy.", h.warningCount)
	default:
		h.Output.Info("✅ All checks passed. Your stackit setup is healthy.")
	}
}
