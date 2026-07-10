package pr

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStackMetadataTrailersRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		stackSize int
		prNumbers []int
		scope     string
		wantSize  int
		wantPRs   []int
		wantScope string
	}{
		{
			name:      "full trailers",
			stackSize: 3,
			prNumbers: []int{45, 46, 47},
			scope:     "PROJ-123",
			wantSize:  3,
			wantPRs:   []int{45, 46, 47},
			wantScope: "PROJ-123",
		},
		{
			name:      "no scope",
			stackSize: 2,
			prNumbers: []int{10, 11},
			scope:     "",
			wantSize:  2,
			wantPRs:   []int{10, 11},
			wantScope: "",
		},
		{
			name:      "no PR numbers",
			stackSize: 1,
			prNumbers: nil,
			scope:     "FIX",
			wantSize:  1,
			wantPRs:   nil,
			wantScope: "FIX",
		},
		{
			name:      "single PR",
			stackSize: 1,
			prNumbers: []int{99},
			scope:     "",
			wantSize:  1,
			wantPRs:   []int{99},
			wantScope: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			formatted := NewStackMetadata(tt.stackSize, tt.prNumbers, tt.scope).ToTrailers()
			info := ParseStackMetadataTrailers(formatted)

			require.NotNil(t, info)
			require.Equal(t, tt.wantSize, info.StackSize)
			require.Equal(t, tt.wantPRs, info.PRNumbers)
			require.Equal(t, tt.wantScope, info.Scope)
		})
	}
}

func TestParseStackMetadataTrailers_noTrailers(t *testing.T) {
	t.Parallel()

	info := ParseStackMetadataTrailers("Just a regular commit message\n\nWith some body text.")
	require.Nil(t, info)
}

func TestParseStackMetadataTrailers_embeddedInCommitBody(t *testing.T) {
	t.Parallel()

	body := `Consolidate stack [PROJ-123]: feat-a, feat-b, feat-c

This merges the following changes:
1. feat-a (#45)
2. feat-b (#46)
3. feat-c (#47)

Stackit-Stack-Size: 3
Stackit-PRs: 45,46,47
Stackit-Scope: PROJ-123`

	info := ParseStackMetadataTrailers(body)
	require.NotNil(t, info)
	require.Equal(t, 3, info.StackSize)
	require.Equal(t, []int{45, 46, 47}, info.PRNumbers)
	require.Equal(t, "PROJ-123", info.Scope)
}

func TestStackMetadataToTrailers_format(t *testing.T) {
	t.Parallel()

	result := NewStackMetadata(3, []int{1, 2, 3}, "SCOPE").ToTrailers()
	require.Contains(t, result, "Stackit-Stack-Size: 3")
	require.Contains(t, result, "Stackit-PRs: 1,2,3")
	require.Contains(t, result, "Stackit-Scope: SCOPE")
}
