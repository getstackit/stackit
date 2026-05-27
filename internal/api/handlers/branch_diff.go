package handlers

import (
	"net/http"

	"github.com/getstackit/stackit/internal/api/registry"
	httpcontract "github.com/getstackit/stackit/internal/contracts/http"
)

// BranchDiffHandler serves raw branch patch diffs.
type BranchDiffHandler struct {
	reg *registry.Registry
}

// NewBranchDiffHandler creates a handler that resolves the per-request repo
// from the registry.
func NewBranchDiffHandler(reg *registry.Registry) *BranchDiffHandler {
	return &BranchDiffHandler{reg: reg}
}

// ServeHTTP handles GET branch diff endpoint.
func (h *BranchDiffHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	entry, ok := resolveRepo(h.reg, w, r)
	if !ok {
		return
	}

	branchName := r.URL.Query().Get("branch")
	if branchName == "" {
		http.Error(w, "missing branch query parameter", http.StatusBadRequest)
		return
	}

	branch := entry.Engine.GetBranch(branchName)
	if !branch.IsTracked() {
		http.Error(w, "branch not found or not tracked", http.StatusNotFound)
		return
	}

	baseRevision, err := entry.Engine.GetDivergencePoint(branchName)
	if err != nil {
		http.Error(w, "failed to resolve branch base: "+err.Error(), http.StatusInternalServerError)
		return
	}

	headRevision, err := branch.GetRevision()
	if err != nil {
		http.Error(w, "failed to resolve branch revision: "+err.Error(), http.StatusInternalServerError)
		return
	}

	patch, err := entry.Engine.GetDiffBetween(r.Context(), baseRevision, headRevision)
	if err != nil {
		http.Error(w, "failed to compute branch diff: "+err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, httpcontract.BranchDiffResponse{
		Branch:       branchName,
		BaseRevision: baseRevision,
		HeadRevision: headRevision,
		Patch:        patch,
	})
}
