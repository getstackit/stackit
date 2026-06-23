package handlers

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/getstackit/stackit/internal/api/githubwebhook"
)

// maxWebhookBody caps the request body the webhook receiver will read. GitHub
// push payloads are well under this; the limit stops a forged or malformed
// request from forcing an unbounded read before the signature is even checked.
const maxWebhookBody = 1 << 20 // 1 MiB

// webhookSyncTimeout bounds the background fetch+refresh kicked off by a
// delivery, so a stuck remote can't leak goroutines.
const webhookSyncTimeout = 2 * time.Minute

// RepoSyncer mirror-fetches and refreshes a single managed repo by its GitHub
// coordinates. The concrete implementation is *reposync.Syncer; the handler
// takes the narrow interface so handlers stays decoupled from reposync.
type RepoSyncer interface {
	SyncRepo(ctx context.Context, owner, name string) error
}

// WebhookHandler receives GitHub webhook deliveries at
// POST /api/v1/webhooks/github and turns a push into a refresh of the matching
// managed checkout. It is the low-latency complement to the interval sync loop;
// the loop remains as a backstop for missed deliveries and for metadata-ref
// pushes, which GitHub does not deliver push events for.
//
// The route bypasses the session/CSRF gate (GitHub can't carry either) and is
// authenticated solely by the HMAC signature, so it must fail closed when no
// secret is configured — otherwise it would be an open refresh trigger.
type WebhookHandler struct {
	secret string
	syncer RepoSyncer
}

// NewWebhookHandler wires a webhook receiver. An empty secret or nil syncer
// leaves the endpoint disabled (it 404s), so it is safe to mount
// unconditionally and gate purely on configuration.
func NewWebhookHandler(secret string, syncer RepoSyncer) *WebhookHandler {
	return &WebhookHandler{secret: secret, syncer: syncer}
}

func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Disabled when unconfigured: 404 so the endpoint is indistinguishable from
	// a server that never had it, rather than advertising a misconfigured hook.
	if h.secret == "" || h.syncer == nil {
		http.NotFound(w, r)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBody))
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}

	if !githubwebhook.Verify(h.secret, body, r.Header.Get(githubwebhook.SignatureHeader)) {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	switch r.Header.Get(githubwebhook.EventHeader) {
	case "push":
		h.handlePush(w, body)
	case "ping":
		// GitHub sends ping once when the hook is created; acknowledge it.
		w.WriteHeader(http.StatusNoContent)
	default:
		// Event types we don't act on (the App should only subscribe to push,
		// but be tolerant). Acknowledge so GitHub doesn't retry.
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h *WebhookHandler) handlePush(w http.ResponseWriter, body []byte) {
	owner, name, ok := githubwebhook.ParsePush(body)
	if !ok {
		// Verified but not a recognizable push payload: acknowledge so GitHub
		// doesn't retry a delivery we can't act on.
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// A mirror-fetch is slow and GitHub expects a prompt response, so ack
	// immediately and run the sync in the background, detached from the request.
	// The inbound rate limiter bounds how often this fans out; a follow-up adds
	// per-repo coalescing so a burst of pushes collapses to one fetch.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), webhookSyncTimeout)
		defer cancel()
		if err := h.syncer.SyncRepo(ctx, owner, name); err != nil {
			slog.Warn("webhook: sync failed", "owner", owner, "name", name, "error", err)
		}
	}()
	w.WriteHeader(http.StatusAccepted)
}
