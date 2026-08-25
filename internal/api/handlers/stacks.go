package handlers

import (
	"net/http"

	"github.com/getstackit/stackit/internal/actions/merge"
	"github.com/getstackit/stackit/internal/api/registry"
	httpcontract "github.com/getstackit/stackit/internal/contracts/http"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/github"
)

// StacksHandler serves stack data.
type StacksHandler struct {
	reg *registry.Registry
}

// NewStacksHandler creates a handler that resolves the per-request repo
// from the registry.
func NewStacksHandler(reg *registry.Registry) *StacksHandler {
	return &StacksHandler{reg: reg}
}

// ServeHTTP handles GET stacks endpoints.
func (h *StacksHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	entry, ok := resolveRepo(h.reg, w, r)
	if !ok {
		return
	}

	root := r.PathValue("name")
	if root == "" {
		h.listStacks(w, entry)
	} else {
		h.getStack(w, r, entry, root)
	}
}

func (h *StacksHandler) listStacks(w http.ResponseWriter, entry *registry.RepoEntry) {
	stacks, err := merge.DiscoverStacksWithSort(entry.Engine, engine.SortStrategySmart)
	if err != nil {
		http.Error(w, "failed to discover stacks: "+err.Error(), http.StatusInternalServerError)
		return
	}

	graph := entry.Engine.Graph(engine.SortStrategySmart)

	// One restack-status pass for every stack combined, instead of one per
	// stack.
	branchCount := 0
	for _, stack := range stacks {
		branchCount += len(stack.AllBranches)
	}
	allBranches := make(engine.Branches, 0, branchCount)
	for _, stack := range stacks {
		allBranches = append(allBranches, graph.BranchesByNames(stack.AllBranches)...)
	}
	statuses := entry.Engine.ReadBranchStatuses(allBranches)

	summaries := make([]httpcontract.StackSummary, 0, len(stacks))
	for _, stack := range stacks {
		summary := httpcontract.MapStackSummary(entry.Engine, graph, stack.RootBranch, stack.AllBranches, stack.PRCount, stack.Scope, "", statuses)
		summaries = append(summaries, summary)
	}

	writeJSON(w, summaries)
}

func (h *StacksHandler) getStack(w http.ResponseWriter, r *http.Request, entry *registry.RepoEntry, rootBranch string) {
	stacks, err := merge.DiscoverStacksWithSort(entry.Engine, engine.SortStrategySmart)
	if err != nil {
		http.Error(w, "failed to discover stacks: "+err.Error(), http.StatusInternalServerError)
		return
	}

	var found *merge.MultiStackInfo
	for i := range stacks {
		if stacks[i].RootBranch == rootBranch {
			found = &stacks[i]
			break
		}
	}
	if found == nil {
		http.Error(w, "stack not found", http.StatusNotFound)
		return
	}

	graph := entry.Engine.Graph(engine.SortStrategySmart)

	var checksMap github.ChecksByBranch
	if entry.GitHub != nil {
		checksMap, _ = entry.GitHub.BatchGetPRChecksStatus(r.Context(), found.AllBranches)
	}

	data := httpcontract.BatchReadStackData(r.Context(), entry.Engine, graph.BranchesByNames(found.AllBranches))
	detail := httpcontract.MapStackDetail(entry.Engine, graph, found.RootBranch, found.AllBranches, found.PRCount, found.Scope, checksMap, data)
	writeJSON(w, detail)
}
