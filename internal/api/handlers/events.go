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
	reg         *registry.Registry
	limiter     *sseLimiter
	maxLifetime time.Duration
}

// NewEventsHandler creates a handler that resolves the per-request repo
// from the registry and streams from that repo's broadcaster. limits bound
// concurrent connections so an unauthenticated public deployment can't be
// exhausted by clients opening unbounded streams; zero fields take the
// package defaults.
func NewEventsHandler(reg *registry.Registry, limits SSELimits) *EventsHandler {
	limits = limits.withDefaults()
	return &EventsHandler{
		reg:         reg,
		limiter:     newSSELimiter(limits.MaxGlobal, limits.MaxPerIP),
		maxLifetime: limits.MaxLifetime,
	}
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

	// SSE is long-lived; opt out of the server-wide WriteTimeout so the
	// connection isn't dropped after the first 30 seconds. Failing here is
	// not fatal — without it the connection is shorter-lived but otherwise
	// works.
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{})

	entry, ok := resolveRepo(h.reg, w, r)
	if !ok {
		return
	}
	if entry.Broadcaster == nil {
		http.Error(w, "events unavailable for this repo", http.StatusServiceUnavailable)
		return
	}

	// Reserve a connection slot before streaming. A public server must cap
	// concurrent streams or anyone can exhaust it by opening connections.
	ip := ClientIP(r)
	if !h.limiter.acquire(ip) {
		http.Error(w, "too many connections", http.StatusTooManyRequests)
		return
	}
	defer h.limiter.release(ip)

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

	// Bound how long a single connection can hold its slot. EventSource
	// reconnects on its own, so this is transparent to clients.
	lifetime := time.NewTimer(h.maxLifetime)
	defer lifetime.Stop()

	// Periodic keepalive: keeps proxies from idling the stream out and makes
	// a dead client fail the write below, releasing its slot promptly.
	heartbeat := time.NewTicker(sseHeartbeatInterval)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-entry.Broadcaster.Done():
			return
		case <-lifetime.C:
			return
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
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
