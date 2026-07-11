package handlers

import (
	"net/http"

	"github.com/getstackit/stackit/internal/api/registry"
	httpcontract "github.com/getstackit/stackit/internal/contracts/http"
	"github.com/getstackit/stackit/internal/git"
)

// defaultBranchDiffConcurrency caps simultaneous diff computations. Each one
// shells out to git, so an unbounded public endpoint could spawn a git
// process per request and exhaust the host.
const defaultBranchDiffConcurrency = 8

// BranchDiffHandler serves raw branch patch diffs.
type BranchDiffHandler struct {
	reg *registry.Registry
	// sem bounds concurrent git invocations. Requests block (respecting the
	// request context) until a slot frees, so callers queue rather than fan
	// out unbounded git subprocesses.
	sem chan struct{}
}

// NewBranchDiffHandler creates a handler that resolves the per-request repo
// from the registry. maxConcurrent bounds simultaneous git diff subprocesses;
// 0 takes the default.
func NewBranchDiffHandler(reg *registry.Registry, maxConcurrent int) *BranchDiffHandler {
	if maxConcurrent <= 0 {
		maxConcurrent = defaultBranchDiffConcurrency
	}
	return &BranchDiffHandler{
		reg: reg,
		sem: make(chan struct{}, maxConcurrent),
	}
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

	branchName := r.PathValue("name")
	if branchName == "" {
		http.Error(w, "missing branch", http.StatusBadRequest)
		return
	}
	if !validateBranchName(w, branchName) {
		return
	}

	branch := entry.Engine.GetBranch(branchName)
	if !branch.IsTracked() {
		http.Error(w, "branch not found or not tracked", http.StatusNotFound)
		return
	}

	// Bound concurrent git subprocesses. Block until a slot frees or the
	// client goes away, so a burst of diff requests queues instead of
	// spawning a git process each.
	select {
	case h.sem <- struct{}{}:
		defer func() { <-h.sem }()
	case <-r.Context().Done():
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

	patch, err := entry.Engine.GetDiffBetween(r.Context(), git.RevRange{Base: baseRevision, Head: headRevision})
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
