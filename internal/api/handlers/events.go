package handlers

import (
	"fmt"
	"net/http"
	"sync"
	"time"
)

// EventBroadcaster manages SSE connections and broadcasts events.
type EventBroadcaster struct {
	mu      sync.RWMutex
	clients map[chan string]struct{}
}

// NewEventBroadcaster creates a new SSE event broadcaster.
func NewEventBroadcaster() *EventBroadcaster {
	return &EventBroadcaster{
		clients: make(map[chan string]struct{}),
	}
}

// Broadcast sends an SSE event to all connected clients.
func (b *EventBroadcaster) Broadcast(event, data string) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	msg := fmt.Sprintf("event: %s\ndata: %s\n\n", event, data)
	for ch := range b.clients {
		select {
		case ch <- msg:
		default:
			// Drop message if client buffer is full
		}
	}
}

func (b *EventBroadcaster) subscribe() chan string {
	ch := make(chan string, 16)
	b.mu.Lock()
	b.clients[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

func (b *EventBroadcaster) unsubscribe(ch chan string) {
	b.mu.Lock()
	delete(b.clients, ch)
	b.mu.Unlock()
	close(ch)
}

// EventsHandler serves the SSE stream at GET /api/events and /api/v1/events.
type EventsHandler struct {
	broadcaster *EventBroadcaster
}

// NewEventsHandler creates a handler for events endpoints.
func NewEventsHandler(broadcaster *EventBroadcaster) *EventsHandler {
	return &EventsHandler{broadcaster: broadcaster}
}

// ServeHTTP handles the SSE connection.
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

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := h.broadcaster.subscribe()
	defer h.broadcaster.unsubscribe(ch)

	// Send initial heartbeat
	if _, err := fmt.Fprintf(w, "event: connected\ndata: {\"timestamp\":\"%s\"}\n\n", time.Now().Format(time.RFC3339)); err != nil {
		return
	}
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
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
