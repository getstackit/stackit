package stack

import (
	"testing"

	"github.com/stretchr/testify/require"

	syncAction "github.com/getstackit/stackit/internal/actions/sync"
)

// conflictResolvingHandler is a fake sync handler that simulates an interactive
// handler which would enter the conflict resolution workflow. It embeds
// NullHandler and overrides only IsInteractive and PromptResolveConflicts.
//
// Without the postMergeSyncHandler wrapper, sync.Action would call
// PromptResolveConflicts on this handler, get true, and invoke
// EnterConflictWorkflow — which detaches HEAD. The merge next post-merge flow
// must never reach that path, so the wrapper has to override the prompt
// regardless of what the underlying handler would have answered.
type conflictResolvingHandler struct {
	syncAction.NullHandler
}

func (h *conflictResolvingHandler) IsInteractive() bool { return true }

func (h *conflictResolvingHandler) PromptResolveConflicts(_ []string) (bool, error) {
	return true, nil
}

func TestPostMergeSyncHandler_SkipsConflictWorkflow(t *testing.T) {
	t.Parallel()

	// Wrap a handler that would otherwise enter the conflict workflow.
	wrapped := &postMergeSyncHandler{Handler: &conflictResolvingHandler{}}

	// The wrapper must override the prompt to skip the conflict workflow,
	// even though the underlying handler answers "yes". Returning true here
	// would route sync.Action into EnterConflictWorkflow, which detaches HEAD
	// and violates the safety invariant for merge next.
	resolve, err := wrapped.PromptResolveConflicts([]string{"branch-a"})
	require.NoError(t, err)
	require.False(t, resolve, "post-merge sync must never enter the conflict workflow")
}

func TestPostMergeSyncHandler_DelegatesIsInteractive(t *testing.T) {
	t.Parallel()

	// IsInteractive must delegate to the wrapped handler so that other
	// interactive prompts (metadata conflicts, branch deletions) keep working.
	// sync.Action only reaches the conflict prompt when IsInteractive is true,
	// so the wrapper must not flip it to false to "fix" the detached HEAD.
	wrapped := &postMergeSyncHandler{Handler: &conflictResolvingHandler{}}
	require.True(t, wrapped.IsInteractive(), "wrapper must delegate IsInteractive to the underlying handler")
}
