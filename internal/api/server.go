// Package api provides the HTTP server for the stackit-web application.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/getstackit/stackit/internal/api/handlers"
	"github.com/getstackit/stackit/internal/api/registry"
	"github.com/getstackit/stackit/internal/api/watcher"
)

// ServerConfig holds configuration for the API server.
type ServerConfig struct {
	Port        int
	CORSOrigins []string
	APIPrefixes []string
	StaticFS    fs.FS
	Registry    *registry.Registry
}

// Server is the stackit-web HTTP server.
type Server struct {
	config            ServerConfig
	broadcaster       *handlers.EventBroadcaster
	httpServer        *http.Server
	refWatcher        *watcher.RefWatcher
	watcherWG         sync.WaitGroup
	lastCurrentBranch string
}

// NewServer creates a new API server backed by the given registry. Per-repo
// engine and GitHub client state lives on each registry entry; the server
// only owns transport-level concerns and (for now) a single broadcaster +
// watcher that follow the first registered repo. A follow-up change moves
// the watcher and broadcaster onto each entry so events are per-repo.
func NewServer(cfg ServerConfig) *Server {
	return &Server{
		config:      cfg,
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

	reg := s.config.Registry

	viewHandler := handlers.NewViewHandler(reg)
	repoHandler := handlers.NewRepoHandler(reg)
	stacksHandler := handlers.NewStacksHandler(reg)
	branchesHandler := handlers.NewBranchesHandler(reg)
	branchDiffHandler := handlers.NewBranchDiffHandler(reg)
	submitHandler := handlers.NewSubmitHandler(reg)
	eventsHandler := handlers.NewEventsHandler(s.broadcaster)
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

	// Start watching the first registered repo's refs so the engine stays
	// current. A follow-up change spawns one watcher per registry entry so
	// every repo's refs are tracked independently.
	if reg != nil {
		if entries := reg.List(); len(entries) > 0 {
			entry := entries[0]
			if entry.RepoRoot != "" {
				trunkName := entry.Engine.Trunk().GetName()
				if cb := entry.Engine.CurrentBranch(); cb != nil {
					s.lastCurrentBranch = cb.GetName()
				}
				s.refWatcher = watcher.NewRefWatcher(entry.RepoRoot, 200*time.Millisecond, func() {
					if err := entry.Engine.Rebuild(trunkName); err != nil {
						log.Printf("engine rebuild failed: %v", err)
						return
					}

					if cb := entry.Engine.CurrentBranch(); cb != nil {
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
		}
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
