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
	"path/filepath"
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
// (STACKIT_ENV=production) emit JSON with a level field so log
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
		bind          = flag.String("bind", "", "Interface to bind on. Defaults to 127.0.0.1 (loopback); STACKIT_ENV=production binds 0.0.0.0.")
		cwd           = flag.String("cwd", "", "Working directory for repository detection (single-repo shortcut; ignored when -database-url is set)")
		databaseURL   = flag.String("database-url", os.Getenv("STACKIT_DATABASE_URL"), "PostgreSQL connection string. When set, repos are served from the DB instead of the -cwd single-repo shortcut.")
		reposRoot     = flag.String("repos-root", os.Getenv("STACKIT_REPOS_ROOT"), "Base directory under which per-repo checkouts live (<reposRoot>/<owner>/<name>) for DB-sourced repos.")
		remote        = flag.String("remote", "origin", "Default git remote name for the single-repo -cwd shortcut")
		corsOrigins   = flag.String("cors", "http://localhost:3000,http://localhost:5173", "Comma-separated allowed CORS origins")
		apiPrefix     = flag.String("api-prefix", "/api/v1", "Canonical API prefix")
		enableLegacy  = flag.Bool("legacy-api-prefix", true, "Also expose legacy /api endpoints")
		shutdownGrace = flag.Duration("shutdown-timeout", 10*time.Second, "Graceful shutdown timeout")
		// Intentionally flag-only (no STACKIT_* env binding): disabling the auth
		// gate must be a deliberate, visible act on the command line, not
		// something a stray env var in a shell or deploy config can switch on.
		authDisabled    = flag.Bool("auth-disabled", false, "Disable GitHub OAuth gate. Refused when the server binds a non-loopback interface (e.g. STACKIT_ENV=production) unless -read-only is set.")
		authForce       = flag.Bool("auth", envBool("STACKIT_AUTH"), "Force-enable the GitHub OAuth gate on a loopback bind to test the login flow locally (loopback binds are anonymous by default even when STACKIT_GITHUB_* is set). Ignored when the bind is exposed (auth is already required there). Also set via STACKIT_AUTH.")
		readOnly        = flag.Bool("read-only", envBool("STACKIT_READ_ONLY"), "Serve in read-only mode: the submit endpoint is disabled so the repo can be exposed publicly without write access. Also set via STACKIT_READ_ONLY.")
		syncInterval    = flag.Duration("sync-interval", envSyncInterval(), "How often to mirror-fetch managed repos from their remotes so served state stays current. Defaults to 5m (the webhook backstop); 0 disables the loop. Also set via STACKIT_SYNC_INTERVAL (e.g. 60s).")
		webhookDebounce = flag.Duration("webhook-debounce", envWebhookDebounce(), "Quiet window a repo's webhook pushes wait out before a fetch, so a stack submit's burst of pushes collapses into one refresh. Defaults to 2s; 0 dispatches immediately. Also set via STACKIT_WEBHOOK_DEBOUNCE (e.g. 5s).")
	)
	flag.Parse()

	env, err := resolveEnv(os.Getenv("STACKIT_ENV"))
	if err != nil {
		return err
	}
	prod := env.isProduction()
	setupLogging(prod)

	slog.Info("stackit-server starting", "version", version, "commit", commit, "built", date, "env", env.String())

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
	// $PORT is the PaaS convention (Railway, Fly, Heroku inject it); honored
	// only in production so a stray $PORT from a dev shell can't move the local
	// listener. resolveBind likewise binds loopback locally, 0.0.0.0 in prod.
	if err := resolvePort(port, portExplicit, prod, os.Getenv("PORT")); err != nil {
		return err
	}
	resolveBind(bind, bindExplicit, prod)

	// The auth requirement keys off actual exposure (a non-loopback bind),
	// not the env name: binding off-host without auth or read-only is refused
	// no matter how we got there (prod default, explicit -bind, etc.).
	exposed := !isLoopbackBind(*bind)

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

	// repoStore is non-nil only in DB-backed mode; it is handed to the server
	// so runtime onboarding can persist newly added repos.
	var repoStore *store.Store
	switch {
	case *databaseURL != "":
		st, err := store.Open(context.Background(), *databaseURL)
		if err != nil {
			return err
		}
		repoStore = st
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

	authBuild, err := buildAuthConfig(authBuildParams{
		disabled:  *authDisabled,
		forceAuth: *authForce,
		exposed:   exposed,
		readOnly:  *readOnly,
		prod:      prod,
	})
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
		slog.Warn("auth: DISABLED — anonymous access. Expected on a loopback bind; pass -auth (STACKIT_AUTH) to test the OAuth login flow locally. Never expose this port publicly.")
	}

	// GitHub App provider authenticates onboarding clones and background syncs.
	// Nil when no App is configured.
	appProvider, err := buildAppProvider()
	if err != nil {
		return err
	}
	if appProvider != nil {
		slog.Info("github app: installation-token provider enabled")
	}

	// Resolve the repos root to an absolute path so onboarded checkouts land
	// in the same place boot-loaded repos resolve to (loadReposFromDB also
	// makes it absolute), keeping persisted repos host-portable.
	absReposRoot := *reposRoot
	if absReposRoot != "" {
		if abs, absErr := filepath.Abs(absReposRoot); absErr == nil {
			absReposRoot = abs
		}
	}

	server := api.NewServer(api.ServerConfig{
		BindAddr:    *bind,
		Port:        *port,
		CORSOrigins: parseCSV(*corsOrigins),
		// On a loopback bind, accept any loopback browser origin so a local dev
		// web server on an environment-assigned port (e.g. cmux-set $PORT) isn't
		// blocked by CORS. Never enabled when exposed.
		AllowLoopbackOrigins: !exposed,
		// Single-repo mode: anything but DB-backed multi-tenant serves one repo
		// discovered from -cwd, so the web client opens it directly instead of
		// showing the (hosted-only) repo picker.
		SingleRepo:          *databaseURL == "",
		APIPrefixes:         prefixes,
		StaticFS:            staticFS,
		Registry:            reg,
		Auth:                authCfg,
		ReadOnly:            *readOnly,
		RepoStore:           repoStore,
		ReposRoot:           absReposRoot,
		RepoSyncTokens:      appProvider,
		SyncInterval:        *syncInterval,
		WebhookDebounce:     *webhookDebounce,
		GitHubWebhookSecret: strings.TrimSpace(os.Getenv("STACKIT_GITHUB_WEBHOOK_SECRET")),
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
		AddedBy:     rc.AddedBy,
		Managed:     rc.Managed,
		Owner:       rc.Owner,
		Name:        rc.Name,
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

// defaultSyncInterval is the background mirror-fetch cadence used when
// STACKIT_SYNC_INTERVAL is unset. It exists so the webhook backstop is present
// out of the box: even if a delivery is missed (or a push only touched stackit
// metadata refs, which GitHub sends no push event for), the loop catches up
// within this window. Operators tune it down for fresher state or set 0 to
// disable polling and rely solely on webhooks.
const defaultSyncInterval = 5 * time.Minute

// envSyncInterval resolves the sync-loop interval default: the value of
// STACKIT_SYNC_INTERVAL when set ("0" explicitly disables the loop), otherwise
// defaultSyncInterval. An unparseable value falls back to the default rather
// than silently disabling the loop.
func envSyncInterval() time.Duration {
	raw := strings.TrimSpace(os.Getenv("STACKIT_SYNC_INTERVAL"))
	if raw == "" {
		return defaultSyncInterval
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return defaultSyncInterval
	}
	return d
}

// defaultWebhookDebounce is the quiet window webhook-triggered fetches wait out
// when STACKIT_WEBHOOK_DEBOUNCE is unset, collapsing a stack submit's burst of
// pushes into a single refresh.
const defaultWebhookDebounce = 2 * time.Second

// envWebhookDebounce resolves the webhook-debounce default: STACKIT_WEBHOOK_DEBOUNCE
// when set ("0" dispatches immediately), otherwise defaultWebhookDebounce. An
// unparseable value falls back to the default.
func envWebhookDebounce() time.Duration {
	raw := strings.TrimSpace(os.Getenv("STACKIT_WEBHOOK_DEBOUNCE"))
	if raw == "" {
		return defaultWebhookDebounce
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return defaultWebhookDebounce
	}
	return d
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
