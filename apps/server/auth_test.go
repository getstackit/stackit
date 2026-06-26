package main

import (
	"testing"

	"github.com/getstackit/stackit/internal/api/auth"

	"github.com/stretchr/testify/require"
)

func TestBuildAuthConfig_DisabledLocalOK(t *testing.T) {
	t.Parallel()

	r, err := buildAuthConfig(authBuildParams{disabled: true})
	require.NoError(t, err)
	require.Nil(t, r)
}

func TestBuildAuthConfig_DisabledExposedRefused(t *testing.T) {
	t.Parallel()

	_, err := buildAuthConfig(authBuildParams{disabled: true, exposed: true})
	require.Error(t, err)
}

func TestBuildAuthConfig_DisabledExposedReadOnlyOK(t *testing.T) {
	t.Parallel()

	// Read-only removes the write route, so disabling auth on an exposed
	// read-only server is safe — reads are anonymous by design.
	r, err := buildAuthConfig(authBuildParams{disabled: true, exposed: true, readOnly: true})
	require.NoError(t, err)
	require.Nil(t, r)
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

	r, err := buildAuthConfig(authBuildParams{})
	require.NoError(t, err)
	require.Nil(t, r, "no env + not exposed = no auth, no error")
}

func TestBuildAuthConfig_NoEnvExposedRefused(t *testing.T) {
	// t.Setenv is incompatible with t.Parallel; these tests share process env.
	withEnv(t, map[string]string{
		"STACKIT_GITHUB_CLIENT_ID":     "",
		"STACKIT_GITHUB_CLIENT_SECRET": "",
		"STACKIT_BASE_URL":             "",
		"STACKIT_SESSION_KEY":          "",
		"STACKIT_ALLOWED_GH_USERS":     "",
		"STACKIT_ALLOWED_GH_ORG":       "",
	})

	_, err := buildAuthConfig(authBuildParams{exposed: true})
	require.Error(t, err)
}

func TestBuildAuthConfig_NoEnvExposedReadOnlyOK(t *testing.T) {
	// t.Setenv is incompatible with t.Parallel; these tests share process env.
	withEnv(t, map[string]string{
		"STACKIT_GITHUB_CLIENT_ID":     "",
		"STACKIT_GITHUB_CLIENT_SECRET": "",
		"STACKIT_BASE_URL":             "",
		"STACKIT_SESSION_KEY":          "",
		"STACKIT_ALLOWED_GH_USERS":     "",
		"STACKIT_ALLOWED_GH_ORG":       "",
	})

	// An exposed read-only server boots anonymously with no OAuth env.
	r, err := buildAuthConfig(authBuildParams{exposed: true, readOnly: true})
	require.NoError(t, err)
	require.Nil(t, r)
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

	_, err := buildAuthConfig(authBuildParams{})
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

	r, err := buildAuthConfig(authBuildParams{})
	require.NoError(t, err)
	require.NotNil(t, r)
	require.NotNil(t, r.cfg.Handler)
	require.NotNil(t, r.cfg.SessionStore)
	require.NoError(t, r.store.Close())
}

func TestBuildAuthConfig_ExposedHTTPBaseURLRefused(t *testing.T) {
	// t.Setenv is incompatible with t.Parallel; these tests share process env.
	withEnv(t, map[string]string{
		"STACKIT_GITHUB_CLIENT_ID":     "id",
		"STACKIT_GITHUB_CLIENT_SECRET": "secret",
		"STACKIT_BASE_URL":             "http://10.0.0.5:8080",
		"STACKIT_SESSION_KEY":          mustKey(t),
		"STACKIT_ALLOWED_GH_USERS":     "jonnii",
	})

	// Exposed (off-host bind) + plaintext base URL would ship a non-Secure
	// session cookie over the network. Fail closed.
	_, err := buildAuthConfig(authBuildParams{exposed: true})
	require.Error(t, err)
	require.Contains(t, err.Error(), "https")
}

func TestBuildAuthConfig_ExposedHTTPSBaseURLOK(t *testing.T) {
	// t.Setenv is incompatible with t.Parallel; these tests share process env.
	withEnv(t, map[string]string{
		"STACKIT_GITHUB_CLIENT_ID":     "id",
		"STACKIT_GITHUB_CLIENT_SECRET": "secret",
		"STACKIT_BASE_URL":             "https://example.com",
		"STACKIT_SESSION_KEY":          mustKey(t),
		"STACKIT_ALLOWED_GH_USERS":     "jonnii",
	})

	// Exposed over https:// (e.g. behind a TLS-terminating proxy) is the
	// supported posture: boots with Secure cookies.
	r, err := buildAuthConfig(authBuildParams{exposed: true})
	require.NoError(t, err)
	require.NotNil(t, r)
	require.NoError(t, r.store.Close())
}

func TestBuildAuthConfig_LocalHTTPBaseURLOK(t *testing.T) {
	// t.Setenv is incompatible with t.Parallel; these tests share process env.
	withEnv(t, map[string]string{
		"STACKIT_GITHUB_CLIENT_ID":     "id",
		"STACKIT_GITHUB_CLIENT_SECRET": "secret",
		"STACKIT_BASE_URL":             "http://localhost:8080",
		"STACKIT_SESSION_KEY":          mustKey(t),
		"STACKIT_ALLOWED_GH_USERS":     "jonnii",
	})

	// Not exposed (loopback) + http:// is fine for local dev — the cookie never
	// leaves the host.
	r, err := buildAuthConfig(authBuildParams{})
	require.NoError(t, err)
	require.NotNil(t, r)
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

	_, err := buildAuthConfig(authBuildParams{})
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
