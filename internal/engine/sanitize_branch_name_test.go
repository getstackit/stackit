package engine

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSanitizeBranchNameForStackID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "slashes replaced with hyphens",
			input:    "feature/with/slashes",
			expected: "feature-with-slashes",
		},
		{
			name:     "consecutive hyphens collapsed",
			input:    "a--b",
			expected: "a-b",
		},
		{
			name:     "leading and trailing hyphens trimmed",
			input:    "-a-",
			expected: "a",
		},
		{
			name:     "special characters replaced with hyphen",
			input:    "a!@#b",
			expected: "a-b",
		},
		{
			name:     "length truncated to 50 chars",
			input:    strings.Repeat("a", 60),
			expected: strings.Repeat("a", 50),
		},
		{
			name:     "all special chars results in fallback",
			input:    "///",
			expected: "stack",
		},
		{
			name:     "empty string returns fallback",
			input:    "",
			expected: "stack",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := sanitizeBranchNameForStackID(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}
