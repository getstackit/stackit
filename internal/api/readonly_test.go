package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/api/auth"
	"github.com/getstackit/stackit/internal/api/registry"
	httpcontract "github.com/getstackit/stackit/internal/contracts/http"
)

// newTestHandler builds the full handler chain for a server with the given
// read-only setting and an empty registry. Auth is left nil so the tests
// exercise routing without a session store.
func newTestHandler(t *testing.T, readOnly bool) http.Handler {
	t.Helper()
	srv := NewServer(ServerConfig{
		APIPrefixes: []string{"/api/v1"},
		Registry:    registry.New(),
		ReadOnly:    readOnly,
	})
	handler, err := srv.buildHandler()
	require.NoError(t, err)
	return handler
}

// newTestHandlerWithAuth is newTestHandler with an AuthConfig attached so
// session enforcement is in play.
func newTestHandlerWithAuth(t *testing.T, readOnly bool, authCfg *AuthConfig) http.Handler {
	t.Helper()
	srv := NewServer(ServerConfig{
		APIPrefixes: []string{"/api/v1"},
		Registry:    registry.New(),
		ReadOnly:    readOnly,
		Auth:        authCfg,
	})
	handler, err := srv.buildHandler()
	require.NoError(t, err)
	return handler
}

// newTestAuthConfig builds an AuthConfig backed by an in-memory session
// store. No session is ever created, so requests are effectively anonymous —
// which is exactly what we want to assert read-only mode lets through.
func newTestAuthConfig(t *testing.T) *AuthConfig {
	t.Helper()
	key, err := auth.GenerateSessionKey()
	require.NoError(t, err)
	cipher, err := auth.NewCipher(key)
	require.NoError(t, err)
	store := auth.NewMemoryStore(cipher)
	t.Cleanup(func() { _ = store.Close() })
	handler, err := auth.NewHandler(auth.Config{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		BaseURL:      "http://localhost",
	}, store, nil, cipher)
	require.NoError(t, err)
	return &AuthConfig{Handler: handler, SessionStore: store}
}

// submitRequest builds a POST to the submit route with the CSRF header set,
// so it clears RequireCSRFHeader and reaches the routed handler.
func submitRequest() *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/repos/default/stacks/main/submit", nil)
	req.Header.Set(auth.CSRFHeader, "1")
	return req
}

func TestReadOnlyModeRefusesSubmit(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t, true)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, submitRequest())

	require.Equal(t, http.StatusMethodNotAllowed, rr.Code)
	require.JSONEq(t, `{"error":"server is read-only"}`, rr.Body.String())
}

func TestReadOnlyModeAllowsReads(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t, true)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/repos", nil))

	// The read API is untouched by read-only mode: the repos index still
	// answers 200 (with an empty list for an empty registry).
	require.Equal(t, http.StatusOK, rr.Code)
}

func TestReadWriteModeKeepsSubmitRoute(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t, false)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, submitRequest())

	// In normal mode the submit route is live and reaches the real handler.
	// It won't 405 with the read-only body; against an empty registry it
	// resolves to an unknown repo instead.
	require.NotEqual(t, http.StatusMethodNotAllowed, rr.Code)
	require.NotContains(t, rr.Body.String(), "server is read-only")
}

func TestReadOnlyModeAllowsAnonymousReadsWithAuthConfigured(t *testing.T) {
	t.Parallel()

	handler := newTestHandlerWithAuth(t, true, newTestAuthConfig(t))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/repos", nil))

	// Read-only mode skips session enforcement, so an anonymous reader
	// gets the repos index even though auth is configured.
	require.Equal(t, http.StatusOK, rr.Code)
}

func TestReadWriteModeRequiresSessionForReads(t *testing.T) {
	t.Parallel()

	handler := newTestHandlerWithAuth(t, false, newTestAuthConfig(t))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/repos", nil))

	// With auth configured and read-only off, anonymous reads are rejected.
	require.Equal(t, http.StatusUnauthorized, rr.Code)
}

func fetchConfig(t *testing.T, handler http.Handler) httpcontract.ConfigResponse {
	t.Helper()
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/config", nil))
	require.Equal(t, http.StatusOK, rr.Code)
	var cfg httpcontract.ConfigResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &cfg))
	return cfg
}

func TestConfigEndpointReadOnly(t *testing.T) {
	t.Parallel()

	cfg := fetchConfig(t, newTestHandler(t, true))
	require.True(t, cfg.ReadOnly)
	require.False(t, cfg.AuthRequired, "read-only servers serve reads anonymously")
}

func TestConfigEndpointReachableWithoutSession(t *testing.T) {
	t.Parallel()

	// Auth configured, read-only off: reads require a session, but /config
	// must still be reachable anonymously so the client can discover that a
	// login is required.
	cfg := fetchConfig(t, newTestHandlerWithAuth(t, false, newTestAuthConfig(t)))
	require.False(t, cfg.ReadOnly)
	require.True(t, cfg.AuthRequired)
}

func TestConfigEndpointAuthDisabled(t *testing.T) {
	t.Parallel()

	// No auth configured: neither read-only nor auth-required.
	cfg := fetchConfig(t, newTestHandler(t, false))
	require.False(t, cfg.ReadOnly)
	require.False(t, cfg.AuthRequired)
}
