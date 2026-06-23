package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/api/auth"
)

// syncRouteRequest builds a POST to the manual-sync route with the CSRF header
// set so it clears RequireCSRFHeader and reaches the routed handler.
func syncRouteRequest() *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/repos/default/sync", nil)
	req.Header.Set(auth.CSRFHeader, "1")
	return req
}

func TestReadOnlyModeRefusesManualSync(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t, true)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, syncRouteRequest())

	// Read-only public servers must not expose an anonymous fetch trigger; the
	// route is replaced with the write-refusal handler, like submit/onboard.
	require.Equal(t, http.StatusMethodNotAllowed, rr.Code)
	require.JSONEq(t, `{"error":"server is read-only"}`, rr.Body.String())
}

func TestReadWriteModeKeepsManualSyncRoute(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t, false)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, syncRouteRequest())

	// In normal mode the route is live and reaches the real handler. Against an
	// empty registry it resolves to an unknown repo (404), not the read-only
	// refusal.
	require.Equal(t, http.StatusNotFound, rr.Code)
	require.NotContains(t, rr.Body.String(), "server is read-only")
}
