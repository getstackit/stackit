package operations

import (
	"context"
	"fmt"
	"sync"

	submitAction "github.com/getstackit/stackit/internal/actions/submit"
	"github.com/getstackit/stackit/internal/app"
)

// SubmitOperation wraps the submit action as an async Operation.
type SubmitOperation struct {
	BaseOperation
	ctx     *app.Context
	options submitAction.Options

	// progress channel for emitting updates
	progress chan Progress
	mu       sync.Mutex

	// tracking state
	branches     []string
	currentIndex int
	totalCount   int
}

// NewSubmitOperation creates a new submit operation.
func NewSubmitOperation(ctx *app.Context, options submitAction.Options) *SubmitOperation {
	return &SubmitOperation{
		BaseOperation: BaseOperation{
			id: nextID("submit"),
		},
		ctx:     ctx,
		options: options,
	}
}

// Start begins the submit operation and returns a channel for progress updates.
func (o *SubmitOperation) Start(ctx context.Context) <-chan Progress {
	o.progress = make(chan Progress, 20)

	// Create a cancellable context
	ctx, cancel := context.WithCancel(ctx)
	o.SetCancel(cancel)

	go func() {
		defer close(o.progress)

		// Replace the context in runtime.Context with our cancellable one
		originalCtx := o.ctx.Context
		o.ctx.Context = ctx
		defer func() { o.ctx.Context = originalCtx }()

		// Run the submit action with ourselves as the handler
		err := submitAction.Action(o.ctx, o.options, o)

		if err != nil {
			// Check if canceled
			if ctx.Err() == context.Canceled {
				o.emit(Progress{
					OperationID: o.id,
					Status:      StatusCanceled,
					Step:        "Operation canceled",
				})
				return
			}

			o.emit(Progress{
				OperationID: o.id,
				Status:      StatusFailed,
				Step:        "Submit failed",
				Error:       err,
			})
			return
		}

		// Note: CompletionEvent will emit the final progress
	}()

	return o.progress
}

// emit sends a progress update if the channel is open
func (o *SubmitOperation) emit(p Progress) {
	select {
	case o.progress <- p:
	default:
		// Channel full or closed, skip
	}
}

// OnEvent handles events from the submit action
func (o *SubmitOperation) OnEvent(e submitAction.Event) {
	switch ev := e.(type) {
	case submitAction.StackDisplayEvent:
		o.emit(Progress{
			OperationID: o.id,
			Status:      StatusRunning,
			Step:        "Analyzing stack",
		})

	case submitAction.RestackEvent:
		if ev.Started {
			o.emit(Progress{
				OperationID: o.id,
				Status:      StatusRunning,
				Step:        "Restacking branches",
			})
		} else if ev.Completed {
			o.emit(Progress{
				OperationID: o.id,
				Status:      StatusRunning,
				Step:        "Restack complete",
			})
		}

	case submitAction.PreparingEvent:
		o.emit(Progress{
			OperationID: o.id,
			Status:      StatusRunning,
			Step:        "Preparing branches",
		})

	case submitAction.BranchPlanEvent:
		o.mu.Lock()
		if !ev.Skipped {
			o.branches = append(o.branches, ev.BranchName)
			o.totalCount = len(o.branches)
		}
		o.mu.Unlock()

		status := StatusRunning
		step := fmt.Sprintf("Will %s %s", ev.Action, ev.BranchName)
		if ev.Skipped {
			status = StatusSkipped
			step = fmt.Sprintf("Skipping %s: %s", ev.BranchName, ev.SkipReason)
		}

		o.emit(Progress{
			OperationID: o.id,
			Status:      status,
			Step:        step,
			Branch:      ev.BranchName,
			Total:       o.totalCount,
		})

	case submitAction.SubmissionStartEvent:
		o.mu.Lock()
		o.totalCount = len(ev.Branches)
		o.currentIndex = 0
		o.mu.Unlock()

		o.emit(Progress{
			OperationID: o.id,
			Status:      StatusRunning,
			Step:        fmt.Sprintf("Submitting %d branches", len(ev.Branches)),
			Total:       len(ev.Branches),
		})

	case submitAction.BranchProgressEvent:
		o.mu.Lock()
		current := o.currentIndex
		total := o.totalCount
		o.mu.Unlock()

		var opStatus Status
		var step string
		var result any

		switch ev.Status {
		case submitAction.StatusSubmitting:
			opStatus = StatusRunning
			step = fmt.Sprintf("Submitting %s", ev.BranchName)
		case submitAction.StatusDone:
			opStatus = StatusCompleted
			step = fmt.Sprintf("Submitted %s", ev.BranchName)
			result = ev.URL
			o.mu.Lock()
			o.currentIndex++
			current = o.currentIndex
			o.mu.Unlock()
		case submitAction.StatusError:
			opStatus = StatusFailed
			step = fmt.Sprintf("Failed to submit %s", ev.BranchName)
		default:
			opStatus = StatusRunning
			step = fmt.Sprintf("%s %s", ev.Status, ev.BranchName)
		}

		o.emit(Progress{
			OperationID: o.id,
			Status:      opStatus,
			Step:        step,
			Branch:      ev.BranchName,
			Current:     current,
			Total:       total,
			Result:      result,
			Error:       ev.Error,
		})

	case submitAction.CompletionEvent:
		status := StatusCompleted
		if !ev.Success {
			status = StatusFailed
		}
		o.emit(Progress{
			OperationID: o.id,
			Status:      status,
			Step:        ev.Message,
			Current:     o.totalCount,
			Total:       o.totalCount,
		})
	}
}

// Confirm auto-confirms with the default value.
// Operations don't support interactive confirmation.
func (o *SubmitOperation) Confirm(_ string, defaultYes bool) (bool, error) {
	return defaultYes, nil
}

// IsInteractive returns false - operations are not interactive.
func (o *SubmitOperation) IsInteractive() bool {
	return false
}
