package cli

import (
	"sync"

	"stackit.dev/stackit/internal/actions/doctor"
	"stackit.dev/stackit/internal/output"
	"stackit.dev/stackit/internal/tui"
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
	splog           output.Output
	mu              sync.Mutex
	currentCategory doctor.Category
	passedCount     int
	warningCount    int
	errorCount      int
}

// NewSimpleDoctorHandler creates a new SimpleDoctorHandler
func NewSimpleDoctorHandler(splog output.Output) *SimpleDoctorHandler {
	return &SimpleDoctorHandler{
		splog: splog,
	}
}

// Start is called at the beginning of doctor
func (h *SimpleDoctorHandler) Start(fix bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if fix {
		h.splog.Info("Running stackit doctor with --fix...")
	} else {
		h.splog.Info("Running stackit doctor...")
	}
	h.splog.Newline()
}

// OnCategory is called when starting a new category of checks
func (h *SimpleDoctorHandler) OnCategory(category doctor.Category) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Add spacing between categories (but not before first)
	if h.currentCategory != "" {
		h.splog.Newline()
	}
	h.currentCategory = category

	switch category {
	case doctor.CategoryEnvironment:
		h.splog.Info("Environment:")
	case doctor.CategoryRepository:
		h.splog.Info("Repository:")
	case doctor.CategoryStackState:
		h.splog.Info("Stack State:")
	}
}

// OnCheck is called for each individual check result
func (h *SimpleDoctorHandler) OnCheck(_ string, status doctor.CheckStatus, message string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	switch status {
	case doctor.CheckPassed:
		h.passedCount++
		h.splog.Info("  ✅ %s", message)
	case doctor.CheckWarning:
		h.warningCount++
		h.splog.Warn("  %s", message)
	case doctor.CheckError:
		h.errorCount++
		h.splog.Error("  %s", message)
	}
}

// Complete is called when doctor finishes
func (h *SimpleDoctorHandler) Complete(_, _, _ int) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.splog.Newline()

	switch {
	case h.errorCount > 0:
		h.splog.Warn("Doctor found %d error(s) and %d warning(s).", h.errorCount, h.warningCount)
	case h.warningCount > 0:
		h.splog.Info("Doctor found %d warning(s). Your stackit setup is mostly healthy.", h.warningCount)
	default:
		h.splog.Info("✅ All checks passed. Your stackit setup is healthy.")
	}
}

// Cleanup is a no-op for the simple handler
func (h *SimpleDoctorHandler) Cleanup() {}
