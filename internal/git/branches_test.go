package git_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
)

// Reproduces the case-insensitive-filesystem divergence from issue #1532
// without needing such a filesystem: HEAD reports one casing, for-each-ref
// reports another.
func TestResolveBranchNameCase(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		head     string
		branches []string
		expected string
	}{
		{
			name:     "exact match is returned unchanged",
			head:     "claude/feature",
			branches: []string{"main", "claude/feature"},
			expected: "claude/feature",
		},
		{
			name:     "HEAD casing maps onto the on-disk ref casing",
			head:     "Claude/20260730021014/translate-raw-checkout-error",
			branches: []string{"main", "claude/20260730021014/translate-raw-checkout-error"},
			expected: "claude/20260730021014/translate-raw-checkout-error",
		},
		{
			name:     "exact match wins over a case-insensitive candidate",
			head:     "Feature",
			branches: []string{"feature", "Feature"},
			expected: "Feature",
		},
		{
			name:     "unknown branch is returned unchanged",
			head:     "missing",
			branches: []string{"main", "feature"},
			expected: "missing",
		},
		{
			name:     "ambiguous case-insensitive candidates leave HEAD authoritative",
			head:     "FEATURE",
			branches: []string{"feature", "Feature"},
			expected: "FEATURE",
		},
		{
			name:     "empty name is returned unchanged",
			head:     "",
			branches: []string{"main"},
			expected: "",
		},
		{
			name:     "empty branch list is returned unchanged",
			head:     "feature",
			branches: nil,
			expected: "feature",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.expected, git.ResolveBranchNameCase(tt.head, tt.branches))
		})
	}
}

func TestCaseMismatchHint(t *testing.T) {
	t.Parallel()

	t.Run("names the on-disk casing when only case differs", func(t *testing.T) {
		t.Parallel()
		hint := git.CaseMismatchHint("Claude/feature", []string{"main", "claude/feature"})
		require.Contains(t, hint, "claude/feature")
		require.Contains(t, hint, "differ only in case")
	})

	t.Run("empty when the branch exists", func(t *testing.T) {
		t.Parallel()
		require.Empty(t, git.CaseMismatchHint("feature", []string{"main", "feature"}))
	})

	t.Run("empty when no case-insensitive candidate exists", func(t *testing.T) {
		t.Parallel()
		require.Empty(t, git.CaseMismatchHint("missing", []string{"main", "feature"}))
	})
}
