package absorb

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckoutSource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		subject  string
		wantFrom string
		wantOK   bool
	}{
		{
			name:     "branch to branch",
			subject:  "checkout: moving from feature/a to main",
			wantFrom: "feature/a",
			wantOK:   true,
		},
		{
			name:     "branch to detached commit",
			subject:  "checkout: moving from feature to 0123abcd",
			wantFrom: "feature",
			wantOK:   true,
		},
		{
			name:    "commit entry",
			subject: "commit: add feature",
		},
		{
			name:    "rebase entry",
			subject: "rebase (finish): returning to refs/heads/feature",
		},
		{
			name: "empty line",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			from, ok := checkoutSource(tt.subject)
			require.Equal(t, tt.wantOK, ok)
			require.Equal(t, tt.wantFrom, from)
		})
	}
}
