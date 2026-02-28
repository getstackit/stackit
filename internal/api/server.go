// Package api provides the HTTP server for the stackit-web application.
package api

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"path"
	"strings"
	"time"

	"stackit.dev/stackit/internal/api/handlers"
	"stackit.dev/stackit/internal/api/watcher"
	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/github"
)

// ServerConfig holds configuration for the API server.
type ServerConfig struct {
	Port        int
	CORSOrigins []string
	RepoRoot    string
	Remote      string
	APIPrefixes []string
	StaticFS    fs.FS
}

// Server is the stackit-web HTTP server.
type Server struct {
	config      ServerConfig
	eng         engine.Engine
	gh          github.Client
	broadcaster *handlers.EventBroadcaster
	httpServer  *http.Server
	refWatcher  *watcher.RefWatcher
}

// NewServer creates a new API server.
func NewServer(cfg ServerConfig, eng engine.Engine, gh github.Client) *Server {
	return &Server{
		config:      cfg,
		eng:         eng,
		gh:          gh,
		broadcaster: handlers.NewEventBroadcaster(),
	}
}

// Broadcaster returns the event broadcaster for triggering SSE updates.
func (s *Server) Broadcaster() *handlers.EventBroadcaster {
	return s.broadcaster
}

// Start begins serving HTTP requests. It blocks until the server is stopped.
func (s *Server) Start() error {
	apiMux := http.NewServeMux()
	prefixes := normalizeAPIPrefixes(s.config.APIPrefixes)

	viewHandler := handlers.NewViewHandler(s.eng, s.gh, s.config.Remote)
	repoHandler := handlers.NewRepoHandler(s.eng, s.gh, s.config.Remote)
	stacksHandler := handlers.NewStacksHandler(s.eng, s.gh)
	branchesHandler := handlers.NewBranchesHandler(s.eng, s.gh)
	eventsHandler := handlers.NewEventsHandler(s.broadcaster)

	for _, prefix := range prefixes {
		apiMux.Handle(path.Join(prefix, "view"), viewHandler)
		apiMux.Handle(path.Join(prefix, "repo"), repoHandler)
		apiMux.Handle(path.Join(prefix, "stacks"), stacksHandler)
		apiMux.Handle(path.Join(prefix, "stacks")+"/", stacksHandler)
		apiMux.Handle(path.Join(prefix, "branches"), branchesHandler)
		apiMux.Handle(path.Join(prefix, "branches")+"/", branchesHandler)
		apiMux.Handle(path.Join(prefix, "events"), eventsHandler)
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

	// Wrap with middleware
	handler := corsMiddleware(s.config.CORSOrigins, root)
	handler = loggingMiddleware(handler)

	// Start watching git refs for changes so the engine stays current
	if s.config.RepoRoot != "" {
		trunkName := s.eng.Trunk().GetName()
		s.refWatcher = watcher.NewRefWatcher(s.config.RepoRoot, 200*time.Millisecond, func() {
			if err := s.eng.Rebuild(trunkName); err != nil {
				log.Printf("engine rebuild failed: %v", err)
				return
			}
			s.broadcaster.Broadcast("refresh", "{}")
		})
		go func() {
			if err := s.refWatcher.Start(); err != nil {
				log.Printf("ref watcher stopped: %v", err)
			}
		}()
	}

	s.httpServer = &http.Server{
		Addr:              fmt.Sprintf(":%d", s.config.Port),
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("stackit-web server listening on http://localhost:%d", s.config.Port)
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.refWatcher != nil {
		s.refWatcher.Stop()
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

func isAPIPath(path string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}
