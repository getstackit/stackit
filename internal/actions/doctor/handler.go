package doctor

// CheckStatus represents the result of a diagnostic check
type CheckStatus string

// Check status constants
const (
	CheckPassed  CheckStatus = "passed"
	CheckWarning CheckStatus = "warning"
	CheckError   CheckStatus = "error"
)

// Category represents a group of related checks
type Category string

// Check category constants
const (
	CategoryEnvironment Category = "environment"
	CategoryRepository  Category = "repository"
	CategoryStackState  Category = "stack_state"
)

// Handler receives events from doctor action
type Handler interface {
	// Start is called at the beginning of doctor
	Start(fix bool)

	// OnCategory is called when starting a new category of checks
	OnCategory(category Category)

	// OnCheck is called for each individual check result
	OnCheck(name string, status CheckStatus, message string)

	// Complete is called when doctor finishes
	Complete(passed, warnings, errors int)

	// Cleanup restores terminal state (may be no-op)
	Cleanup()
}

// NullHandler is a no-op handler for when nil is passed
type NullHandler struct{}

// Start implements Handler.
func (h *NullHandler) Start(_ bool) {}

// OnCategory implements Handler.
func (h *NullHandler) OnCategory(_ Category) {}

// OnCheck implements Handler.
func (h *NullHandler) OnCheck(_ string, _ CheckStatus, _ string) {}

// Complete implements Handler.
func (h *NullHandler) Complete(_, _, _ int) {}

// Cleanup implements Handler.
func (h *NullHandler) Cleanup() {}
