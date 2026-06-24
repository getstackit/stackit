package handlers

import (
	"net/http"

	"github.com/getstackit/stackit/internal/api/registry"
	httpcontract "github.com/getstackit/stackit/internal/contracts/http"
)

// ReposListHandler serves GET /api/v1/repos — the unscoped index of repos
// the server is configured to serve. Clients use this to render a picker
// before scoping subsequent calls to /api/v1/repos/{repoID}/...
type ReposListHandler struct {
	reg *registry.Registry
}

// NewReposListHandler creates a handler backed by reg.
func NewReposListHandler(reg *registry.Registry) *ReposListHandler {
	return &ReposListHandler{reg: reg}
}

// ServeHTTP returns one RepoSummary per registry entry, sorted by ID.
func (h *ReposListHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	user := requestUser(r)
	entries := h.reg.List()
	summaries := make([]httpcontract.RepoSummary, 0, len(entries))
	for _, entry := range entries {
		if !visibleTo(entry, user) {
			continue
		}
		summary := httpcontract.RepoSummary{
			ID:          entry.ID,
			Owner:       entry.Owner,
			Repo:        entry.Name,
			DisplayName: entry.DisplayName,
		}
		if entry.Engine != nil {
			summary.Trunk = entry.Engine.Trunk().GetName()
			if current := entry.Engine.CurrentBranch(); current != nil {
				summary.CurrentBranch = current.GetName()
			}
		}
		summaries = append(summaries, summary)
	}

	writeJSON(w, httpcontract.ReposListResponse{Repos: summaries})
}
