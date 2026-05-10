package operations

import (
	"context"

	syncAction "github.com/getstackit/stackit/internal/actions/sync"
	"github.com/getstackit/stackit/internal/app"
)

// SyncOperation wraps the sync action as an async Operation.
type SyncOperation struct {
	BaseOperation
	ctx     *app.Context
	options syncAction.Options
}

// NewSyncOperation creates a new sync operation.
func NewSyncOperation(ctx *app.Context, options syncAction.Options) *SyncOperation {
	return &SyncOperation{
		BaseOperation: BaseOperation{
			id: nextID("sync"),
		},
		ctx:     ctx,
		options: options,
	}
}

// Start begins the sync operation and returns a channel for progress updates.
func (o *SyncOperation) Start(ctx context.Context) <-chan Progress {
	progress := make(chan Progress, 10)

	// Create a cancellable context
	ctx, cancel := context.WithCancel(ctx)
	o.SetCancel(cancel)

	go func() {
		defer close(progress)

		// Emit start
		progress <- Progress{
			OperationID: o.id,
			Status:      StatusRunning,
			Step:        "Starting sync",
			Current:     0,
			Total:       3, // Pull trunk, sync GitHub, clean/restack
		}

		// Replace the context in runtime.Context with our cancellable one
		originalCtx := o.ctx.Context
		o.ctx.Context = ctx
		defer func() { o.ctx.Context = originalCtx }()

		// Emit pulling trunk
		progress <- Progress{
			OperationID: o.id,
			Status:      StatusRunning,
			Step:        "Pulling trunk from remote",
			Current:     1,
			Total:       3,
		}

		// Run the sync action
		// Note: Pass nil handler to use the NullSyncHandler
		// The operations framework has its own progress mechanism
		err := syncAction.Action(o.ctx, o.options, nil)

		if err != nil {
			// Check if canceled
			if ctx.Err() == context.Canceled {
				progress <- Progress{
					OperationID: o.id,
					Status:      StatusCanceled,
					Step:        "Sync canceled",
				}
				return
			}

			progress <- Progress{
				OperationID: o.id,
				Status:      StatusFailed,
				Step:        "Sync failed",
				Error:       err,
			}
			return
		}

		progress <- Progress{
			OperationID: o.id,
			Status:      StatusCompleted,
			Step:        "Sync complete",
			Current:     3,
			Total:       3,
		}
	}()

	return progress
}
