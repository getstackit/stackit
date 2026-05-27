package main

import (
	"testing"

	"github.com/getstackit/stackit/internal/api/auth"

	"github.com/stretchr/testify/require"
)

func TestBuildAuthConfig_DisabledLocalOK(t *testing.T) {
	t.Parallel()

	r, err := buildAuthConfig(true, false)
	require.NoError(t, err)
	require.Nil(t, r)
}

func TestBuildAuthConfig_DisabledInPublicModeRefused(t *testing.T) {
	t.Parallel()

	_, err := buildAuthConfig(true, true)
	require.Error(t, err)
}

func TestBuildAuthConfig_NoEnvLocalFallsThroughToUnauthed(t *testing.T) {
	// t.Setenv is incompatible with t.Parallel; these tests share process env.
	withEnv(t, map[string]string{
		"STACKIT_GITHUB_CLIENT_ID":     "",
		"STACKIT_GITHUB_CLIENT_SECRET": "",
		"STACKIT_BASE_URL":             "",
		"STACKIT_SESSION_KEY":          "",
		"STACKIT_ALLOWED_GH_USERS":     "",
		"STACKIT_ALLOWED_GH_ORG":       "",
	})

	r, err := buildAuthConfig(false, false)
	require.NoError(t, err)
	require.Nil(t, r, "no env + local mode = no auth, no error")
}

func TestBuildAuthConfig_NoEnvInPublicModeRefused(t *testing.T) {
	// t.Setenv is incompatible with t.Parallel; these tests share process env.
	withEnv(t, map[string]string{
		"STACKIT_GITHUB_CLIENT_ID":     "",
		"STACKIT_GITHUB_CLIENT_SECRET": "",
		"STACKIT_BASE_URL":             "",
		"STACKIT_SESSION_KEY":          "",
		"STACKIT_ALLOWED_GH_USERS":     "",
		"STACKIT_ALLOWED_GH_ORG":       "",
	})

	_, err := buildAuthConfig(false, true)
	require.Error(t, err)
}

func TestBuildAuthConfig_PartialEnvErrors(t *testing.T) {
	// t.Setenv is incompatible with t.Parallel; these tests share process env.
	withEnv(t, map[string]string{
		"STACKIT_GITHUB_CLIENT_ID":     "id",
		"STACKIT_GITHUB_CLIENT_SECRET": "",
		"STACKIT_BASE_URL":             "https://example.com",
		"STACKIT_SESSION_KEY":          mustKey(t),
		"STACKIT_ALLOWED_GH_USERS":     "jonnii",
	})

	_, err := buildAuthConfig(false, false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "STACKIT_GITHUB_CLIENT_SECRET")
}

func TestBuildAuthConfig_HappyPath(t *testing.T) {
	// t.Setenv is incompatible with t.Parallel; these tests share process env.
	withEnv(t, map[string]string{
		"STACKIT_GITHUB_CLIENT_ID":     "id",
		"STACKIT_GITHUB_CLIENT_SECRET": "secret",
		"STACKIT_BASE_URL":             "https://example.com",
		"STACKIT_SESSION_KEY":          mustKey(t),
		"STACKIT_ALLOWED_GH_USERS":     "jonnii",
	})

	r, err := buildAuthConfig(false, false)
	require.NoError(t, err)
	require.NotNil(t, r)
	require.NotNil(t, r.cfg.Handler)
	require.NotNil(t, r.cfg.SessionStore)
	require.NoError(t, r.store.Close())
}

func TestBuildAuthConfig_EmptyAllowlistErrors(t *testing.T) {
	// t.Setenv is incompatible with t.Parallel; these tests share process env.
	withEnv(t, map[string]string{
		"STACKIT_GITHUB_CLIENT_ID":     "id",
		"STACKIT_GITHUB_CLIENT_SECRET": "secret",
		"STACKIT_BASE_URL":             "https://example.com",
		"STACKIT_SESSION_KEY":          mustKey(t),
		"STACKIT_ALLOWED_GH_USERS":     "",
		"STACKIT_ALLOWED_GH_ORG":       "",
	})

	_, err := buildAuthConfig(false, false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "allowlist")
}

func withEnv(t *testing.T, kvs map[string]string) {
	t.Helper()
	for k, v := range kvs {
		t.Setenv(k, v)
	}
}

func mustKey(t *testing.T) string {
	t.Helper()
	k, err := auth.GenerateSessionKey()
	require.NoError(t, err)
	return k
}
