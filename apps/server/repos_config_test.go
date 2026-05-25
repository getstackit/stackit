package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeTemp(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "repos.json")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	return path
}

func TestLoadReposConfig_ValidatesAndDefaults(t *testing.T) {
	t.Parallel()

	path := writeTemp(t, `{
	  "repos": [
	    {"id": "a", "path": "/tmp/a"},
	    {"id": "b", "displayName": "Repo B", "path": "/tmp/b", "remote": "upstream"}
	  ]
	}`)

	cfg, err := loadReposConfig(path)
	require.NoError(t, err)
	require.Len(t, cfg.Repos, 2)

	// Defaults filled in for repo a
	require.Equal(t, "a", cfg.Repos[0].ID)
	require.Equal(t, "a", cfg.Repos[0].DisplayName)
	require.Equal(t, "origin", cfg.Repos[0].Remote)

	// Explicit values preserved for repo b
	require.Equal(t, "Repo B", cfg.Repos[1].DisplayName)
	require.Equal(t, "upstream", cfg.Repos[1].Remote)
}

func TestLoadReposConfig_RejectsBadInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "empty repos", body: `{"repos": []}`, want: "must list at least one repo"},
		{name: "missing id", body: `{"repos": [{"path": "/tmp/a"}]}`, want: "missing id"},
		{name: "invalid id", body: `{"repos": [{"id": "a/b", "path": "/tmp/a"}]}`, want: "must match"},
		{name: "duplicate id", body: `{"repos": [{"id": "a", "path": "/x"}, {"id": "a", "path": "/y"}]}`, want: "duplicate id"},
		{name: "missing path", body: `{"repos": [{"id": "a"}]}`, want: "missing path"},
		{name: "malformed json", body: `not json`, want: "parse repos config"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := writeTemp(t, tt.body)
			_, err := loadReposConfig(path)
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestLoadReposConfig_MissingFile(t *testing.T) {
	t.Parallel()
	_, err := loadReposConfig(filepath.Join(t.TempDir(), "nope.json"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "read repos config")
}
