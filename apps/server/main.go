package main

import (
	"context"
	"embed"
	"errors"
	"flag"
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
		port          = flag.Int("port", 8080, "Port to listen on")
		cwd           = flag.String("cwd", "", "Working directory for repository detection")
		remote        = flag.String("remote", "origin", "Git remote name used in API responses")
		corsOrigins   = flag.String("cors", "http://localhost:3000,http://localhost:5173", "Comma-separated allowed CORS origins")
		apiPrefix     = flag.String("api-prefix", "/api/v1", "Canonical API prefix")
		enableLegacy  = flag.Bool("legacy-api-prefix", true, "Also expose legacy /api endpoints")
		shutdownGrace = flag.Duration("shutdown-timeout", 10*time.Second, "Graceful shutdown timeout")
	)
	flag.Parse()

	opts := app.GetDefaultGlobalOptions()
	opts.Cwd = *cwd
	opts.Interactive = false

	runtimeCtx, err := app.GetContext(context.Background(), opts)
	if err != nil {
		return err
	}
	defer func() {
		if runtimeCtx.Logger != nil {
			if closeErr := runtimeCtx.Logger.Close(); closeErr != nil {
				log.Printf("failed to close logger: %v", closeErr)
			}
		}
	}()

	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return err
	}

	prefixes := []string{*apiPrefix}
	if *enableLegacy && *apiPrefix != "/api" {
		prefixes = append(prefixes, "/api")
	}

	gh := runtimeCtx.GitHub()
	if gh == nil && runtimeCtx.GitHubError() != nil {
		log.Printf("GitHub client unavailable: %v", runtimeCtx.GitHubError())
	}

	reg := registry.New()
	if err := reg.Add(&registry.RepoEntry{
		ID:          "default",
		DisplayName: "default",
		RepoRoot:    runtimeCtx.RepoRoot,
		Remote:      *remote,
		Engine:      runtimeCtx.Engine,
		GitHub:      gh,
	}); err != nil {
		return err
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
