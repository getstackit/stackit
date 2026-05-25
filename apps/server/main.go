package main

import (
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/getstackit/stackit/internal/api"
	"github.com/getstackit/stackit/internal/api/registry"
	"github.com/getstackit/stackit/internal/app"
)

// all: is required because Next static exports use underscore-prefixed paths like _next/.
//
//go:embed all:static
var staticFiles embed.FS

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	var (
		port            = flag.Int("port", 8080, "Port to listen on")
		cwd             = flag.String("cwd", "", "Working directory for repository detection (single-repo shortcut; ignored when -repos-config is set)")
		reposConfigPath = flag.String("repos-config", "", "Path to a JSON file listing repos to serve (mutually exclusive with -cwd)")
		remote          = flag.String("remote", "origin", "Default git remote name for the single-repo -cwd shortcut")
		corsOrigins     = flag.String("cors", "http://localhost:3000,http://localhost:5173", "Comma-separated allowed CORS origins")
		apiPrefix       = flag.String("api-prefix", "/api/v1", "Canonical API prefix")
		enableLegacy    = flag.Bool("legacy-api-prefix", true, "Also expose legacy /api endpoints")
		shutdownGrace   = flag.Duration("shutdown-timeout", 10*time.Second, "Graceful shutdown timeout")
	)
	flag.Parse()

	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return err
	}

	prefixes := []string{*apiPrefix}
	if *enableLegacy && *apiPrefix != "/api" {
		prefixes = append(prefixes, "/api")
	}

	reg := registry.New()
	defer func() {
		if closeErr := reg.Close(); closeErr != nil {
			log.Printf("registry close: %v", closeErr)
		}
	}()

	switch {
	case *reposConfigPath != "":
		cfg, err := loadReposConfig(*reposConfigPath)
		if err != nil {
			return err
		}
		for _, rc := range cfg.Repos {
			if err := addRegistryEntry(reg, rc); err != nil {
				return fmt.Errorf("repo %q: %w", rc.ID, err)
			}
		}
	default:
		// Single-repo shortcut. `-cwd ""` falls back to git discovery
		// from the process cwd, matching the previous behavior.
		if err := addRegistryEntry(reg, repoConfig{
			ID:          "default",
			DisplayName: "default",
			Path:        *cwd,
			Remote:      *remote,
		}); err != nil {
			return err
		}
	}

	server := api.NewServer(api.ServerConfig{
		Port:        *port,
		CORSOrigins: parseCSV(*corsOrigins),
		APIPrefixes: prefixes,
		StaticFS:    staticFS,
		Registry:    reg,
	})

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start()
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	select {
	case sig := <-stop:
		log.Printf("received %s, shutting down", sig)
		ctx, cancel := context.WithTimeout(context.Background(), *shutdownGrace)
		defer cancel()
		return server.Shutdown(ctx)
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

// addRegistryEntry resolves rc into an engine via app.GetContext and adds
// the resulting RepoEntry to reg, registering a logger-close callback so
// reg.Close releases the file handle on shutdown.
func addRegistryEntry(reg *registry.Registry, rc repoConfig) error {
	opts := app.GetDefaultGlobalOptions()
	opts.Cwd = rc.Path
	opts.Interactive = false

	runtimeCtx, err := app.GetContext(context.Background(), opts)
	if err != nil {
		return err
	}

	gh := runtimeCtx.GitHub()
	if gh == nil && runtimeCtx.GitHubError() != nil {
		log.Printf("[%s] GitHub client unavailable: %v", rc.ID, runtimeCtx.GitHubError())
	}

	entry := &registry.RepoEntry{
		ID:          rc.ID,
		DisplayName: rc.DisplayName,
		RepoRoot:    runtimeCtx.RepoRoot,
		Remote:      rc.Remote,
		Engine:      runtimeCtx.Engine,
		GitHub:      gh,
	}
	if runtimeCtx.Logger != nil {
		entry.AddCloser(runtimeCtx.Logger.Close)
	}

	return reg.Add(entry)
}

func parseCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, strings.TrimRight(part, "/"))
	}
	return out
}
