package doctor

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFormatGHVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		out  string
		want string
	}{
		{
			name: "standard output reports the version number",
			out:  "gh version 2.89.0 (2026-03-26)\nhttps://github.com/cli/cli/releases/tag/v2.89.0\n",
			want: "gh 2.89.0",
		},
		{
			name: "output without a release URL",
			out:  "gh version 2.40.1 (2023-12-13)\n",
			want: "gh 2.40.1",
		},
		{
			name: "unrecognized output falls back to the first line",
			out:  "  something unexpected\nsecond line\n",
			want: "something unexpected",
		},
		{
			name: "empty output",
			out:  "",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, formatGHVersion(tt.out))
		})
	}
}
