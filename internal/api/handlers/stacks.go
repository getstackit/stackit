package handlers

import (
	"net/http"
	"strings"

	"stackit.dev/stackit/internal/actions/merge"
	httpcontract "stackit.dev/stackit/internal/contracts/http"
	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/github"
)

// StacksHandler serves stack data.
type StacksHandler struct {
	eng engine.BranchReader
	gh  github.Client
}

// NewStacksHandler creates a handler for /api/stacks and /api/v1/stacks.
func NewStacksHandler(eng engine.BranchReader, gh github.Client) *StacksHandler {
	return &StacksHandler{eng: eng, gh: gh}
}

// ServeHTTP handles GET stacks endpoints.
func (h *StacksHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	root, hasRoot := parseResourcePath(r.URL.Path, "stacks")
	if !hasRoot {
		http.NotFound(w, r)
		return
	}

	if root == "" {
		h.listStacks(w)
	} else {
		h.getStack(w, r, root)
	}
}

func (h *StacksHandler) listStacks(w http.ResponseWriter) {
	stacks, err := merge.DiscoverStacksWithSort(h.eng, engine.SortStrategySmart)
	if err != nil {
		http.Error(w, "failed to discover stacks: "+err.Error(), http.StatusInternalServerError)
		return
	}

	graph := engine.BuildStackGraph(h.eng, engine.SortStrategySmart, nil)

	summaries := make([]httpcontract.StackSummary, 0, len(stacks))
	for _, stack := range stacks {
		summary := httpcontract.MapStackSummary(h.eng, graph, stack.RootBranch, stack.AllBranches, stack.PRCount, stack.Scope, "")
		summaries = append(summaries, summary)
	}

	writeJSON(w, summaries)
}

func (h *StacksHandler) getStack(w http.ResponseWriter, r *http.Request, rootBranch string) {
	stacks, err := merge.DiscoverStacksWithSort(h.eng, engine.SortStrategySmart)
	if err != nil {
		http.Error(w, "failed to discover stacks: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Find the requested stack
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

	graph := engine.BuildStackGraph(h.eng, engine.SortStrategySmart, nil)

	// Fetch CI checks if GitHub client is available
	var checksMap map[string]*github.CheckStatus
	if h.gh != nil {
		checksMap, _ = h.gh.BatchGetPRChecksStatus(r.Context(), found.AllBranches)
	}

	detail := httpcontract.MapStackDetail(h.eng, graph, found.RootBranch, found.AllBranches, found.PRCount, found.Scope, checksMap)
	writeJSON(w, detail)
}

func parseResourcePath(requestPath, resource string) (string, bool) {
	marker := "/" + resource
	_, after, ok := strings.Cut(requestPath, marker)
	if !ok {
		return "", false
	}

	suffix := strings.TrimPrefix(after, "/")
	return suffix, true
}
