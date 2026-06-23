package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/api/githubwebhook"
)

// fakeSyncer records Trigger calls and signals each one on done, so a test can
// wait for the (otherwise fire-and-forget) hand-off.
type fakeSyncer struct {
	mu    sync.Mutex
	calls []string
	done  chan struct{}
}

func newFakeSyncer() *fakeSyncer {
	return &fakeSyncer{done: make(chan struct{}, 1)}
}

func (f *fakeSyncer) Trigger(owner, name string) {
	f.mu.Lock()
	f.calls = append(f.calls, owner+"/"+name)
	f.mu.Unlock()
	select {
	case f.done <- struct{}{}:
	default:
	}
}

func (f *fakeSyncer) called() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

const testSecret = "webhook-secret"

func pushBody() []byte {
	return []byte(`{"repository":{"name":"widget","full_name":"octo/widget","owner":{"login":"octo"}}}`)
}

// webhookRequest builds a POST signed with testSecret for the given event
// type and body.
func webhookRequest(event string, body []byte) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", strings.NewReader(string(body)))
	req.Header.Set(githubwebhook.EventHeader, event)
	req.Header.Set(githubwebhook.SignatureHeader, sign(testSecret, body))
	return req
}

func TestWebhookHandler_DisabledWithoutSecret(t *testing.T) {
	t.Parallel()
	h := NewWebhookHandler("", newFakeSyncer())
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, webhookRequest("push", pushBody()))
	require.Equal(t, http.StatusNotFound, rr.Code, "an unconfigured receiver must look like it doesn't exist")
}

func TestWebhookHandler_RejectsBadSignature(t *testing.T) {
	t.Parallel()
	sy := newFakeSyncer()
	h := NewWebhookHandler(testSecret, sy)

	body := pushBody()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", strings.NewReader(string(body)))
	req.Header.Set(githubwebhook.EventHeader, "push")
	req.Header.Set(githubwebhook.SignatureHeader, sign("wrong-secret", body))

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	require.Equal(t, http.StatusUnauthorized, rr.Code)
	require.Empty(t, sy.called(), "an unverified delivery must not trigger a sync")
}

func TestWebhookHandler_PushTriggersSync(t *testing.T) {
	t.Parallel()
	sy := newFakeSyncer()
	h := NewWebhookHandler(testSecret, sy)

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, webhookRequest("push", pushBody()))
	require.Equal(t, http.StatusAccepted, rr.Code)

	// Wait for the trigger hand-off (the real coalescer runs the fetch async).
	select {
	case <-sy.done:
	case <-time.After(2 * time.Second):
		t.Fatal("Trigger was not called for a verified push")
	}
	require.Equal(t, []string{"octo/widget"}, sy.called())
}

func TestWebhookHandler_PingAcknowledgedWithoutSync(t *testing.T) {
	t.Parallel()
	sy := newFakeSyncer()
	h := NewWebhookHandler(testSecret, sy)

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, webhookRequest("ping", []byte(`{"zen":"hi"}`)))
	require.Equal(t, http.StatusNoContent, rr.Code)
	require.Empty(t, sy.called(), "ping must not trigger a sync")
}

func TestWebhookHandler_UnparseablePushAcknowledged(t *testing.T) {
	t.Parallel()
	sy := newFakeSyncer()
	h := NewWebhookHandler(testSecret, sy)

	body := []byte(`{"zen":"no repository here"}`)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, webhookRequest("push", body))
	// Verified but unactionable: ack so GitHub stops retrying, no sync.
	require.Equal(t, http.StatusNoContent, rr.Code)
	require.Empty(t, sy.called())
}
