package main

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeRepoEntry_OwnerNameDerivesPathFromRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	r := repoConfig{Owner: "getstackit", Name: "stackit"}
	require.NoError(t, normalizeRepoEntry(&r, root))

	// id defaulted to <owner>-<name>; displayName to <owner>/<name>; path under root.
	require.Equal(t, "getstackit-stackit", r.ID)
	require.Equal(t, "getstackit/stackit", r.DisplayName)
	require.Equal(t, filepath.Join(root, "getstackit", "stackit"), r.Path)
	require.Equal(t, "origin", r.Remote)
}

func TestNormalizeRepoEntry_ExplicitIDAndDisplayNamePreserved(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	r := repoConfig{ID: "claude", DisplayName: "Claude Code", Owner: "anthropics", Name: "claude-code"}
	require.NoError(t, normalizeRepoEntry(&r, root))

	require.Equal(t, "claude", r.ID)
	require.Equal(t, "Claude Code", r.DisplayName)
	require.Equal(t, filepath.Join(root, "anthropics", "claude-code"), r.Path)
}

func TestNormalizeRepoEntry_AbsolutePathBypassesRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	abs := filepath.Join(t.TempDir(), "somewhere")

	r := repoConfig{ID: "a", Path: abs}
	require.NoError(t, normalizeRepoEntry(&r, root))
	require.Equal(t, abs, r.Path)
}

func TestNormalizeRepoEntry_RelativePathResolvesAgainstRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	r := repoConfig{ID: "a", Path: "team/proj"}
	require.NoError(t, normalizeRepoEntry(&r, root))
	require.Equal(t, filepath.Join(root, "team", "proj"), r.Path)
}

func TestNormalizeRepoEntry_RejectsBadInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		entry     repoConfig
		reposRoot string
		want      string
	}{
		{name: "missing id and owner+name", entry: repoConfig{Path: "/tmp/a"}, want: "missing id"},
		{name: "owner+name without root", entry: repoConfig{Owner: "a", Name: "b"}, want: "requires reposRoot"},
		{name: "no path and no owner/name", entry: repoConfig{ID: "a"}, want: "must set path, or owner+name"},
		{name: "relative path without root", entry: repoConfig{ID: "a", Path: "rel"}, want: "requires reposRoot"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			entry := tt.entry
			err := normalizeRepoEntry(&entry, tt.reposRoot)
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestRepoIDPattern(t *testing.T) {
	t.Parallel()

	for _, id := range []string{"a", "stackit", "my-repo", "my_repo", "repo123", "getstackit-stackit"} {
		require.True(t, repoIDPattern.MatchString(id), "want valid: %q", id)
	}
	for _, id := range []string{"a/b", "a b", "a.b", "a@b", ""} {
		require.False(t, repoIDPattern.MatchString(id), "want invalid: %q", id)
	}
}
