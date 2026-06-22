package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/api/auth"
	"github.com/getstackit/stackit/internal/api/registry"
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
