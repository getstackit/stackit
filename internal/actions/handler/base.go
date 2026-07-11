// Package handler provides common handler types and base implementations
// for action handlers throughout the stackit codebase.
//
// All action handlers share common patterns:
//   - StepStatus enum for step lifecycle states
//   - Cleanup() method for terminal state restoration
//   - IsInteractive() method for interactive prompt support
//   - NullBase struct embedding for default no-op implementations
package handler

// StepStatus represents the lifecycle status of a step in an action.
// This is the standard status type used across all action handlers.
type StepStatus string

// Step status constants - use these across all handlers for consistency.
const (
	// StatusStarted indicates a step is beginning execution.
	StatusStarted StepStatus = "started"

	// StatusCompleted indicates a step finished successfully.
	StatusCompleted StepStatus = "completed"

	// StatusSkipped indicates a step was skipped (e.g., not applicable).
	StatusSkipped StepStatus = "skipped"

	// StatusFailed indicates a step encountered an error.
	StatusFailed StepStatus = "failed"
)

// Base defines the common interface that all action handlers must implement.
// Embed NullBase in your handler struct to get default no-op implementations.
type Base interface {
	// Cleanup restores terminal state after the action completes.
	// Called in defer blocks to ensure cleanup on success or failure.
	// May be a no-op for non-interactive handlers.
	Cleanup()

	// IsInteractive returns true if the handler supports interactive prompts.
	// Used to determine whether to show confirmation dialogs, progress UI, etc.
	IsInteractive() bool
}

// Reparent describes a branch moving from one parent to another.
type Reparent struct {
	Branch    string
	OldParent string
	NewParent string
}

// NullBase provides no-op implementations of the Base interface.
// Embed this in handler structs to inherit default implementations.
//
// Example:
//
//	type MyNullHandler struct {
//	    handler.NullBase
//	}
//
//	// MyNullHandler now has Cleanup() and IsInteractive() implemented
type NullBase struct{}

// Cleanup implements Base. No-op for null handler.
func (NullBase) Cleanup() {}

// IsInteractive implements Base. Returns false for null handler.
func (NullBase) IsInteractive() bool { return false }

// PromptHandler defines the interface for handlers that support confirmation prompts.
// Not all handlers need prompts, so this is a separate interface.
type PromptHandler interface {
	// PromptConfirm displays a confirmation prompt and returns the user's choice.
	// Returns (true, nil) to proceed, (false, nil) to cancel.
	// Returns an error if the prompt fails (e.g., terminal error).
	PromptConfirm(message string) (bool, error)
}

// NullPromptHandler provides a no-op implementation of PromptHandler.
// Returns (false, nil) for all prompts (auto-decline).
type NullPromptHandler struct{}

// PromptConfirm implements PromptHandler. Returns false (auto-decline) for null handler.
func (NullPromptHandler) PromptConfirm(string) (bool, error) { return false, nil }

// AutoConfirmPromptHandler provides a PromptHandler that always confirms.
// Use this when you want prompts to auto-confirm (e.g., --yes flag).
type AutoConfirmPromptHandler struct{}

// PromptConfirm implements PromptHandler. Returns true (auto-confirm).
func (AutoConfirmPromptHandler) PromptConfirm(string) (bool, error) { return true, nil }

// ProgressHandler defines the interface for handlers that report step progress.
// The step parameter type varies by action, so this uses a generic Step type.
type ProgressHandler[Step any] interface {
	// OnStep is called to report progress on a named step.
	OnStep(step Step, status StepStatus, message string)
}

// NullProgress provides a no-op implementation of ProgressHandler for any step type.
type NullProgress[Step any] struct{}

// OnStep implements ProgressHandler. No-op for null handler.
func (NullProgress[Step]) OnStep(Step, StepStatus, string) {}
