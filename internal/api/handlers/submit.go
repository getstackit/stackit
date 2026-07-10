package handlers

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"github.com/getstackit/stackit/internal/actions/submit"
	"github.com/getstackit/stackit/internal/api/auth"
	"github.com/getstackit/stackit/internal/api/registry"
	"github.com/getstackit/stackit/internal/api/reqid"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/output"
)

// SubmitHandler handles POST requests to submit a stack.
type SubmitHandler struct {
	reg *registry.Registry
}

// NewSubmitHandler creates a handler that resolves the per-request repo
// from the registry.
func NewSubmitHandler(reg *registry.Registry) *SubmitHandler {
	return &SubmitHandler{reg: reg}
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

	entry, ok := resolveRepo(h.reg, w, r)
	if !ok {
		return
	}

	rootBranch := r.PathValue("rootBranch")
	if rootBranch == "" {
		http.NotFound(w, r)
		return
	}
	if !validateBranchName(w, rootBranch) {
		return
	}

	// Audit line for a mutating call. "actor" is whichever GitHub login
	// the session belongs to; unauthenticated submits (only possible
	// when -auth-disabled is set on a private deploy) log actor=<none>.
	actor := "<none>"
	if sess := auth.SessionFromContext(r.Context()); sess != nil {
		actor = sess.GitHubLogin
	}
	slog.Info("audit", "action", "submit", "actor", actor, "repo", entry.ID, "branch", rootBranch, "request_id", reqid.FromContext(r.Context())) //nolint:gosec // values emitted as structured slog fields

	ctx := app.NewContext(entry.Engine,
		app.WithRepoRoot(entry.RepoRoot),
		app.WithInteractive(false),
		app.WithWriter(io.Discard),
		app.WithLogger(output.NewNullLogger()),
	)
	ctx.Context = r.Context()
	ctx.GitHubClient = entry.GitHub

	opts := submit.Options{
		Branch:     rootBranch,
		Restack:    true,
		StackRange: engine.StackRangeUpstack(true),
	}

	handler := submit.NewChannelHandler(64)
	var branches []branchResult
	var completionMsg string
	var success bool

	done := make(chan error, 1)
	go func() {
		err := submit.Action(ctx, opts, handler)
		handler.Close()
		done <- err
	}()

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
			success = e.Success()
			completionMsg = e.Message
		}
	}

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
