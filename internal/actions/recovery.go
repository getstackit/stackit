package actions

import "context"

// RecoveryEngine is the engine surface shared by the commands that unwind a
// halted operation (abort, undo): abandoning Git's in-progress rebase or merge
// and rolling refs and the working tree back to a snapshot. Each command
// embeds it in its own consumer-side interface alongside the few extra methods
// it needs.
type RecoveryEngine interface {
	IsRebaseInProgress(ctx context.Context) bool
	IsMergeInProgress(ctx context.Context) bool
	RebaseAbort(ctx context.Context) error
	MergeAbort(ctx context.Context) error
	RestoreSnapshot(ctx context.Context, snapshotID string) error
	RestoreWorktree(ctx context.Context, snapshotID string) (bool, error)
}
