package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/getstackit/stackit/internal/api/registry"
	httpcontract "github.com/getstackit/stackit/internal/contracts/http"
)

// RepoHandler serves repository metadata.
type RepoHandler struct {
	reg *registry.Registry
}

// NewRepoHandler creates a handler that resolves the per-request repo from
// the registry.
func NewRepoHandler(reg *registry.Registry) *RepoHandler {
	return &RepoHandler{reg: reg}
}

// ServeHTTP handles GET repo endpoints.
func (h *RepoHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	entry, ok := resolveRepo(h.reg, w, r)
	if !ok {
		return
	}

	owner, repo := "", ""
	if entry.GitHub != nil {
		owner, repo = entry.GitHub.GetOwnerRepo()
	}

	resp := httpcontract.RepoResponse{
		Owner:         owner,
		Repo:          repo,
		Trunk:         entry.Engine.Trunk().GetName(),
		CurrentBranch: entry.Engine.CurrentBranch().GetName(),
		Remote:        entry.Remote,
	}

	writeJSON(w, resp)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
}
