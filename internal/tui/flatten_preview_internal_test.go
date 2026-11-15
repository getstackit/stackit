package tui

import (
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

func TestTruncateBranchName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{name: "short name unchanged", input: "feature", maxLen: 10, want: "feature"},
		{name: "long name gets ellipsis", input: "feature-branch", maxLen: 10, want: "feature..."},
		{name: "tiny width cuts without ellipsis", input: "feature", maxLen: 3, want: "fea"},
		{name: "multi-byte characters are never split", input: "fëature-brånch", maxLen: 6, want: "fëa..."},
		{name: "wide characters are measured in display cells", input: "機能ブランチ", maxLen: 7, want: "機能..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := truncateBranchName(tt.input, tt.maxLen)
			require.Equal(t, tt.want, got)
			require.True(t, utf8.ValidString(got))
		})
	}
}
