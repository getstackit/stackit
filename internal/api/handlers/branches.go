package handlers

import (
	"context"
	"net/http"

	"github.com/getstackit/stackit/internal/actions/stackview"
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
		h.listBranches(r.Context(), w, r, entry)
		return
	}
	if !validateBranchName(w, branchName) {
		return
	}
	h.getBranch(r.Context(), w, r, entry, branchName)
}

func (h *BranchesHandler) listBranches(ctx context.Context, w http.ResponseWriter, r *http.Request, entry *registry.RepoEntry) {
	graph := entry.Engine.Graph(ctx, engine.SortStrategyAlphabetical)
	branches := entry.Engine.AllBranches().Filter(engine.Branch.IsTracked)

	var checksMap github.ChecksByBranch
	if entry.GitHub != nil {
		checksMap, _ = entry.GitHub.BatchGetPRChecksStatus(r.Context(), branches.Names())
	}

	// One batch pass over every branch instead of one of each read per branch.
	data := stackview.FetchBranchData(r.Context(), entry.Engine, branches)

	responses := make([]httpcontract.BranchResponse, 0, len(branches))
	for _, branch := range branches {
		node := graph.GetNode(branch.Name())
		if node == nil {
			continue
		}
		responses = append(responses, httpcontract.MapBranch(ctx, entry.Engine, httpcontract.NewBranchInput(node, checksMap.Get(branch.Name()), data)))
	}

	writeJSON(w, responses)
}

func (h *BranchesHandler) getBranch(ctx context.Context, w http.ResponseWriter, r *http.Request, entry *registry.RepoEntry, branchName string) {
	branch := entry.Engine.GetBranch(branchName)
	if !branch.IsTracked() {
		http.Error(w, "branch not found or not tracked", http.StatusNotFound)
		return
	}

	graph := entry.Engine.Graph(ctx, engine.SortStrategyAlphabetical)
	node := graph.GetNode(branchName)
	if node == nil {
		http.Error(w, "branch not in stack graph", http.StatusNotFound)
		return
	}

	var checks *github.CheckStatus
	if entry.GitHub != nil {
		checksMap, _ := entry.GitHub.BatchGetPRChecksStatus(r.Context(), []string{branchName})
		checks = checksMap.Get(branchName)
	}

	data := stackview.FetchBranchData(r.Context(), entry.Engine, engine.BranchesOf(branch))
	writeJSON(w, httpcontract.MapBranch(ctx, entry.Engine, httpcontract.NewBranchInput(node, checks, data)))
}
