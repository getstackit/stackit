package handlers

import (
	"fmt"
	"net/http"
	"time"

	"github.com/getstackit/stackit/internal/api/registry"
)

// EventsHandler streams server-sent events from the per-repo broadcaster.
// Each connection is scoped to one repo: subscribers to /repos/A/events
// only see events broadcast on repo A.
type EventsHandler struct {
	reg *registry.Registry
}

// NewEventsHandler creates a handler that resolves the per-request repo
// from the registry and streams from that repo's broadcaster.
func NewEventsHandler(reg *registry.Registry) *EventsHandler {
	return &EventsHandler{reg: reg}
}

// ServeHTTP handles the SSE connection for one repo.
func (h *EventsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	entry, ok := resolveRepo(h.reg, w, r)
	if !ok {
		return
	}
	if entry.Broadcaster == nil {
		http.Error(w, "events unavailable for this repo", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch, ok := entry.Broadcaster.Subscribe()
	if !ok {
		http.Error(w, "server shutting down", http.StatusServiceUnavailable)
		return
	}
	defer entry.Broadcaster.Unsubscribe(ch)

	// Send initial heartbeat so the client knows the stream is alive.
	if _, err := fmt.Fprintf(w, "event: connected\ndata: {\"timestamp\":\"%s\"}\n\n", time.Now().Format(time.RFC3339)); err != nil {
		return
	}
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-entry.Broadcaster.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			if _, err := fmt.Fprint(w, msg); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
