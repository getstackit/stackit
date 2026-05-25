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

	"github.com/getstackit/stackit/internal/api/handlers"
	"github.com/getstackit/stackit/internal/api/registry"
)

// ServerConfig holds configuration for the API server.
type ServerConfig struct {
	Port        int
	CORSOrigins []string
	APIPrefixes []string
	StaticFS    fs.FS
	Registry    *registry.Registry
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

	webHandler := newStaticHandler(s.config.StaticFS)
	root := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isAPIPath(r.URL.Path, prefixes) {
			apiMux.ServeHTTP(w, r)
			return
		}

		if webHandler != nil {
			webHandler.ServeHTTP(w, r)
			return
		}

		http.NotFound(w, r)
	})

	handler := corsMiddleware(s.config.CORSOrigins, root)
	handler = loggingMiddleware(handler)

	s.httpServer = &http.Server{
		Addr:              fmt.Sprintf(":%d", s.config.Port),
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("stackit-web server listening on http://localhost:%d", s.config.Port)
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

func isAPIPath(path string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}
