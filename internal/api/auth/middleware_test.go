package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRequireSession_NoCookie401(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore(newTestCipher(t))
	t.Cleanup(func() { _ = store.Close() })

	called := false
	h := RequireSession(store, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		called = true
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/repos", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	require.Equal(t, http.StatusUnauthorized, rr.Code)
	require.False(t, called)
	require.Contains(t, rr.Body.String(), "unauthenticated")
}

func TestRequireSession_UnknownCookie401(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore(newTestCipher(t))
	t.Cleanup(func() { _ = store.Close() })

	h := RequireSession(store, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("downstream handler should not run")
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/repos", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "not-a-real-session"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	require.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestRequireSession_ValidSessionPassesAndAttachesContext(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore(newTestCipher(t))
	t.Cleanup(func() { _ = store.Close() })

	sess, _ := store.Create("jonnii", 1, "tok")

	var seen *Session
	h := RequireSession(store, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = SessionFromContext(r.Context())
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/repos", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sess.ID})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.NotNil(t, seen)
	require.Equal(t, "jonnii", seen.GitHubLogin)
}
