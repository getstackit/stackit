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

// buildAuthConfig wires up the OAuth handler, session store, and allowlist
// from environment variables. Returns (nil, nil, nil) when auth is
// explicitly disabled; an error when required env is missing in public
// mode.
func buildAuthConfig(authDisabled, publicMode bool) (*authBuildResult, error) {
	if authDisabled {
		if publicMode {
			return nil, errors.New("-auth-disabled is not allowed when $PORT or $STACKIT_PUBLIC are set; remove the flag or unset the env vars")
		}
		return nil, nil
	}

	clientID := strings.TrimSpace(os.Getenv("STACKIT_GITHUB_CLIENT_ID"))
	clientSecret := strings.TrimSpace(os.Getenv("STACKIT_GITHUB_CLIENT_SECRET"))
	baseURL := strings.TrimSpace(os.Getenv("STACKIT_BASE_URL"))
	sessionKey := strings.TrimSpace(os.Getenv("STACKIT_SESSION_KEY"))

	// Outside public mode, missing OAuth config is "off by default" —
	// users running a local dev binary shouldn't have to set env vars to
	// boot. They can pass -auth-disabled explicitly to make the intent
	// clear, or just leave the OAuth env unset.
	if clientID == "" && clientSecret == "" && baseURL == "" && sessionKey == "" {
		if publicMode {
			return nil, errors.New("public mode requires STACKIT_GITHUB_CLIENT_ID, STACKIT_GITHUB_CLIENT_SECRET, STACKIT_BASE_URL, STACKIT_SESSION_KEY (and STACKIT_ALLOWED_GH_USERS or STACKIT_ALLOWED_GH_ORG); pass -auth-disabled if you really mean to run open")
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
		Cookies:      auth.CookieOptions{Secure: publicMode || strings.HasPrefix(baseURL, "https://")},
	}, store, allow, cipher)
	if err != nil {
		_ = store.Close()
		return nil, err
	}

	return &authBuildResult{
		cfg:   &api.AuthConfig{Handler: handler, SessionStore: store},
		store: store,
	}, nil
}
