package handlers

import (
	"encoding/json"
	"net/http"

	httpcontract "stackit.dev/stackit/internal/contracts/http"
	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/github"
)

// RepoHandler serves repository metadata.
type RepoHandler struct {
	eng    engine.BranchReader
	gh     github.Client
	remote string
}

// NewRepoHandler creates a handler for /api/repo and /api/v1/repo.
func NewRepoHandler(eng engine.BranchReader, gh github.Client, remote string) *RepoHandler {
	return &RepoHandler{eng: eng, gh: gh, remote: remote}
}

// ServeHTTP handles GET repo endpoints.
func (h *RepoHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	owner, repo := "", ""
	if h.gh != nil {
		owner, repo = h.gh.GetOwnerRepo()
	}

	resp := httpcontract.RepoResponse{
		Owner:         owner,
		Repo:          repo,
		Trunk:         h.eng.Trunk().GetName(),
		CurrentBranch: h.eng.CurrentBranch().GetName(),
		Remote:        h.remote,
	}

	writeJSON(w, resp)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
}
