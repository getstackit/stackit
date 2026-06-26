package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/getstackit/stackit/internal/api"
	"github.com/getstackit/stackit/internal/api/auth"
)

// authBuildResult is what buildAuthConfig returns when auth is enabled.
type authBuildResult struct {
	cfg   *api.AuthConfig
	store auth.Store // exposed separately so main can close it on shutdown
}

// authBuildParams describes the runtime posture buildAuthConfig needs. The
// auth requirement keys off exposure (a non-loopback bind), not the env name:
// a server reachable off-host must be authenticated or read-only, however it
// got that way.
type authBuildParams struct {
	disabled bool // -auth-disabled was passed
	exposed  bool // resolved bind is non-loopback (reachable off-host)
	readOnly bool // read-only posture: writes impossible, reads anonymous
	prod     bool // production env: force Secure cookies (behind TLS)
}

// buildAuthConfig wires up the OAuth handler, session store, and allowlist
// from environment variables. Returns (nil, nil) when auth is off (explicitly
// disabled, or unconfigured on a non-exposed/read-only server); an error when
// an exposed, writable server would be left unauthenticated.
func buildAuthConfig(p authBuildParams) (*authBuildResult, error) {
	// An exposed, writable server must be authenticated. Read-only removes the
	// write route by construction, so anonymous reads are safe there.
	mustAuth := p.exposed && !p.readOnly

	if p.disabled {
		if mustAuth {
			return nil, errors.New("-auth-disabled is not allowed when the server is reachable off-host (non-loopback bind): it would expose an unauthenticated, writable server. Pass -read-only, bind loopback (STACKIT_ENV=local), or configure GitHub OAuth")
		}
		return nil, nil
	}

	clientID := strings.TrimSpace(os.Getenv("STACKIT_GITHUB_CLIENT_ID"))
	clientSecret := strings.TrimSpace(os.Getenv("STACKIT_GITHUB_CLIENT_SECRET"))
	baseURL := strings.TrimSpace(os.Getenv("STACKIT_BASE_URL"))
	sessionKey := strings.TrimSpace(os.Getenv("STACKIT_SESSION_KEY"))

	// When the server isn't exposed (local loopback) or is read-only, missing
	// OAuth config is "off by default" — a local dev binary shouldn't need env
	// vars to boot, and a read-only server serves reads anonymously anyway.
	if clientID == "" && clientSecret == "" && baseURL == "" && sessionKey == "" {
		if mustAuth {
			return nil, errors.New("the server is reachable off-host (non-loopback bind) but auth is not configured: set STACKIT_GITHUB_CLIENT_ID, STACKIT_GITHUB_CLIENT_SECRET, STACKIT_BASE_URL, STACKIT_SESSION_KEY (and STACKIT_ALLOWED_GH_USERS or STACKIT_ALLOWED_GH_ORG), or pass -read-only to serve anonymously")
		}
		return nil, nil
	}

	if clientID == "" {
		return nil, errors.New("STACKIT_GITHUB_CLIENT_ID is required when configuring auth")
	}
	if clientSecret == "" {
		return nil, errors.New("STACKIT_GITHUB_CLIENT_SECRET is required when configuring auth")
	}
	if baseURL == "" {
		return nil, errors.New("STACKIT_BASE_URL is required when configuring auth")
	}
	if sessionKey == "" {
		return nil, errors.New("STACKIT_SESSION_KEY is required when configuring auth (generate with: openssl rand -base64 32)")
	}

	// An exposed server issues the session cookie over the network. If its
	// public base URL is plaintext http://, that bearer cookie travels in the
	// clear: it can't be marked Secure (a Secure cookie is never sent back over
	// http, which would silently break login), so it is capturable and
	// replayable on the network path. Fail closed rather than ship that posture.
	// Behind a TLS-terminating proxy the base URL is the public https:// URL, so
	// this check passes and the internal http hop is fine.
	if p.exposed && !strings.HasPrefix(baseURL, "https://") {
		return nil, errors.New("the server is reachable off-host (non-loopback bind) but STACKIT_BASE_URL is not https://: the session cookie would be sent over plaintext and could be captured and replayed. Use an https:// base URL (terminate TLS at a proxy if needed), or bind loopback (STACKIT_ENV=local)")
	}

	cipher, err := auth.NewCipher(sessionKey)
	if err != nil {
		return nil, fmt.Errorf("STACKIT_SESSION_KEY: %w", err)
	}

	allow, err := auth.NewAllowlist(auth.AllowlistConfig{
		Users: parseCSV(os.Getenv("STACKIT_ALLOWED_GH_USERS")),
		Org:   strings.TrimSpace(os.Getenv("STACKIT_ALLOWED_GH_ORG")),
	})
	if err != nil {
		return nil, fmt.Errorf("allowlist: %w", err)
	}

	store := auth.NewMemoryStore(cipher)

	handler, err := auth.NewHandler(auth.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		BaseURL:      baseURL,
		// Secure tracks actual exposure, not just the env name: any off-host
		// (exposed) deployment serves the cookie over TLS — guaranteed by the
		// https:// base-URL check above — so it must be marked Secure.
		Cookies: auth.CookieOptions{Secure: p.prod || p.exposed || strings.HasPrefix(baseURL, "https://")},
	}, store, allow, cipher)
	if err != nil {
		_ = store.Close()
		return nil, err
	}

	return &authBuildResult{
		cfg:   &api.AuthConfig{Handler: handler, SessionStore: store, Cipher: cipher},
		store: store,
	}, nil
}
