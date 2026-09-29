package pr

import (
	"testing"

	"github.com/getstackit/stackit/internal/git"
	"github.com/stretchr/testify/require"
)

func TestStackMetadataToTrailers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		stackSize int
		prNumbers []git.PRNumber
		scope     string
		want      string
	}{
		{
			name:      "full trailers",
			stackSize: 3,
			prNumbers: []git.PRNumber{45, 46, 47},
			scope:     "PROJ-123",
			want:      "Stackit-Stack-Size: 3\nStackit-PRs: 45,46,47\nStackit-Scope: PROJ-123\n",
		},
		{
			name:      "no scope",
			stackSize: 2,
			prNumbers: []git.PRNumber{10, 11},
			want:      "Stackit-Stack-Size: 2\nStackit-PRs: 10,11\n",
		},
		{
			name:      "no PR numbers",
			stackSize: 1,
			scope:     "FIX",
			want:      "Stackit-Stack-Size: 1\nStackit-Scope: FIX\n",
		},
		{
			name:      "single PR",
			stackSize: 1,
			prNumbers: []git.PRNumber{99},
			want:      "Stackit-Stack-Size: 1\nStackit-PRs: 99\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, NewStackMetadata(tt.stackSize, tt.prNumbers, tt.scope).ToTrailers())
		})
	}
}

func TestStackMetadataToTrailers_format(t *testing.T) {
	t.Parallel()

	result := NewStackMetadata(3, []git.PRNumber{1, 2, 3}, "SCOPE").ToTrailers()
	require.Contains(t, result, "Stackit-Stack-Size: 3")
	require.Contains(t, result, "Stackit-PRs: 1,2,3")
	require.Contains(t, result, "Stackit-Scope: SCOPE")
}
