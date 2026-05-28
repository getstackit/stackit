package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

func TestIsSafeReturnPath(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"/", true},
		{"/stacks/main", true},
		{"//evil.com", false},
		{`/\evil.com`, false},
		{"http://evil.com", false},
		{"javascript:alert(1)", false},
		{"relative/path", false},
	}
	for _, tc := range cases {
		require.Equalf(t, tc.want, isSafeReturnPath(tc.in), "input=%q", tc.in)
	}
}

func TestConstantTimeStringEq(t *testing.T) {
	t.Parallel()

	require.True(t, constantTimeStringEq("abc", "abc"))
	require.False(t, constantTimeStringEq("abc", "abd"))
	require.False(t, constantTimeStringEq("", "x"))
	require.False(t, constantTimeStringEq("abc", "ab"))
	require.True(t, constantTimeStringEq("", ""))
}

func TestJoinURL(t *testing.T) {
	t.Parallel()

	cases := []struct {
		base, suffix, want string
		wantErr            bool
	}{
		{"https://stackit.example.com", "/auth/callback", "https://stackit.example.com/auth/callback", false},
		{"https://stackit.example.com/", "/auth/callback", "https://stackit.example.com/auth/callback", false},
		{"http://localhost:8080", "/auth/callback", "http://localhost:8080/auth/callback", false},
		{"ftp://nope.com", "/auth/callback", "", true},
		{":://broken", "/auth/callback", "", true},
	}
	for _, tc := range cases {
		got, err := joinURL(tc.base, tc.suffix)
		if tc.wantErr {
			require.Errorf(t, err, "base=%q", tc.base)
			continue
		}
		require.NoErrorf(t, err, "base=%q", tc.base)
		require.Equal(t, tc.want, got)
	}
}

// stubGitHub stands in for the GitHub OAuth + /user endpoints. The Handler
// is pointed at it via SetGitHubEndpointForTest.
type stubGitHub struct {
	server      *httptest.Server
	authCalls   int
	tokenCalls  int
	userCalls   int
	denyToken   bool
	userLogin   string
	userID      int64
	emitState   string // recorded state from the most recent auth request
	mintedCode  string
	mintedToken string
}

func newStubGitHub(t *testing.T) *stubGitHub {
	t.Helper()
	s := &stubGitHub{
		userLogin:   "jonnii",
		userID:      42,
		mintedCode:  "test-code",
		mintedToken: "test-access-token",
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/login/oauth/authorize", func(w http.ResponseWriter, r *http.Request) {
		s.authCalls++
		s.emitState = r.URL.Query().Get("state")
		// Production GitHub 302s back to the redirect URL with code+state.
		// In tests we don't actually want the redirect — the Handler is
		// driven directly by the test. So we just record state and 200.
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/login/oauth/access_token", func(w http.ResponseWriter, r *http.Request) {
		s.tokenCalls++
		if s.denyToken {
			http.Error(w, "denied", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": s.mintedToken,
			"token_type":   "bearer",
			"scope":        "read:user repo",
		})
	})
	mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
		s.userCalls++
		if got := r.Header.Get("Authorization"); got != "Bearer "+s.mintedToken {
			http.Error(w, "bad auth", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"login": s.userLogin,
			"id":    s.userID,
		})
	})
	s.server = httptest.NewServer(mux)
	t.Cleanup(s.server.Close)
	return s
}

func (s *stubGitHub) oauth2Endpoint() oauth2.Endpoint {
	return oauth2.Endpoint{
		AuthURL:  s.server.URL + "/login/oauth/authorize",
		TokenURL: s.server.URL + "/login/oauth/access_token",
	}
}

func (s *stubGitHub) userURL() string {
	return s.server.URL + "/user"
}

func buildTestHandler(t *testing.T, allow *Allowlist, stub *stubGitHub) (*Handler, *MemoryStore) {
	t.Helper()
	cipher := newTestCipher(t)
	store := NewMemoryStore(cipher)
	t.Cleanup(func() { _ = store.Close() })

	h, err := NewHandler(Config{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		BaseURL:      "http://localhost:8080",
	}, store, allow, cipher)
	require.NoError(t, err)
	h.SetGitHubEndpointForTest(stub.oauth2Endpoint(), stub.userURL(), stub.server.Client())
	return h, store
}

func TestLoginHandler_SetsStateCookieAndRedirects(t *testing.T) {
	t.Parallel()

	allow, _ := NewAllowlist(AllowlistConfig{Users: []string{"jonnii"}})
	allow.SetOrgChecker(&fakeOrgChecker{})
	stub := newStubGitHub(t)
	h, _ := buildTestHandler(t, allow, stub)

	req := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
	rr := httptest.NewRecorder()
	h.LoginHandler(rr, req)

	require.Equal(t, http.StatusFound, rr.Code)
	loc := rr.Header().Get("Location")
	require.Contains(t, loc, "/login/oauth/authorize")
	require.Contains(t, loc, "client_id=test-client")
	require.Contains(t, loc, "state=")

	// State cookie must be HttpOnly and scoped to /auth/.
	cookies := rr.Result().Cookies()
	var state *http.Cookie
	for _, c := range cookies {
		if c.Name == stateCookieName {
			state = c
		}
	}
	require.NotNil(t, state)
	require.True(t, state.HttpOnly)
	require.Equal(t, "/auth/", state.Path)
	require.NotEmpty(t, state.Value)
}

func TestCallbackHandler_HappyPath(t *testing.T) {
	t.Parallel()

	allow, _ := NewAllowlist(AllowlistConfig{Users: []string{"jonnii"}})
	allow.SetOrgChecker(&fakeOrgChecker{})
	stub := newStubGitHub(t)
	h, store := buildTestHandler(t, allow, stub)

	// Simulate a callback with matching state cookie + state query.
	state := "abc123"
	req := httptest.NewRequest(http.MethodGet, "/auth/callback?code=test-code&state="+state, nil)
	req.AddCookie(&http.Cookie{Name: stateCookieName, Value: state})
	rr := httptest.NewRecorder()

	h.CallbackHandler(rr, req)

	require.Equal(t, http.StatusFound, rr.Code)
	require.Equal(t, "/", rr.Header().Get("Location"))

	// Session cookie set.
	var sessionCookie *http.Cookie
	for _, c := range rr.Result().Cookies() {
		if c.Name == sessionCookieName {
			sessionCookie = c
		}
	}
	require.NotNil(t, sessionCookie)
	require.True(t, sessionCookie.HttpOnly)
	require.NotEmpty(t, sessionCookie.Value)

	// Store now has the session under that cookie value.
	sess, ok := store.Get(sessionCookie.Value)
	require.True(t, ok)
	require.Equal(t, "jonnii", sess.GitHubLogin)
	require.Equal(t, int64(42), sess.GitHubUserID)

	tok, err := sess.AccessToken(store.cipher)
	require.NoError(t, err)
	require.Equal(t, "test-access-token", tok)
}

func TestCallbackHandler_StateMismatchRejected(t *testing.T) {
	t.Parallel()

	allow, _ := NewAllowlist(AllowlistConfig{Users: []string{"jonnii"}})
	allow.SetOrgChecker(&fakeOrgChecker{})
	stub := newStubGitHub(t)
	h, _ := buildTestHandler(t, allow, stub)

	req := httptest.NewRequest(http.MethodGet, "/auth/callback?code=test-code&state=mismatched", nil)
	req.AddCookie(&http.Cookie{Name: stateCookieName, Value: "different"})
	rr := httptest.NewRecorder()

	h.CallbackHandler(rr, req)

	require.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestCallbackHandler_MissingStateCookieRejected(t *testing.T) {
	t.Parallel()

	allow, _ := NewAllowlist(AllowlistConfig{Users: []string{"jonnii"}})
	allow.SetOrgChecker(&fakeOrgChecker{})
	stub := newStubGitHub(t)
	h, _ := buildTestHandler(t, allow, stub)

	req := httptest.NewRequest(http.MethodGet, "/auth/callback?code=test-code&state=anything", nil)
	rr := httptest.NewRecorder()

	h.CallbackHandler(rr, req)
	require.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestCallbackHandler_AllowlistDeniedRedirectsToDeniedPath(t *testing.T) {
	t.Parallel()

	allow, _ := NewAllowlist(AllowlistConfig{Users: []string{"someone-else"}})
	allow.SetOrgChecker(&fakeOrgChecker{})
	stub := newStubGitHub(t)
	h, store := buildTestHandler(t, allow, stub)

	state := "s"
	req := httptest.NewRequest(http.MethodGet, "/auth/callback?code=test-code&state="+state, nil)
	req.AddCookie(&http.Cookie{Name: stateCookieName, Value: state})
	rr := httptest.NewRecorder()

	h.CallbackHandler(rr, req)

	require.Equal(t, http.StatusFound, rr.Code)
	require.Equal(t, "/auth/denied", rr.Header().Get("Location"))

	// No session cookie issued.
	for _, c := range rr.Result().Cookies() {
		require.NotEqual(t, sessionCookieName, c.Name, "no session for denied user")
	}
	// And no session in the store.
	store.mu.RLock()
	require.Equal(t, 0, len(store.sessions))
	store.mu.RUnlock()
}

func TestCallbackHandler_HonorsSafeReturnPath(t *testing.T) {
	t.Parallel()

	allow, _ := NewAllowlist(AllowlistConfig{Users: []string{"jonnii"}})
	allow.SetOrgChecker(&fakeOrgChecker{})
	stub := newStubGitHub(t)
	h, _ := buildTestHandler(t, allow, stub)

	state := "s"
	req := httptest.NewRequest(http.MethodGet, "/auth/callback?code=test-code&state="+state, nil)
	req.AddCookie(&http.Cookie{Name: stateCookieName, Value: state})
	req.AddCookie(&http.Cookie{Name: returnCookieName, Value: "/stacks/foo"})
	rr := httptest.NewRecorder()

	h.CallbackHandler(rr, req)
	require.Equal(t, "/stacks/foo", rr.Header().Get("Location"))
}

func TestCallbackHandler_RejectsUnsafeReturnPath(t *testing.T) {
	t.Parallel()

	allow, _ := NewAllowlist(AllowlistConfig{Users: []string{"jonnii"}})
	allow.SetOrgChecker(&fakeOrgChecker{})
	stub := newStubGitHub(t)
	h, _ := buildTestHandler(t, allow, stub)

	state := "s"
	req := httptest.NewRequest(http.MethodGet, "/auth/callback?code=test-code&state="+state, nil)
	req.AddCookie(&http.Cookie{Name: stateCookieName, Value: state})
	req.AddCookie(&http.Cookie{Name: returnCookieName, Value: "//evil.example.com/steal"})
	rr := httptest.NewRecorder()

	h.CallbackHandler(rr, req)
	require.Equal(t, "/", rr.Header().Get("Location"), "open-redirect attempt must be ignored")
}

func TestLogoutHandler_ClearsCookieAndStore(t *testing.T) {
	t.Parallel()

	allow, _ := NewAllowlist(AllowlistConfig{Users: []string{"jonnii"}})
	allow.SetOrgChecker(&fakeOrgChecker{})
	stub := newStubGitHub(t)
	h, store := buildTestHandler(t, allow, stub)

	sess, _ := store.Create("jonnii", 1, "tok")

	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sess.ID})
	rr := httptest.NewRecorder()
	h.LogoutHandler(rr, req)

	require.Equal(t, http.StatusNoContent, rr.Code)
	_, ok := store.Get(sess.ID)
	require.False(t, ok, "session must be deleted server-side")

	var cleared *http.Cookie
	for _, c := range rr.Result().Cookies() {
		if c.Name == sessionCookieName {
			cleared = c
		}
	}
	require.NotNil(t, cleared)
	require.Equal(t, -1, cleared.MaxAge)
}

func TestMeHandler_401WithoutSession(t *testing.T) {
	t.Parallel()

	allow, _ := NewAllowlist(AllowlistConfig{Users: []string{"jonnii"}})
	allow.SetOrgChecker(&fakeOrgChecker{})
	stub := newStubGitHub(t)
	h, _ := buildTestHandler(t, allow, stub)

	req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	rr := httptest.NewRecorder()
	h.MeHandler(rr, req)

	require.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestMeHandler_ReturnsCurrentUser(t *testing.T) {
	t.Parallel()

	allow, _ := NewAllowlist(AllowlistConfig{Users: []string{"jonnii"}})
	allow.SetOrgChecker(&fakeOrgChecker{})
	stub := newStubGitHub(t)
	h, store := buildTestHandler(t, allow, stub)

	sess, _ := store.Create("jonnii", 42, "tok")

	req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sess.ID})
	rr := httptest.NewRecorder()
	h.MeHandler(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	var resp MeResponse
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	require.Equal(t, "jonnii", resp.Login)
	require.Equal(t, int64(42), resp.ID)
}

// sanity check that GitHub-style query encoding in the auth URL survives a
// round trip — guards against the test stub diverging from what
// golang.org/x/oauth2 actually emits.
func TestLoginHandler_AuthURLEncodesScopes(t *testing.T) {
	t.Parallel()

	allow, _ := NewAllowlist(AllowlistConfig{Users: []string{"jonnii"}})
	allow.SetOrgChecker(&fakeOrgChecker{})
	stub := newStubGitHub(t)
	h, _ := buildTestHandler(t, allow, stub)

	req := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
	rr := httptest.NewRecorder()
	h.LoginHandler(rr, req)

	loc, err := url.Parse(rr.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, "read:user repo", loc.Query().Get("scope"))
	require.True(t, strings.HasSuffix(loc.Path, "/login/oauth/authorize"))
}
