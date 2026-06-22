package main

import (
	"context"
	"embed"
	"errors"
	"flag"
	"io/fs"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/getstackit/stackit/internal/api"
	"github.com/getstackit/stackit/internal/api/provision"
	"github.com/getstackit/stackit/internal/api/registry"
	"github.com/getstackit/stackit/internal/api/store"
)

// all: is required because Next static exports use underscore-prefixed paths like _next/.
//
//go:embed all:static
var staticFiles embed.FS

// Build metadata injected via -ldflags by goreleaser (production) and the
// `mise run build` task (local). Defaults are surfaced by `go run` and bare
// `go build` so the binary is still self-identifying.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	if err := run(); err != nil {
		slog.Error("startup failed", "error", err)
		os.Exit(1)
	}
}

// setupLogging configures the default slog logger. Production deploys
// (PORT or STACKIT_PUBLIC set) emit JSON with a level field so log
// aggregators tag entries correctly; local runs use the text handler
// for readability. Output goes to stdout so platforms like Railway
// don't classify everything as error (which is what happens when the
// stdlib log package writes to stderr).
func setupLogging(jsonOutput bool) {
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	var handler slog.Handler
	if jsonOutput {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}
	slog.SetDefault(slog.New(handler))
	log.SetFlags(0)
	log.SetOutput(os.Stdout)
}

func run() error {
	var (
		port          = flag.Int("port", 8080, "Port to listen on")
		bind          = flag.String("bind", "", "Interface to bind on. Defaults to 127.0.0.1; switches to 0.0.0.0 when $PORT or $STACKIT_PUBLIC is set.")
		cwd           = flag.String("cwd", "", "Working directory for repository detection (single-repo shortcut; ignored when -database-url is set)")
		databaseURL   = flag.String("database-url", os.Getenv("STACKIT_DATABASE_URL"), "PostgreSQL connection string. When set, repos are served from the DB instead of the -cwd single-repo shortcut.")
		reposRoot     = flag.String("repos-root", os.Getenv("STACKIT_REPOS_ROOT"), "Base directory under which per-repo checkouts live (<reposRoot>/<owner>/<name>) for DB-sourced repos.")
		remote        = flag.String("remote", "origin", "Default git remote name for the single-repo -cwd shortcut")
		corsOrigins   = flag.String("cors", "http://localhost:3000,http://localhost:5173", "Comma-separated allowed CORS origins")
		apiPrefix     = flag.String("api-prefix", "/api/v1", "Canonical API prefix")
		enableLegacy  = flag.Bool("legacy-api-prefix", true, "Also expose legacy /api endpoints")
		shutdownGrace = flag.Duration("shutdown-timeout", 10*time.Second, "Graceful shutdown timeout")
		authDisabled  = flag.Bool("auth-disabled", false, "Disable GitHub OAuth gate. Refused in public mode ($PORT or $STACKIT_PUBLIC).")
		readOnly      = flag.Bool("read-only", envBool("STACKIT_READ_ONLY"), "Serve in read-only mode: the submit endpoint is disabled so the repo can be exposed publicly without write access. Also set via STACKIT_READ_ONLY.")
	)
	flag.Parse()

	publicMode := os.Getenv("PORT") != "" || os.Getenv("STACKIT_PUBLIC") != ""
	setupLogging(publicMode)

	slog.Info("stackit-server starting", "version", version, "commit", commit, "built", date)

	// Honor $PORT when -port wasn't passed explicitly. PaaS hosts (Railway,
	// Fly, Heroku) inject the port this way.
	portExplicit := false
	bindExplicit := false
	cwdExplicit := false
	flag.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "port":
			portExplicit = true
		case "bind":
			bindExplicit = true
		case "cwd":
			cwdExplicit = true
		}
	})
	if err := resolvePort(port, portExplicit, os.Getenv("PORT")); err != nil {
		return err
	}
	resolveBind(bind, bindExplicit, publicMode)

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
			slog.Error("registry close failed", "error", closeErr)
		}
	}()

	switch {
	case *databaseURL != "":
		st, err := store.Open(context.Background(), *databaseURL)
		if err != nil {
			return err
		}
		defer func() {
			if closeErr := st.Close(); closeErr != nil {
				slog.Error("repo store close failed", "error", closeErr)
			}
		}()

		repos, err := loadReposFromDB(context.Background(), st, *reposRoot)
		if err != nil {
			return err
		}
		for _, rc := range repos {
			if err := addRegistryEntry(reg, rc); err != nil {
				// A missing checkout shouldn't take down the whole
				// server — other configured repos may still be usable,
				// and a future "add repo from GitHub" flow will clone
				// these on demand. Log and skip instead.
				slog.Warn("repo not registered", "repo", rc.ID, "error", err)
			}
		}
	default:
		// Single-repo shortcut. `-cwd ""` falls back to git discovery
		// from the process cwd. Discovery is best-effort when -cwd was
		// not passed explicitly: hosted deploys (Railway, Fly, ...)
		// often start the binary from a non-repo directory and should
		// still come up with an empty registry rather than crashing.
		// When -cwd is set explicitly the operator asked for that
		// path, so failure remains fatal.
		err := addRegistryEntry(reg, repoConfig{
			ID:          "default",
			DisplayName: "default",
			Path:        *cwd,
			Remote:      *remote,
		})
		if err != nil {
			if cwdExplicit {
				return err
			}
			slog.Info("no default repo registered", "error", err)
		}
	}

	authBuild, err := buildAuthConfig(*authDisabled, publicMode)
	if err != nil {
		return err
	}
	var authCfg *api.AuthConfig
	if authBuild != nil {
		authCfg = authBuild.cfg
		defer func() {
			if closeErr := authBuild.store.Close(); closeErr != nil {
				slog.Error("session store close failed", "error", closeErr)
			}
		}()
		slog.Info("auth: GitHub OAuth gate enabled")
	} else {
		slog.Warn("auth: DISABLED (no STACKIT_GITHUB_* env or -auth-disabled set). Do not expose this port publicly.")
	}

	server := api.NewServer(api.ServerConfig{
		BindAddr:    *bind,
		Port:        *port,
		CORSOrigins: parseCSV(*corsOrigins),
		APIPrefixes: prefixes,
		StaticFS:    staticFS,
		Registry:    reg,
		Auth:        authCfg,
		ReadOnly:    *readOnly,
	})

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start()
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	select {
	case sig := <-stop:
		slog.Info("shutting down", "signal", sig.String())
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

// addRegistryEntry resolves rc into a RepoEntry via provision.BuildEntry and
// adds it to reg. The build path is shared with runtime onboarding so startup
// and "add a repo" can't drift.
func addRegistryEntry(reg *registry.Registry, rc repoConfig) error {
	entry, err := provision.BuildEntry(context.Background(), provision.EntryParams{
		ID:          rc.ID,
		DisplayName: rc.DisplayName,
		Path:        rc.Path,
		Remote:      rc.Remote,
	})
	if err != nil {
		return err
	}
	return reg.Add(entry)
}

// envBool reports whether the named environment variable is set to a truthy
// value (1, t, true, etc. per strconv.ParseBool). Unset or unparseable
// values are false, so STACKIT_READ_ONLY=0 disables the mode rather than
// tripping it the way a bare presence check would.
func envBool(name string) bool {
	v, err := strconv.ParseBool(os.Getenv(name))
	return err == nil && v
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
