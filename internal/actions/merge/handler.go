package merge

import (
	"time"

	"github.com/getstackit/stackit/internal/github"
)

// Phase represents a phase in the merge process
type Phase string

// Phase constants
const (
	PhasePlan     Phase = "plan"
	PhaseMerge    Phase = "merge"
	PhaseRestack  Phase = "restack"
	PhaseCleanup  Phase = "cleanup"
	PhaseWaiting  Phase = "waiting"
	PhaseComplete Phase = "complete"
)

// EventType represents the type of merge event
type EventType string

// Event type constants
const (
	EventStarted   EventType = "started"
	EventProgress  EventType = "progress"
	EventCompleted EventType = "completed"
	EventFailed    EventType = "failed"
	EventWaiting   EventType = "waiting"
	EventSkipped   EventType = "skipped"
)

// Event represents a merge progress event
type Event struct {
	Phase     Phase
	Type      EventType
	StepIndex int
	Step      *PlanStep
	Message   string
	Error     error

	// Waiting-specific fields
	Elapsed time.Duration
	Timeout time.Duration
	Checks  []github.CheckDetail

	// Estimate fields
	EstimatedDuration time.Duration
}

// Result contains the final result of a merge operation
type Result struct {
	Success             bool
	ConsolidationResult *ConsolidationResult
	Error               error
}

// EventHandler is the interface for reporting merge progress using events.
// This is the recommended interface for new handler implementations.
//
// Lifecycle:
//  1. Start() is called once when the merge plan is ready
//  2. EmitEvent() is called for each step (started, completed, failed, waiting)
//  3. Complete() is called once when the operation finishes
//  4. Cleanup() should be called via defer to restore terminal state
//
// Thread safety: All methods may be called from different goroutines.
// Implementations should handle synchronization internally.
type EventHandler interface {
	// Start is called when the merge plan is ready to execute.
	// The plan contains all steps that will be executed.
	Start(plan *Plan)

	// EmitEvent sends a progress event to the handler.
	// Called for each step transition (started, completed, failed, waiting).
	// May be called concurrently from multiple goroutines.
	EmitEvent(event Event)

	// Complete is called when the merge operation is finished.
	// Result contains success status and any consolidation result.
	Complete(result *Result)

	// Cleanup ensures terminal state is restored.
	// Should be called via defer after creating the handler.
	// No-op for non-TTY handlers.
	Cleanup()
}
