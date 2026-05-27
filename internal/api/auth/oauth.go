package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2"
	githuboauth "golang.org/x/oauth2/github"
)

// DefaultScopes is what the OAuth flow requests today. `read:user` gets the
// identity needed for the allowlist check; `repo` captures push scope up
// front so when per-user push lands later the user doesn't re-consent.
var DefaultScopes = []string{"read:user", "repo"}

// Config is the operator-supplied OAuth configuration. ClientID/Secret are
// the values from the GitHub OAuth App. BaseURL is the canonical https:// URL
// the server is reachable at (used to build the callback URL because the
// server cannot infer its public hostname).
type Config struct {
	ClientID     string
	ClientSecret string //nolint:gosec // field name reflects the OAuth spec; not a literal secret
	BaseURL      string
	Scopes       []string
	// AfterCallbackPath is where the user lands after a successful login.
	// Defaults to "/". Settable for tests.
	AfterCallbackPath string
	// DeniedPath is where allowlist denials redirect to. Defaults to
	// "/auth/denied". The static handler serves a small page at this path;
	// if the frontend doesn't render one, the browser sees a generic 404
	// from the SPA which is still a clear signal.
	DeniedPath string
	// Cookies controls Secure-flag behavior.
	Cookies CookieOptions
}

// Handler bundles the OAuth state needed by the login/callback/logout/me
// HTTP handlers. Build one in main.go; mount its methods at the auth
// routes.
type Handler struct {
	cfg       Config
	store     Store
	allow     *Allowlist
	cipher    *Cipher
	oauth2cfg *oauth2.Config

	// httpClient is the http client used for the GitHub user lookup. Split
	// out so tests can swap in a transport pointed at httptest.Server.
	httpClient *http.Client

	// userInfoURL overrides the GitHub /user endpoint. Populated by
	// SetGitHubEndpointForTest; production default empty means
	// https://api.github.com/user.
	userInfoURL string
}

// NewHandler validates the config and returns a ready Handler.
func NewHandler(cfg Config, store Store, allow *Allowlist, cipher *Cipher) (*Handler, error) {
	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		return nil, errors.New("oauth client id and secret are required")
	}
	if cfg.BaseURL == "" {
		return nil, errors.New("oauth base url is required")
	}
	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = DefaultScopes
	}
	if cfg.AfterCallbackPath == "" {
		cfg.AfterCallbackPath = "/"
	}
	if cfg.DeniedPath == "" {
		cfg.DeniedPath = "/auth/denied"
	}

	callbackURL, err := joinURL(cfg.BaseURL, "/auth/callback")
	if err != nil {
		return nil, err
	}

	o := &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Scopes:       scopes,
		RedirectURL:  callbackURL,
		Endpoint:     githuboauth.Endpoint,
	}

	return &Handler{
		cfg:        cfg,
		store:      store,
		allow:      allow,
		cipher:     cipher,
		oauth2cfg:  o,
		httpClient: http.DefaultClient,
	}, nil
}

// SetGitHubEndpointForTest swaps the OAuth provider endpoint and HTTP
// client. Tests point both at an httptest.Server stubbing GitHub's auth +
// API surface.
func (h *Handler) SetGitHubEndpointForTest(endpoint oauth2.Endpoint, userInfoURL string, client *http.Client) {
	h.oauth2cfg.Endpoint = endpoint
	h.userInfoURL = userInfoURL
	h.httpClient = client
}

// userInfoURL is the GitHub URL we GET after token exchange to learn who
// the user is. Production-default empty -> falls back to api.github.com.
// Tests set it to their httptest URL.
func (h *Handler) resolveUserInfoURL() string {
	if h.userInfoURL != "" {
		return h.userInfoURL
	}
	return "https://api.github.com/user"
}

// LoginHandler kicks off the OAuth flow. It mints a fresh `state` value,
// stores it in a short-lived cookie, optionally remembers the desired
// post-login URL via ?return=, and 302s the user to GitHub.
func (h *Handler) LoginHandler(w http.ResponseWriter, r *http.Request) {
	state, err := randomToken(16)
	if err != nil {
		log.Printf("login: random state: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	h.cfg.Cookies.setState(w, state)

	// Only honor a ?return= when it's a server-relative path. Anything
	// else (especially absolute URLs to other hosts) is an open-redirect
	// risk, so we silently drop it.
	if ret := r.URL.Query().Get("return"); isSafeReturnPath(ret) {
		h.cfg.Cookies.setReturn(w, ret)
	}

	url := h.oauth2cfg.AuthCodeURL(state, oauth2.AccessTypeOnline)
	http.Redirect(w, r, url, http.StatusFound)
}

// CallbackHandler completes the OAuth flow: verifies state, exchanges the
// code for a token, fetches the GitHub user, checks the allowlist, creates
// a session, and redirects the user to the app (or to DeniedPath on a
// non-allowlisted login).
func (h *Handler) CallbackHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	gotState := q.Get("state")
	wantState := readCookie(r, stateCookieName)
	h.cfg.Cookies.clearState(w)
	if wantState == "" || gotState == "" || !constantTimeStringEq(gotState, wantState) {
		log.Printf("auth callback: bad state")
		http.Error(w, "invalid auth state", http.StatusBadRequest)
		return
	}

	code := q.Get("code")
	if code == "" {
		// GitHub redirects here with `error=...` when the user denies.
		log.Printf("auth callback: missing code (oauth_error=%q)", q.Get("error")) //nolint:gosec // logged value is %q-quoted
		http.Redirect(w, r, h.cfg.DeniedPath, http.StatusFound)
		return
	}

	ctx := r.Context()
	tok, err := h.oauth2cfg.Exchange(ctx, code)
	if err != nil {
		log.Printf("auth callback: token exchange: %v", err)
		http.Error(w, "token exchange failed", http.StatusBadGateway)
		return
	}

	user, err := h.fetchGitHubUser(ctx, tok.AccessToken)
	if err != nil {
		log.Printf("auth callback: fetch user: %v", err)
		http.Error(w, "github user lookup failed", http.StatusBadGateway)
		return
	}

	if err := h.allow.Check(ctx, user.Login, tok.AccessToken); err != nil {
		if errors.Is(err, ErrDenied) {
			log.Printf("auth callback: denied login=%q", user.Login) //nolint:gosec // login is %q-quoted
			http.Redirect(w, r, h.cfg.DeniedPath, http.StatusFound)
			return
		}
		log.Printf("auth callback: allowlist check error: %v", err)
		http.Error(w, "allowlist check failed", http.StatusBadGateway)
		return
	}

	sess, err := h.store.Create(user.Login, user.ID, tok.AccessToken)
	if err != nil {
		log.Printf("auth callback: create session: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	h.cfg.Cookies.setSession(w, sess.ID, sess.ExpiresAt)

	target := h.cfg.AfterCallbackPath
	if ret := readCookie(r, returnCookieName); isSafeReturnPath(ret) {
		target = ret
	}
	h.cfg.Cookies.clearReturn(w)

	log.Printf("auth: login login=%q user_id=%d -> %s", user.Login, user.ID, target) //nolint:gosec // logged values are quoted/numeric/safe target
	http.Redirect(w, r, target, http.StatusFound)
}

// LogoutHandler drops the session both server-side (Store.Delete) and
// client-side (clears the cookie). Always returns 204 so the web app can
// fire-and-forget the call.
func (h *Handler) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	if cookie := readCookie(r, sessionCookieName); cookie != "" {
		if sess, ok := h.store.Get(cookie); ok {
			log.Printf("auth: logout login=%q", sess.GitHubLogin) //nolint:gosec // login is %q-quoted
		}
		h.store.Delete(cookie)
	}
	h.cfg.Cookies.clearSession(w)
	w.WriteHeader(http.StatusNoContent)
}

// MeResponse is the body served by GET /auth/me.
type MeResponse struct {
	Login string `json:"login"`
	ID    int64  `json:"id"`
}

// MeHandler returns the logged-in user's identity, or 401 if no session.
// The web app calls this on load to decide between rendering the app and
// redirecting to /auth/login.
func (h *Handler) MeHandler(w http.ResponseWriter, r *http.Request) {
	sess := SessionFromContext(r.Context())
	if sess == nil {
		cookie := readCookie(r, sessionCookieName)
		if cookie == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthenticated"}`))
			return
		}
		s, ok := h.store.Get(cookie)
		if !ok {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthenticated"}`))
			return
		}
		sess = s
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(MeResponse{
		Login: sess.GitHubLogin,
		ID:    sess.GitHubUserID,
	})
}

// gitHubUser is the subset of /user we care about.
type gitHubUser struct {
	Login string `json:"login"`
	ID    int64  `json:"id"`
}

func (h *Handler) fetchGitHubUser(ctx context.Context, accessToken string) (*gitHubUser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.resolveUserInfoURL(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github /user: status %d", resp.StatusCode)
	}
	var u gitHubUser
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return nil, fmt.Errorf("decode github /user: %w", err)
	}
	if u.Login == "" {
		return nil, errors.New("github /user returned empty login")
	}
	return &u, nil
}

func randomToken(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// constantTimeStringEq is hmac.Equal-style comparison for state tokens. We
// don't need full crypto strength but using a constant-time compare keeps
// timing-channel concerns off the table.
func constantTimeStringEq(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := 0; i < len(a); i++ {
		v |= a[i] ^ b[i]
	}
	return v == 0
}

// isSafeReturnPath returns true when target is a path on this server
// (starts with `/`, doesn't start with `//` or `/\`, doesn't contain a
// scheme). Anything else is treated as an open-redirect attempt.
func isSafeReturnPath(target string) bool {
	if target == "" {
		return false
	}
	if !strings.HasPrefix(target, "/") {
		return false
	}
	if strings.HasPrefix(target, "//") || strings.HasPrefix(target, "/\\") {
		return false
	}
	// Reject URLs with a scheme even if path-ish.
	if u, err := url.Parse(target); err == nil && u.Scheme != "" {
		return false
	}
	return true
}

func joinURL(base, suffix string) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("base url: %w", err)
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return "", fmt.Errorf("base url scheme must be http(s), got %q", u.Scheme)
	}
	u.Path = strings.TrimRight(u.Path, "/") + suffix
	return u.String(), nil
}

// LoginExpiryProbe is the time after which a session would be considered
// stale. Exposed for tests / health-check style code.
func LoginExpiryProbe(now time.Time) time.Time {
	return now.Add(SessionTTL)
}
