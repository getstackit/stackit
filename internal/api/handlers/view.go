package handlers

import (
	"net/http"

	"stackit.dev/stackit/internal/actions/merge"
	httpcontract "stackit.dev/stackit/internal/contracts/http"
	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/github"
)

// ViewHandler serves the combined view payload for the frontend.
type ViewHandler struct {
	eng    engine.BranchReader
	gh     github.Client
	remote string
}

// NewViewHandler creates a handler for /api/view and /api/v1/view.
func NewViewHandler(eng engine.BranchReader, gh github.Client, remote string) *ViewHandler {
	return &ViewHandler{eng: eng, gh: gh, remote: remote}
}

// ServeHTTP handles GET view endpoints.
func (h *ViewHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Build repo info
	owner, repo := "", ""
	var currentUser string
	if h.gh != nil {
		owner, repo = h.gh.GetOwnerRepo()
		currentUser, _ = h.gh.GetCurrentUser(r.Context())
	}
	repoResp := httpcontract.RepoResponse{
		Owner:         owner,
		Repo:          repo,
		Trunk:         h.eng.Trunk().GetName(),
		CurrentBranch: h.eng.CurrentBranch().GetName(),
		Remote:        h.remote,
		CurrentUser:   currentUser,
	}

	// Discover all stacks
	stacks, err := merge.DiscoverStacksWithSort(h.eng, engine.SortStrategySmart)
	if err != nil {
		http.Error(w, "failed to discover stacks: "+err.Error(), http.StatusInternalServerError)
		return
	}

	graph := engine.BuildStackGraph(h.eng, engine.SortStrategySmart, nil)

	// Collect all branch names across all stacks for a single batched CI check
	var allBranches []string
	for _, stack := range stacks {
		allBranches = append(allBranches, stack.AllBranches...)
	}

	var checksMap map[string]*github.CheckStatus
	if h.gh != nil && len(allBranches) > 0 {
		checksMap, _ = h.gh.BatchGetPRChecksStatus(r.Context(), allBranches)
	}

	// Map all stack details using the shared checks map
	details := make([]httpcontract.StackDetail, 0, len(stacks))
	for _, stack := range stacks {
		detail := httpcontract.MapStackDetail(h.eng, graph, stack.RootBranch, stack.AllBranches, stack.PRCount, stack.Scope, checksMap)
		details = append(details, detail)
	}

	writeJSON(w, httpcontract.ViewResponse{
		Repo:   repoResp,
		Stacks: details,
	})
}
