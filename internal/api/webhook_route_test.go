package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/api/registry"
)

func signBody(secret, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// TestWebhookRouteReachableWithoutSession proves the webhook bypasses the
// session/CSRF gate (GitHub carries neither) yet is authenticated by signature:
// a correctly signed push is accepted even with auth configured.
func TestWebhookRouteReachableWithoutSession(t *testing.T) {
	t.Parallel()
	const secret = "route-secret"
	srv := NewServer(ServerConfig{
		APIPrefixes:         []string{"/api/v1"},
		Registry:            registry.New(),
		Auth:                newTestAuthConfig(t),
		GitHubWebhookSecret: secret,
	})
	handler, err := srv.buildHandler()
	require.NoError(t, err)

	body := `{"repository":{"name":"widget","full_name":"octo/widget","owner":{"login":"octo"}}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", strings.NewReader(body))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-Hub-Signature-256", signBody(secret, body))

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// 202 Accepted: routed past session+CSRF and verified. The repo isn't
	// registered, but that failure happens in the detached background sync, not
	// on the response path.
	require.Equal(t, http.StatusAccepted, rr.Code)
}

// TestWebhookRouteDisabledWithoutSecret proves the endpoint self-disables when
// no secret is configured, the correct posture for a local server.
func TestWebhookRouteDisabledWithoutSecret(t *testing.T) {
	t.Parallel()
	srv := NewServer(ServerConfig{
		APIPrefixes: []string{"/api/v1"},
		Registry:    registry.New(),
	})
	handler, err := srv.buildHandler()
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", strings.NewReader("{}"))
	req.Header.Set("X-GitHub-Event", "push")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusNotFound, rr.Code)
}
