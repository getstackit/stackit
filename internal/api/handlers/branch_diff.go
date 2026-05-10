package handlers

import (
	"net/http"

	httpcontract "github.com/getstackit/stackit/internal/contracts/http"
	"github.com/getstackit/stackit/internal/engine"
)

// BranchDiffHandler serves raw branch patch diffs.
type BranchDiffHandler struct {
	eng engine.BranchReader
}

// NewBranchDiffHandler creates a handler for /api/branch-diff and /api/v1/branch-diff.
func NewBranchDiffHandler(eng engine.BranchReader) *BranchDiffHandler {
	return &BranchDiffHandler{eng: eng}
}

// ServeHTTP handles GET branch diff endpoint.
func (h *BranchDiffHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	branchName := r.URL.Query().Get("branch")
	if branchName == "" {
		http.Error(w, "missing branch query parameter", http.StatusBadRequest)
		return
	}

	branch := h.eng.GetBranch(branchName)
	if !branch.IsTracked() {
		http.Error(w, "branch not found or not tracked", http.StatusNotFound)
		return
	}

	baseRevision, err := h.eng.GetDivergencePoint(branchName)
	if err != nil {
		http.Error(w, "failed to resolve branch base: "+err.Error(), http.StatusInternalServerError)
		return
	}

	headRevision, err := branch.GetRevision()
	if err != nil {
		http.Error(w, "failed to resolve branch revision: "+err.Error(), http.StatusInternalServerError)
		return
	}

	patch, err := h.eng.GetDiffBetween(r.Context(), baseRevision, headRevision)
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
