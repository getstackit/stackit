package registry

import (
	"fmt"
	"sync"
)

// Broadcaster fans out SSE messages to subscribed clients. Each RepoEntry
// owns one so events stay scoped to a single repo — a client subscribed to
// /repos/A/events never receives B's refresh notifications.
type Broadcaster struct {
	mu         sync.RWMutex
	clients    map[chan string]struct{}
	shutdownCh chan struct{}
	closed     bool
}

// NewBroadcaster creates a new event broadcaster.
func NewBroadcaster() *Broadcaster {
	return &Broadcaster{
		clients:    make(map[chan string]struct{}),
		shutdownCh: make(chan struct{}),
	}
}

// Broadcast sends an SSE event to every subscribed client. Slow clients
// drop the message rather than blocking the broadcast.
func (b *Broadcaster) Broadcast(event, data string) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.closed {
		return
	}

	msg := fmt.Sprintf("event: %s\ndata: %s\n\n", event, data)
	for ch := range b.clients {
		select {
		case ch <- msg:
		default:
			// Drop message if the client's buffer is full.
		}
	}
}

// Done returns a channel that closes when the broadcaster is shutting down.
// SSE handlers select on this to exit promptly during server shutdown.
func (b *Broadcaster) Done() <-chan struct{} {
	return b.shutdownCh
}

// Close notifies all subscribers to exit and prevents future broadcasts.
// It closes every active client channel so SSE handlers blocked on a
// receive unblock immediately. Safe to call multiple times.
func (b *Broadcaster) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	close(b.shutdownCh)
	for ch := range b.clients {
		close(ch)
		delete(b.clients, ch)
	}
}

// Subscribe returns a buffered channel that receives every broadcast
// message until Unsubscribe is called or the broadcaster is closed. The
// bool is false when the broadcaster is already shut down.
func (b *Broadcaster) Subscribe() (chan string, bool) {
	ch := make(chan string, 16)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		close(ch)
		return nil, false
	}
	b.clients[ch] = struct{}{}
	return ch, true
}

// Unsubscribe removes ch from the broadcast set and closes it. Safe to
// call from a defer even if ch was already removed (e.g. by Close).
func (b *Broadcaster) Unsubscribe(ch chan string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.clients[ch]; !ok {
		return
	}
	delete(b.clients, ch)
	close(ch)
}
