package style

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDisplayBranchName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "strips user and timestamp segments",
			input:    "jonnii/20260511011552/guard-runner.repoRoot-reads-with-repoMu-to-fix",
			expected: "guard-runner.repoRoot-reads-with-repoMu-to-fix",
		},
		{
			name:     "keeps slashes in the slug",
			input:    "jonnii/20260605032227/extract-OpenEditor-into-internal/editor-out-of",
			expected: "extract-OpenEditor-into-internal/editor-out-of",
		},
		{
			name:     "leaves plain names alone",
			input:    "refactor-unify-editor-command-launching",
			expected: "refactor-unify-editor-command-launching",
		},
		{
			name:     "leaves non-timestamp middle segments alone",
			input:    "feature/auth/login",
			expected: "feature/auth/login",
		},
		{
			name:     "leaves short digit segments alone",
			input:    "release/2026/patch",
			expected: "release/2026/patch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.expected, DisplayBranchName(tt.input))
		})
	}
}

func TestBranchNameResolver(t *testing.T) {
	t.Parallel()
	alice := "alice/20260901000000/fix-tests"
	bob := "bob/20260905000000/fix-tests"

	t.Run("unique short names stay short", func(t *testing.T) {
		t.Parallel()
		r := NewBranchNameResolver(alice, "carol/20260906000000/docs")
		require.Equal(t, "fix-tests", r.Short(alice))
		require.Equal(t, "docs", r.Short("carol/20260906000000/docs"))
	})

	t.Run("seeded collision renders both in full", func(t *testing.T) {
		t.Parallel()
		r := NewBranchNameResolver(alice, bob)
		require.Equal(t, alice, r.Short(alice))
		require.Equal(t, bob, r.Short(bob))
	})

	t.Run("collision learned while rendering", func(t *testing.T) {
		t.Parallel()
		r := NewBranchNameResolver()
		require.Equal(t, "fix-tests", r.Short(alice))
		require.Equal(t, bob, r.Short(bob))
		require.Equal(t, alice, r.Short(alice))
	})

	t.Run("short name shared with an untimestamped branch", func(t *testing.T) {
		t.Parallel()
		r := NewBranchNameResolver("fix-tests", alice)
		require.Equal(t, alice, r.Short(alice))
		require.Equal(t, "fix-tests", r.Short("fix-tests"))
	})
}
