// Package api provides the HTTP server for the stackit-web application.
package api

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/getstackit/stackit/internal/api/auth"
	"github.com/getstackit/stackit/internal/api/handlers"
	"github.com/getstackit/stackit/internal/api/registry"
	"github.com/getstackit/stackit/internal/api/reposync"
	"github.com/getstackit/stackit/internal/api/store"
	githubpkg "github.com/getstackit/stackit/internal/github"
)

// ServerConfig holds configuration for the API server.
type ServerConfig struct {
	// BindAddr is the host/IP to listen on. Empty means "all interfaces".
	// apps/server picks a safe default (127.0.0.1) and switches to the
	// public-facing form when PORT or STACKIT_PUBLIC is set.
	BindAddr    string
	Port        int
	CORSOrigins []string
	APIPrefixes []string
	StaticFS    fs.FS
	Registry    *registry.Registry

	// SSELimits bounds concurrent Server-Sent Events connections. Zero
	// fields take the package defaults, which are generous enough for normal
	// use but cap the blast radius of a public deployment.
	SSELimits handlers.SSELimits

	// RateLimit configures the per-IP request rate limiter applied to the
	// API. Zero fields take the defaults; a negative RequestsPerSecond
	// disables limiting.
	RateLimit RateLimitConfig

	// BranchDiffConcurrency caps how many branch-diff requests run their git
	// subprocesses at once. Zero takes the handler default.
	BranchDiffConcurrency int

	// ReadOnly puts the server into a public read-only posture: the
	// mutating submit route is replaced with a handler that refuses with
	// 405 so writes are impossible by construction, not just by policy.
	// This is the safety floor for exposing a repo publicly — a read-only
	// server cannot push branches or touch GitHub no matter who calls it.
	ReadOnly bool

	// Auth bundles the OAuth handler, session store, and surrounding state
	// needed to gate /api/* routes behind GitHub login. When nil the server
	// runs without authentication; that mode is only safe on a private
	// network (localhost dev, tunneled access). apps/server refuses to
	// start unauthenticated when STACKIT_PUBLIC or $PORT are set unless
	// -auth-disabled is passed explicitly.
	Auth *AuthConfig

	// RepoStore is the persistence backend for repo configuration. It is set
	// only when the server runs in DB-backed mode (-database-url). Runtime
	// repo onboarding persists new repos here so they survive a restart; when
	// nil, onboarding is unavailable and the registry is fixed at boot.
	RepoStore *store.Store

	// ReposRoot is the absolute base directory under which onboarded repos are
	// checked out (<ReposRoot>/<owner>/<name>). Required for runtime
	// onboarding; when empty, onboarding is refused.
	ReposRoot string

	// RepoSyncTokens mints GitHub App installation tokens for git operations on
	// managed repos. It authenticates onboarding clones and background syncs.
	// Nil when no GitHub App is configured, in which case onboarding is refused
	// and the sync loop can only fetch public repos.
	RepoSyncTokens *githubpkg.AppTokenProvider

	// SyncInterval, when > 0, enables the background sync loop: every interval
	// each managed repo is mirror-fetched from its remote so the served state
	// stays current without a local actor. Zero disables it.
	SyncInterval time.Duration
}

// AuthConfig is the runtime auth setup. SessionStore must outlive the
// Server's lifetime (close it after Shutdown returns).
type AuthConfig struct {
	Handler      *auth.Handler
	SessionStore auth.Store

	// Cipher decrypts the GitHub access token stored on a session. Handlers
	// that act as the requesting user (e.g. runtime repo onboarding verifying
	// the user's access to a repo) need it to recover the token from
	// SessionFromContext. It is the same cipher the OAuth handler sealed with.
	Cipher *auth.Cipher
}

// Server is the stackit-web HTTP server. Per-repo state (engine, watcher,
// broadcaster) lives on each registry entry — the server only owns
// transport-level concerns.
type Server struct {
	config     ServerConfig
	httpServer *http.Server
	syncCancel context.CancelFunc
}

// NewServer creates a new API server backed by the given registry.
func NewServer(cfg ServerConfig) *Server {
	return &Server{config: cfg}
}

// Start begins serving HTTP requests. It blocks until the server is stopped.
func (s *Server) Start() error {
	handler, err := s.buildHandler()
	if err != nil {
		return err
	}

	s.httpServer = &http.Server{
		Addr:              fmt.Sprintf("%s:%d", s.config.BindAddr, s.config.Port),
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MiB
	}

	s.startSyncLoop()

	mode := "read-write"
	if s.config.ReadOnly {
		mode = "read-only"
	}
	slog.Info("stackit-web server listening", "url", fmt.Sprintf("http://%s:%d", displayHost(s.config.BindAddr), s.config.Port), "mode", mode)
	return s.httpServer.ListenAndServe()
}

// startSyncLoop launches the background repo sync loop when SyncInterval > 0.
// It runs until Shutdown cancels its context. With no GitHub App configured the
// loop still runs but can only refresh public repos.
func (s *Server) startSyncLoop() {
	if s.config.SyncInterval <= 0 {
		return
	}
	var provider reposync.TokenProvider
	if s.config.RepoSyncTokens != nil {
		provider = s.config.RepoSyncTokens
	} else {
		slog.Warn("repo sync: no GitHub App configured; only public repos will refresh")
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.syncCancel = cancel
	syncer := reposync.New(s.config.Registry, provider, s.config.SyncInterval)
	go syncer.Run(ctx)
	slog.Info("repo sync loop enabled", "interval", s.config.SyncInterval.String())
}

// buildHandler assembles the full HTTP handler chain (routes + middleware)
// without binding a listener, so tests can exercise routing and read-only
// behavior via httptest. Start wraps the result in an *http.Server.
func (s *Server) buildHandler() (http.Handler, error) {
	csp, err := buildCSP(s.config.StaticFS)
	if err != nil {
		return nil, fmt.Errorf("build CSP from static FS: %w", err)
	}

	apiMux := http.NewServeMux()
	prefixes := normalizeAPIPrefixes(s.config.APIPrefixes)

	reg := s.config.Registry

	// A read-only server is meant to be public, so handlers must omit
	// operator-identifying fields (and skip the GitHub calls that would
	// fetch them on the operator's token).
	visibility := handlers.VisibilityPrivate
	if s.config.ReadOnly {
		visibility = handlers.VisibilityPublic
	}

	viewHandler := handlers.NewViewHandler(reg, visibility)
	repoHandler := handlers.NewRepoHandler(reg)
	stacksHandler := handlers.NewStacksHandler(reg)
	branchesHandler := handlers.NewBranchesHandler(reg)
	branchDiffHandler := handlers.NewBranchDiffHandler(reg, s.config.BranchDiffConcurrency)
	eventsHandler := handlers.NewEventsHandler(reg, s.config.SSELimits)
	reposListHandler := handlers.NewReposListHandler(reg)

	// authRequired tells the client whether reads need a login. Read-only
	// mode and auth-disabled both serve reads anonymously.
	authRequired := s.config.Auth != nil && !s.config.ReadOnly
	configHandler := handlers.NewConfigHandler(s.config.ReadOnly, authRequired)

	// The submit route is the server's only mutating endpoint. In
	// read-only mode it is replaced with a handler that refuses every
	// request, so a public deployment cannot push branches or touch
	// GitHub regardless of who calls it.
	var submitHandler http.Handler = handlers.NewSubmitHandler(reg)
	if s.config.ReadOnly {
		submitHandler = readOnlyWriteHandler()
	}

	// Runtime onboarding (POST /repos) acts as the requesting user, so it needs
	// the session cipher and the persistence store. Both may be absent
	// (auth-disabled / no DB); the handler refuses with a clear 503 in that
	// case. Read-only mode replaces it with the write-refusal handler, exactly
	// like submit.
	var cipher *auth.Cipher
	if s.config.Auth != nil {
		cipher = s.config.Auth.Cipher
	}
	var persister handlers.RepoPersister
	if s.config.RepoStore != nil {
		persister = s.config.RepoStore
	}
	var tokens handlers.InstallationTokenProvider
	if s.config.RepoSyncTokens != nil {
		tokens = s.config.RepoSyncTokens
	}
	var onboardHandler http.Handler = handlers.NewOnboardHandler(reg, persister, cipher, s.config.ReposRoot, tokens)
	if s.config.ReadOnly {
		onboardHandler = readOnlyWriteHandler()
	}

	for _, prefix := range prefixes {
		// Unscoped index of available repos.
		apiMux.Handle("GET "+prefix+"/repos", reposListHandler)
		apiMux.Handle("POST "+prefix+"/repos", onboardHandler)

		// New multi-repo routes. {repoID} resolves through the registry;
		// unknown IDs return 404 from inside the handler.
		apiMux.Handle("GET "+prefix+"/repos/{repoID}/view", viewHandler)
		apiMux.Handle("GET "+prefix+"/repos/{repoID}/repo", repoHandler)
		apiMux.Handle("GET "+prefix+"/repos/{repoID}/stacks", stacksHandler)
		apiMux.Handle("GET "+prefix+"/repos/{repoID}/stacks/{name...}", stacksHandler)
		apiMux.Handle("POST "+prefix+"/repos/{repoID}/stacks/{rootBranch}/submit", submitHandler)
		apiMux.Handle("GET "+prefix+"/repos/{repoID}/branches", branchesHandler)
		apiMux.Handle("GET "+prefix+"/repos/{repoID}/branches/{name...}", branchesHandler)
		apiMux.Handle("GET "+prefix+"/repos/{repoID}/branch-diff", branchDiffHandler)
		apiMux.Handle("GET "+prefix+"/repos/{repoID}/events", eventsHandler)

		// Legacy unscoped routes. With no {repoID} path value, handlers
		// fall back to "default" (see defaultRepoID in handlers/common.go).
		// These keep the existing web client working while the multi-repo
		// migration is in progress and are removed once the frontend uses
		// the /repos/{repoID}/ shape.
		apiMux.Handle("GET "+prefix+"/view", viewHandler)
		apiMux.Handle("GET "+prefix+"/repo", repoHandler)
		apiMux.Handle("GET "+prefix+"/stacks", stacksHandler)
		apiMux.Handle("GET "+prefix+"/stacks/{name...}", stacksHandler)
		apiMux.Handle("POST "+prefix+"/stacks/{rootBranch}/submit", submitHandler)
		apiMux.Handle("GET "+prefix+"/branches", branchesHandler)
		apiMux.Handle("GET "+prefix+"/branches/{name...}", branchesHandler)
		apiMux.Handle("GET "+prefix+"/branch-diff", branchDiffHandler)
		apiMux.Handle("GET "+prefix+"/events", eventsHandler)
	}

	// Apply session enforcement to /api/* only. /auth/* and static assets
	// stay unauthenticated so the user can complete the login flow and
	// land on the SPA shell.
	//
	// Read-only mode skips session enforcement entirely: the API is all
	// reads (the lone write route is the read-only refusal handler), so the
	// repo is meant to be publicly viewable without a login. Without this,
	// a read-only server with Auth configured would still 401 anonymous
	// readers and defeat the purpose of the mode.
	var sessionGated http.Handler = apiMux
	if s.config.Auth != nil && !s.config.ReadOnly {
		sessionGated = auth.RequireSession(s.config.Auth.SessionStore, apiMux)
	}
	// CSRF header check wraps the session-gated API so it covers POST
	// /submit. Safe methods (GET/HEAD/OPTIONS) pass through untouched, so the
	// read API is unaffected.
	sessionGated = auth.RequireCSRFHeader(sessionGated)

	// /config advertises server capabilities and must be reachable without a
	// session so the client can learn whether a login is required before
	// attempting one. It bypasses the session gate but still gets the outer
	// middleware (and rate limiting below).
	publicAPIMux := http.NewServeMux()
	for _, prefix := range prefixes {
		publicAPIMux.Handle("GET "+prefix+"/config", configHandler)
	}

	// Combined API dispatch: public capability routes bypass the session
	// gate; everything else is session-gated. Rate limiting wraps both so the
	// public endpoint is protected from floods too.
	var protectedAPI http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isConfigPath(r.URL.Path, prefixes) {
			publicAPIMux.ServeHTTP(w, r)
			return
		}
		sessionGated.ServeHTTP(w, r)
	})

	// Per-IP rate limiting guards the public API surface. A negative RPS
	// disables it; otherwise it wraps the API so a single client can't flood
	// the server with reads.
	if s.config.RateLimit.RequestsPerSecond >= 0 {
		protectedAPI = rateLimitMiddleware(newRateLimiter(s.config.RateLimit), protectedAPI)
	}

	authMux := http.NewServeMux()
	if s.config.Auth != nil {
		h := s.config.Auth.Handler
		authMux.HandleFunc("GET /auth/login", h.LoginHandler)
		authMux.HandleFunc("GET /auth/callback", h.CallbackHandler)
		authMux.HandleFunc("POST /auth/logout", h.LogoutHandler)
		authMux.HandleFunc("GET /auth/me", h.MeHandler)
	}
	// /auth/logout is a POST and needs the same CSRF gate as /api/*.
	// Login and callback are GETs and pass through. The CSRF middleware
	// short-circuits safe methods so wrapping the whole mux is fine.
	authHandler := auth.RequireCSRFHeader(http.Handler(authMux))

	webHandler := newStaticHandler(s.config.StaticFS)
	root := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isAPIPath(r.URL.Path, prefixes) {
			protectedAPI.ServeHTTP(w, r)
			return
		}

		if s.config.Auth != nil && strings.HasPrefix(r.URL.Path, "/auth/") {
			authHandler.ServeHTTP(w, r)
			return
		}

		if webHandler != nil {
			webHandler.ServeHTTP(w, r)
			return
		}

		http.NotFound(w, r)
	})

	// Middleware order (outermost first). recover wraps everything so a
	// panic in any inner layer can't kill the process; logging is innermost
	// so it sees the final status written by handlers (and by any earlier
	// middleware that short-circuits).
	handler := loggingMiddleware(root)
	handler = maxBodyMiddleware(handler)
	handler = corsMiddleware(s.config.CORSOrigins, handler)
	handler = securityHeadersMiddleware(csp, handler)
	handler = requestIDMiddleware(handler)
	handler = recoverMiddleware(handler)

	return handler, nil
}

// Shutdown gracefully stops the server. The registry's watchers and
// broadcasters are torn down separately by main.go's defer reg.Close().
func (s *Server) Shutdown(ctx context.Context) error {
	if s.syncCancel != nil {
		s.syncCancel()
	}
	if s.httpServer != nil {
		return s.httpServer.Shutdown(ctx)
	}
	return nil
}

func normalizeAPIPrefixes(prefixes []string) []string {
	if len(prefixes) == 0 {
		return []string{"/api/v1", "/api"}
	}

	seen := make(map[string]struct{}, len(prefixes))
	normalized := make([]string, 0, len(prefixes))
	for _, prefix := range prefixes {
		prefix = strings.TrimSpace(prefix)
		if prefix == "" {
			continue
		}

		if !strings.HasPrefix(prefix, "/") {
			prefix = "/" + prefix
		}
		if len(prefix) > 1 {
			prefix = strings.TrimRight(prefix, "/")
		}

		if _, ok := seen[prefix]; ok {
			continue
		}
		seen[prefix] = struct{}{}
		normalized = append(normalized, prefix)
	}

	if len(normalized) == 0 {
		return []string{"/api/v1", "/api"}
	}
	return normalized
}

// displayHost returns a user-friendly host for the startup log line. Empty
// bind address means "listen on all interfaces", which we surface as
// "localhost" because that's what the operator will type in a browser to
// reach the server locally.
func displayHost(bind string) string {
	if bind == "" || bind == "0.0.0.0" || bind == "::" {
		return "localhost"
	}
	return bind
}

func isAPIPath(path string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

// isConfigPath reports whether path is the unauthenticated capabilities
// endpoint for any configured prefix (e.g. /api/v1/config or /api/config).
func isConfigPath(path string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if path == prefix+"/config" {
			return true
		}
	}
	return false
}
