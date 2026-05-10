package handlers

import (
	"net/http"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/github"
)

// ViewHandler serves the combined view payload for the frontend.
type ViewHandler struct {
	assembler *ViewAssembler
}

// NewViewHandler creates a handler for /api/view and /api/v1/view.
// Assembly logic lives in ViewAssembler to keep this handler transport-focused.
func NewViewHandler(eng engine.BranchReader, gh github.Client, remote string) *ViewHandler {
	return &ViewHandler{
		assembler: NewViewAssembler(eng, gh, remote),
	}
}

// ServeHTTP handles GET view endpoints.
func (h *ViewHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	view, err := h.assembler.Build(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, view)
}
