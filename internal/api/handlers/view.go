package handlers

import (
	"net/http"

	"github.com/getstackit/stackit/internal/api/registry"
)

// ViewHandler serves the combined view payload for the frontend.
type ViewHandler struct {
	reg *registry.Registry
}

// NewViewHandler creates a handler that resolves the per-request repo from
// the registry. Assembly logic lives in ViewAssembler so this handler stays
// transport-focused.
func NewViewHandler(reg *registry.Registry) *ViewHandler {
	return &ViewHandler{reg: reg}
}

// ServeHTTP handles GET view endpoints.
func (h *ViewHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	entry, ok := resolveRepo(h.reg, w, r)
	if !ok {
		return
	}

	assembler := NewViewAssembler(entry.Engine, entry.GitHub, entry.Remote)
	view, err := assembler.Build(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, view)
}
