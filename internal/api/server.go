// Package api provides the HTTP server for the stackit-web application.
package api

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/getstackit/stackit/internal/api/auth"
	"github.com/getstackit/stackit/internal/api/handlers"
	"github.com/getstackit/stackit/internal/api/registry"
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

	// Auth bundles the OAuth handler, session store, and surrounding state
	// needed to gate /api/* routes behind GitHub login. When nil the server
	// runs without authentication; that mode is only safe on a private
	// network (localhost dev, tunneled access). apps/server refuses to
	// start unauthenticated when STACKIT_PUBLIC or $PORT are set unless
	// -auth-disabled is passed explicitly.
	Auth *AuthConfig
}

// AuthConfig is the runtime auth setup. SessionStore must outlive the
// Server's lifetime (close it after Shutdown returns).
type AuthConfig struct {
	Handler      *auth.Handler
	SessionStore auth.Store
}

// Server is the stackit-web HTTP server. Per-repo state (engine, watcher,
// broadcaster) lives on each registry entry — the server only owns
// transport-level concerns.
type Server struct {
	config     ServerConfig
	httpServer *http.Server
}

// NewServer creates a new API server backed by the given registry.
func NewServer(cfg ServerConfig) *Server {
	return &Server{config: cfg}
}

// Start begins serving HTTP requests. It blocks until the server is stopped.
func (s *Server) Start() error {
	apiMux := http.NewServeMux()
	prefixes := normalizeAPIPrefixes(s.config.APIPrefixes)

	reg := s.config.Registry

	viewHandler := handlers.NewViewHandler(reg)
	repoHandler := handlers.NewRepoHandler(reg)
	stacksHandler := handlers.NewStacksHandler(reg)
	branchesHandler := handlers.NewBranchesHandler(reg)
	branchDiffHandler := handlers.NewBranchDiffHandler(reg)
	submitHandler := handlers.NewSubmitHandler(reg)
	eventsHandler := handlers.NewEventsHandler(reg)
	reposListHandler := handlers.NewReposListHandler(reg)

	for _, prefix := range prefixes {
		// Unscoped index of available repos.
		apiMux.Handle("GET "+prefix+"/repos", reposListHandler)

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
	var protectedAPI http.Handler = apiMux
	if s.config.Auth != nil {
		protectedAPI = auth.RequireSession(s.config.Auth.SessionStore, apiMux)
	}

	authMux := http.NewServeMux()
	if s.config.Auth != nil {
		h := s.config.Auth.Handler
		authMux.HandleFunc("GET /auth/login", h.LoginHandler)
		authMux.HandleFunc("GET /auth/callback", h.CallbackHandler)
		authMux.HandleFunc("POST /auth/logout", h.LogoutHandler)
		authMux.HandleFunc("GET /auth/me", h.MeHandler)
	}

	webHandler := newStaticHandler(s.config.StaticFS)
	root := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isAPIPath(r.URL.Path, prefixes) {
			protectedAPI.ServeHTTP(w, r)
			return
		}

		if s.config.Auth != nil && strings.HasPrefix(r.URL.Path, "/auth/") {
			authMux.ServeHTTP(w, r)
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
	handler = securityHeadersMiddleware(handler)
	handler = requestIDMiddleware(handler)
	handler = recoverMiddleware(handler)

	s.httpServer = &http.Server{
		Addr:              fmt.Sprintf("%s:%d", s.config.BindAddr, s.config.Port),
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MiB
	}

	log.Printf("stackit-web server listening on http://%s:%d", displayHost(s.config.BindAddr), s.config.Port)
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully stops the server. The registry's watchers and
// broadcasters are torn down separately by main.go's defer reg.Close().
func (s *Server) Shutdown(ctx context.Context) error {
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
