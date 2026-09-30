package merge

import (
	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/actions/sync"
	"github.com/getstackit/stackit/internal/app"
)

// PostMergeSyncOptions configures PostMergeSync.
type PostMergeSyncOptions struct {
	// RestackScope limits the restack to these branches (empty = sync default).
	RestackScope []string
	// OnTrunkCheckedOut, when set, is called after trunk is checked out and
	// before sync runs, so adapters can surface worktree-switch directives in
	// order.
	OnTrunkCheckedOut func(result actions.CheckoutResult)
}

// PostMergeSyncResult reports what the post-merge sync did.
type PostMergeSyncResult struct {
	// Summary is the sync summary; ConflictBranches lists restacks that were
	// left unresolved.
	Summary sync.Summary
}

// PostMergeCheckoutError is returned by PostMergeSync when trunk could not be
// checked out, so no sync was attempted. Callers typically render recovery
// instructions (stash or commit local changes) for this case.
type PostMergeCheckoutError struct {
	Err error
}

func (e *PostMergeCheckoutError) Error() string { return e.Err.Error() }

func (e *PostMergeCheckoutError) Unwrap() error { return e.Err }

// PostMergeSync runs the follow-up after a PR lands: check out trunk, then
// sync with restack.
//
// newSyncHandler is called only once trunk is checked out, so an adapter can
// defer starting a sync UI until there is a sync to show.
//
// It never enters the conflict-resolution workflow, whatever the handler answers:
// that workflow detaches HEAD, and a merge command must never leave the user
// in detached HEAD. Restack conflicts are left in the returned summary for the
// caller to report or fail on.
func PostMergeSync(ctx *app.Context, opts PostMergeSyncOptions, newSyncHandler func() sync.Handler) (PostMergeSyncResult, error) {
	checkoutResult, err := actions.CheckoutAction(ctx, actions.CheckoutOptions{
		CheckoutTrunk: true,
	}, nil)
	if err != nil {
		return PostMergeSyncResult{}, &PostMergeCheckoutError{Err: err}
	}
	if opts.OnTrunkCheckedOut != nil {
		opts.OnTrunkCheckedOut(checkoutResult)
	}

	wrapped := &postMergeSyncHandler{Handler: newSyncHandler()}
	err = sync.Action(ctx, sync.Options{
		Restack:      true,
		RestackScope: opts.RestackScope,
	}, wrapped)
	return PostMergeSyncResult{Summary: wrapped.summary}, err
}

// postMergeSyncHandler wraps a sync handler to prevent entering the conflict
// resolution workflow after a merge, and captures the sync summary.
// EnterConflictWorkflow detaches HEAD, which would leave the user in an
// unexpected state after a merge operation.
type postMergeSyncHandler struct {
	sync.Handler
	summary sync.Summary
}

// PromptResolveConflicts always returns false to skip entering the conflict
// workflow. Conflicts are shown in the summary with instructions to run
// 'stackit restack' manually.
func (h *postMergeSyncHandler) PromptResolveConflicts(_ []string) (bool, error) {
	return false, nil
}

// Complete records the summary and forwards it to the wrapped handler.
func (h *postMergeSyncHandler) Complete(summary sync.Summary) {
	h.summary = summary
	h.Handler.Complete(summary)
}
