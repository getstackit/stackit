// Package api provides the HTTP server for the stackit-web application.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/getstackit/stackit/internal/api/handlers"
	"github.com/getstackit/stackit/internal/api/watcher"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/github"
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
	config            ServerConfig
	eng               engine.Engine
	gh                github.Client
	broadcaster       *handlers.EventBroadcaster
	httpServer        *http.Server
	refWatcher        *watcher.RefWatcher
	watcherWG         sync.WaitGroup
	lastCurrentBranch string
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
	branchDiffHandler := handlers.NewBranchDiffHandler(s.eng)
	eventsHandler := handlers.NewEventsHandler(s.broadcaster)

	for _, prefix := range prefixes {
		apiMux.Handle(path.Join(prefix, "view"), viewHandler)
		apiMux.Handle(path.Join(prefix, "repo"), repoHandler)
		apiMux.Handle(path.Join(prefix, "stacks"), stacksHandler)
		apiMux.Handle(path.Join(prefix, "stacks")+"/", stacksHandler)
		apiMux.Handle(path.Join(prefix, "branches"), branchesHandler)
		apiMux.Handle(path.Join(prefix, "branches")+"/", branchesHandler)
		apiMux.Handle(path.Join(prefix, "branch-diff"), branchDiffHandler)
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
		if cb := s.eng.CurrentBranch(); cb != nil {
			s.lastCurrentBranch = cb.GetName()
		}
		s.refWatcher = watcher.NewRefWatcher(s.config.RepoRoot, 200*time.Millisecond, func() {
			if err := s.eng.Rebuild(trunkName); err != nil {
				log.Printf("engine rebuild failed: %v", err)
				return
			}

			if cb := s.eng.CurrentBranch(); cb != nil {
				newBranch := cb.GetName()
				if s.lastCurrentBranch != "" && newBranch != s.lastCurrentBranch {
					data, _ := json.Marshal(map[string]string{
						"from":      s.lastCurrentBranch,
						"to":        newBranch,
						"timestamp": time.Now().Format(time.RFC3339),
					})
					s.broadcaster.Broadcast("branch_switched", string(data))
				}
				s.lastCurrentBranch = newBranch
			}

			s.broadcaster.Broadcast("refresh", "{}")
		})
		s.watcherWG.Go(func() {
			if err := s.refWatcher.Start(); err != nil {
				log.Printf("ref watcher stopped: %v", err)
			}
		})
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
	s.broadcaster.Close()
	if s.refWatcher != nil {
		s.refWatcher.Stop()
	}

	// Wait for the ref watcher goroutine to exit, bounded by ctx.
	watcherDone := make(chan struct{})
	go func() {
		s.watcherWG.Wait()
		close(watcherDone)
	}()
	select {
	case <-watcherDone:
	case <-ctx.Done():
		log.Printf("ref watcher did not exit before shutdown deadline")
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
