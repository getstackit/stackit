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
