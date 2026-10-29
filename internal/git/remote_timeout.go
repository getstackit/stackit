package git

import (
	"context"
	"time"
)

// DefaultRemoteOperationTimeout bounds explicit remote operations launched by
// commands when the parent command context does not already carry a deadline.
const DefaultRemoteOperationTimeout = 5 * time.Minute

// WithRemoteOperationTimeout applies the default remote-operation timeout only
// when the parent context has no deadline. The returned cancel function should
// always be called by the caller.
func WithRemoteOperationTimeout(parent context.Context) (context.Context, context.CancelFunc) {
	if _, ok := parent.Deadline(); ok {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, DefaultRemoteOperationTimeout)
}
