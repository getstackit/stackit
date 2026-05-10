package handlers

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/getstackit/stackit/internal/actions/submit"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/github"
	"github.com/getstackit/stackit/internal/output"
)

// SubmitHandler handles POST requests to submit a stack.
type SubmitHandler struct {
	eng      engine.Engine
	gh       github.Client
	repoRoot string
}

// NewSubmitHandler creates a handler for /api/stacks/{rootBranch}/submit.
func NewSubmitHandler(eng engine.Engine, gh github.Client, repoRoot string) *SubmitHandler {
	return &SubmitHandler{eng: eng, gh: gh, repoRoot: repoRoot}
}

type submitResponse struct {
	Success  bool           `json:"success"`
	Message  string         `json:"message"`
	Branches []branchResult `json:"branches,omitempty"`
}

type branchResult struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	URL    string `json:"url,omitempty"`
	Error  string `json:"error,omitempty"`
}

// ServeHTTP handles the submit request.
func (h *SubmitHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rootBranch, hasRoot := parseResourcePath(r.URL.Path, "submit")
	if !hasRoot || rootBranch == "" {
		http.NotFound(w, r)
		return
	}

	ctx := app.NewContext(h.eng,
		app.WithRepoRoot(h.repoRoot),
		app.WithInteractive(false),
		app.WithWriter(io.Discard),
		app.WithLogger(output.NewNullLogger()),
	)
	ctx.Context = r.Context()
	ctx.GitHubClient = h.gh

	opts := submit.Options{
		Branch:     rootBranch,
		Restack:    true,
		StackRange: engine.StackRangeUpstack(true),
	}

	handler := submit.NewChannelHandler(64)
	var branches []branchResult
	var completionMsg string
	var success bool

	// Run submit in a goroutine, collect events
	done := make(chan error, 1)
	go func() {
		err := submit.Action(ctx, opts, handler)
		handler.Close()
		done <- err
	}()

	// Collect events
	for event := range handler.Events() {
		switch e := event.(type) {
		case submit.BranchProgressEvent:
			br := branchResult{
				Name:   e.BranchName,
				Status: string(e.Status),
				URL:    e.URL,
			}
			if e.Error != nil {
				br.Error = e.Error.Error()
			}
			branches = append(branches, br)
		case submit.CompletionEvent:
			success = e.Success
			completionMsg = e.Message
		}
	}

	// Wait for submit to finish
	if err := <-done; err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		if encErr := json.NewEncoder(w).Encode(submitResponse{
			Success:  false,
			Message:  err.Error(),
			Branches: branches,
		}); encErr != nil {
			http.Error(w, encErr.Error(), http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(submitResponse{
		Success:  success,
		Message:  completionMsg,
		Branches: branches,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
