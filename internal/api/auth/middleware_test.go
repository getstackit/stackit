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

func TestRequireCSRFHeader_AllowsSafeMethodsWithoutHeader(t *testing.T) {
	t.Parallel()

	called := 0
	h := RequireCSRFHeader(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called++
		w.WriteHeader(http.StatusOK)
	}))

	for _, m := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		req := httptest.NewRequest(m, "/api/v1/repos", nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		require.Equalf(t, http.StatusOK, rr.Code, "method %s should pass without header", m)
	}
	require.Equal(t, 3, called)
}

func TestRequireCSRFHeader_BlocksMutatingMethodsWithoutHeader(t *testing.T) {
	t.Parallel()

	h := RequireCSRFHeader(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("downstream handler should not run")
	}))

	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		req := httptest.NewRequest(m, "/api/v1/repos/default/stacks/main/submit", nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		require.Equalf(t, http.StatusForbidden, rr.Code, "method %s without header should be 403", m)
		require.Contains(t, rr.Body.String(), "missing csrf header")
	}
}

func TestRequireCSRFHeader_AllowsMutatingMethodsWithHeader(t *testing.T) {
	t.Parallel()

	called := false
	h := RequireCSRFHeader(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/repos/default/stacks/main/submit", nil)
	req.Header.Set(CSRFHeader, "1")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	require.True(t, called)
	require.Equal(t, http.StatusOK, rr.Code)
}

func TestRequireCSRFHeader_AnyNonEmptyValueAccepted(t *testing.T) {
	t.Parallel()

	// The presence of the header is what matters, not its content.
	// Browsers can't set a custom header on a cross-origin request without
	// preflight, so the value doesn't need to be unguessable.
	for _, v := range []string{"1", "true", "yes-please", "x"} {
		called := false
		h := RequireCSRFHeader(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
			called = true
		}))

		req := httptest.NewRequest(http.MethodPost, "/", nil)
		req.Header.Set(CSRFHeader, v)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)

		require.Truef(t, called, "value %q should be accepted", v)
	}
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
