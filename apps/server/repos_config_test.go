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

func TestLoadReposConfig_OwnerNameDerivesPathFromRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := writeTemp(t, `{
	  "reposRoot": "`+root+`",
	  "repos": [
	    {"owner": "getstackit", "name": "stackit"},
	    {"id": "claude", "owner": "anthropics", "name": "claude-code", "displayName": "Claude Code"}
	  ]
	}`)

	cfg, err := loadReposConfig(path, "")
	require.NoError(t, err)
	require.Len(t, cfg.Repos, 2)

	// id defaulted to <owner>-<name>; displayName to <owner>/<name>; path under reposRoot.
	require.Equal(t, "getstackit-stackit", cfg.Repos[0].ID)
	require.Equal(t, "getstackit/stackit", cfg.Repos[0].DisplayName)
	require.Equal(t, filepath.Join(root, "getstackit", "stackit"), cfg.Repos[0].Path)
	require.Equal(t, "origin", cfg.Repos[0].Remote)

	// Explicit id + displayName preserved.
	require.Equal(t, "claude", cfg.Repos[1].ID)
	require.Equal(t, "Claude Code", cfg.Repos[1].DisplayName)
	require.Equal(t, filepath.Join(root, "anthropics", "claude-code"), cfg.Repos[1].Path)
}

func TestLoadReposConfig_RootOverrideWinsOverFile(t *testing.T) {
	t.Parallel()

	fileRoot := t.TempDir()
	cliRoot := t.TempDir()
	path := writeTemp(t, `{
	  "reposRoot": "`+fileRoot+`",
	  "repos": [{"owner": "a", "name": "b"}]
	}`)

	cfg, err := loadReposConfig(path, cliRoot)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(cliRoot, "a", "b"), cfg.Repos[0].Path)
}

func TestLoadReposConfig_AbsolutePathBypassesRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	abs := filepath.Join(t.TempDir(), "somewhere")
	path := writeTemp(t, `{
	  "reposRoot": "`+root+`",
	  "repos": [{"id": "a", "path": "`+abs+`"}]
	}`)

	cfg, err := loadReposConfig(path, "")
	require.NoError(t, err)
	require.Equal(t, abs, cfg.Repos[0].Path)
}

func TestLoadReposConfig_RelativePathResolvesAgainstRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := writeTemp(t, `{
	  "reposRoot": "`+root+`",
	  "repos": [{"id": "a", "path": "team/proj"}]
	}`)

	cfg, err := loadReposConfig(path, "")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(root, "team", "proj"), cfg.Repos[0].Path)
}

func TestLoadReposConfig_RejectsBadInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "empty repos", body: `{"repos": []}`, want: "must list at least one repo"},
		{name: "missing id and owner+name", body: `{"repos": [{"path": "/tmp/a"}]}`, want: "missing id"},
		{name: "invalid id", body: `{"repos": [{"id": "a/b", "path": "/tmp/a"}]}`, want: "must match"},
		{name: "duplicate id", body: `{"repos": [{"id": "a", "path": "/x"}, {"id": "a", "path": "/y"}]}`, want: "duplicate id"},
		{name: "owner+name without root", body: `{"repos": [{"owner": "a", "name": "b"}]}`, want: "requires reposRoot"},
		{name: "no path and no owner/name", body: `{"repos": [{"id": "a"}]}`, want: "must set path, or owner+name"},
		{name: "relative path without root", body: `{"repos": [{"id": "a", "path": "rel"}]}`, want: "requires reposRoot"},
		{name: "malformed json", body: `not json`, want: "parse repos config"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := writeTemp(t, tt.body)
			_, err := loadReposConfig(path, "")
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestLoadReposConfig_MissingFile(t *testing.T) {
	t.Parallel()
	_, err := loadReposConfig(filepath.Join(t.TempDir(), "nope.json"), "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "read repos config")
}
