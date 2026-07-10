package handlers

import (
	"net/http"

	"github.com/getstackit/stackit/internal/api/registry"
	httpcontract "github.com/getstackit/stackit/internal/contracts/http"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/github"
)

// BranchesHandler serves branch data.
type BranchesHandler struct {
	reg *registry.Registry
}

// NewBranchesHandler creates a handler that resolves the per-request repo
// from the registry.
func NewBranchesHandler(reg *registry.Registry) *BranchesHandler {
	return &BranchesHandler{reg: reg}
}

// ServeHTTP handles GET branches endpoints.
func (h *BranchesHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
		h.listBranches(w, r, entry)
		return
	}
	if !validateBranchName(w, branchName) {
		return
	}
	h.getBranch(w, r, entry, branchName)
}

func (h *BranchesHandler) listBranches(w http.ResponseWriter, r *http.Request, entry *registry.RepoEntry) {
	graph := entry.Engine.Graph(engine.SortStrategyAlphabetical)
	allBranches := entry.Engine.AllBranches()

	branches := make([]engine.Branch, 0, len(allBranches))
	for _, b := range allBranches {
		if b.IsTracked() {
			branches = append(branches, b)
		}
	}

	var checksMap map[string]*github.CheckStatus
	if entry.GitHub != nil {
		names := make([]string, len(branches))
		for i, b := range branches {
			names[i] = b.GetName()
		}
		checksMap, _ = entry.GitHub.BatchGetPRChecksStatus(r.Context(), names)
	}

	// One remote listing, one stats pass, and one commits pass for all
	// branches instead of one of each per branch.
	branchSet := engine.BranchesOf(branches...)
	remoteStatuses := entry.Engine.ReadBranchRemoteStatuses(r.Context(), branchSet)
	stats := entry.Engine.BatchBranchStats(branchSet)
	commitsByBranch := entry.Engine.BatchCommits(branchSet, engine.CommitFormatReadableWithDate)

	responses := make([]httpcontract.BranchResponse, 0, len(branches))
	for _, branch := range branches {
		node := graph.GetNode(branch.GetName())
		if node == nil {
			continue
		}
		var checks *github.CheckStatus
		if checksMap != nil {
			checks = checksMap[branch.GetName()]
		}
		responses = append(responses, httpcontract.MapBranch(entry.Engine, branch, node, checks, remoteStatuses.ForBranch(branch), stats[branch.GetName()], commitsByBranch[branch.GetName()]))
	}

	writeJSON(w, responses)
}

func (h *BranchesHandler) getBranch(w http.ResponseWriter, r *http.Request, entry *registry.RepoEntry, branchName string) {
	branch := entry.Engine.GetBranch(branchName)
	if !branch.IsTracked() {
		http.Error(w, "branch not found or not tracked", http.StatusNotFound)
		return
	}

	graph := entry.Engine.Graph(engine.SortStrategyAlphabetical)
	node := graph.GetNode(branchName)
	if node == nil {
		http.Error(w, "branch not in stack graph", http.StatusNotFound)
		return
	}

	var checks *github.CheckStatus
	if entry.GitHub != nil {
		checksMap, _ := entry.GitHub.BatchGetPRChecksStatus(r.Context(), []string{branchName})
		if checksMap != nil {
			checks = checksMap[branchName]
		}
	}

	branchSet := engine.BranchesOf(branch)
	remoteStatus := entry.Engine.ReadBranchRemoteStatuses(r.Context(), branchSet).ForBranch(branch)
	stat := entry.Engine.BatchBranchStats(branchSet)[branchName]
	commits := entry.Engine.BatchCommits(branchSet, engine.CommitFormatReadableWithDate)[branchName]
	resp := httpcontract.MapBranch(entry.Engine, branch, node, checks, remoteStatus, stat, commits)
	writeJSON(w, resp)
}
