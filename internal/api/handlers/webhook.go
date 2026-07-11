package handlers

import (
	"io"
	"log/slog"
	"net/http"

	"github.com/getstackit/stackit/internal/api/githubwebhook"
	"github.com/getstackit/stackit/internal/api/registry"
)

// maxWebhookBody caps the request body the webhook receiver will read. GitHub
// push payloads are well under this; the limit stops a forged or malformed
// request from forcing an unbounded read before the signature is even checked.
const maxWebhookBody = 1 << 20 // 1 MiB

// RepoSyncTrigger requests an asynchronous, coalesced sync of a managed repo by
// its GitHub coordinates. The concrete implementation is *reposync.Coalescer;
// the handler takes the narrow interface so handlers stays decoupled from
// reposync, and so the receiver never blocks on a fetch.
type RepoSyncTrigger interface {
	Trigger(repo registry.RepoRef)
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
	secret  string
	trigger RepoSyncTrigger
}

// NewWebhookHandler wires a webhook receiver. An empty secret or nil trigger
// leaves the endpoint disabled (it 404s), so it is safe to mount
// unconditionally and gate purely on configuration.
func NewWebhookHandler(secret string, trigger RepoSyncTrigger) *WebhookHandler {
	return &WebhookHandler{secret: secret, trigger: trigger}
}

func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Disabled when unconfigured: 404 so the endpoint is indistinguishable from
	// a server that never had it, rather than advertising a misconfigured hook.
	if h.secret == "" || h.trigger == nil {
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

	// Delivery ID correlates this log trail with GitHub's "Recent Deliveries".
	delivery := r.Header.Get(githubwebhook.DeliveryHeader)

	switch event := r.Header.Get(githubwebhook.EventHeader); event {
	case "push":
		h.handlePush(w, body, delivery)
	case "ping":
		// GitHub sends ping once when the hook is created; acknowledge it.
		slog.Info("webhook: ping acknowledged", "delivery", delivery) //nolint:gosec // values emitted as structured slog fields
		w.WriteHeader(http.StatusNoContent)
	default:
		// Event types we don't act on (the App should only subscribe to push,
		// but be tolerant). Acknowledge so GitHub doesn't retry.
		slog.Debug("webhook: ignoring event", "event", event, "delivery", delivery) //nolint:gosec // values emitted as structured slog fields
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h *WebhookHandler) handlePush(w http.ResponseWriter, body []byte, delivery string) {
	repo, ok := githubwebhook.ParsePush(body)
	if !ok {
		// Verified but not a recognizable push payload: acknowledge so GitHub
		// doesn't retry a delivery we can't act on.
		slog.Warn("webhook: push had no repository in payload, ignoring", "delivery", delivery) //nolint:gosec // values emitted as structured slog fields
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// A mirror-fetch is slow and GitHub expects a prompt response, so hand off
	// to the coalescer (which runs and de-dups the fetch off the request path)
	// and ack immediately. The sync outcome is logged downstream (reposync):
	// "sync: refreshed repo" on success, or a warning if the fetch fails / the
	// repo isn't managed here.
	slog.Info("webhook: push accepted, triggering sync", "repo", repo.String(), "delivery", delivery) //nolint:gosec // values emitted as structured slog fields
	h.trigger.Trigger(repo)
	w.WriteHeader(http.StatusAccepted)
}
