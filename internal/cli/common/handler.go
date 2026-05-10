package common

import (
	"sync"

	"github.com/getstackit/stackit/internal/output"
)

// BaseHandler provides common functionality for CLI handlers.
// Embed this in handler structs to reduce boilerplate.
type BaseHandler struct {
	Output output.Output
	mu     sync.Mutex
}

// NewBaseHandler creates a new BaseHandler with the given output.
func NewBaseHandler(out output.Output) BaseHandler {
	return BaseHandler{Output: out}
}

// Lock acquires the handler's mutex.
func (h *BaseHandler) Lock() {
	h.mu.Lock()
}

// Unlock releases the handler's mutex.
func (h *BaseHandler) Unlock() {
	h.mu.Unlock()
}

// Cleanup is a default no-op implementation.
func (h *BaseHandler) Cleanup() {}

// IsInteractive returns false by default (for simple handlers).
func (h *BaseHandler) IsInteractive() bool {
	return false
}
